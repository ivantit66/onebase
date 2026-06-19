package storage

// Журнал ИИ-обращений (план 54, этап 2) и суточный потолок токенов (этап 3).
// Каждое обращение к ИИ (чат) и каждый запрос инструмента «выполнить_запрос»
// пишутся в _ai_audit: кто, когда, какая модель, какой запрос, сколько строк
// и токенов. Нужен и для безопасности (флаг AIDataAccess = чтение всех данных
// + передача во внешний LLM — должно быть видно, кто что спрашивал), и для
// контроля стоимости.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AIAuditEntry — одна запись журнала ИИ.
type AIAuditEntry struct {
	ID           string
	UserID       string
	UserLogin    string
	Task         string // "чат", "чат-запрос" (инструмент), ...
	Model        string
	Query        string // текст запроса инструмента (пусто для сводной записи чата)
	Rows         int    // строк результата (для инструмента)
	InputTokens  int
	OutputTokens int
	Response     string // ответ модели (для журнала конфигуратора)
	At           time.Time
}

// EnsureAIAuditSchema создаёт таблицу _ai_audit (идемпотентно).
func (db *DB) EnsureAIAuditSchema(ctx context.Context) error {
	d := db.dialect
	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS _ai_audit (
			id %s PRIMARY KEY,
			user_id TEXT NOT NULL DEFAULT '',
			user_login TEXT NOT NULL DEFAULT '',
			task TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			query TEXT NOT NULL DEFAULT '',
			rows_count INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			at %s NOT NULL DEFAULT %s
		)`, d.TypeUUID(), d.TypeTimestamp(), d.CurrentTimestampTZ())
	if _, err := db.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("ai_audit: create _ai_audit: %w", err)
	}
	// Ленивая миграция: добавляем столбец ответа для баз, созданных раньше.
	if err := db.AddColumnIfMissing(ctx, "_ai_audit", "response", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("ai_audit: add response: %w", err)
	}
	return nil
}

// LogAIQuery пишет запись журнала ИИ. Best-effort: ошибка записи не должна
// ломать ответ пользователю, поэтому проглатывается (таблица создаётся лениво).
func (db *DB) LogAIQuery(ctx context.Context, e AIAuditEntry) {
	if err := db.EnsureAIAuditSchema(ctx); err != nil {
		return
	}
	d := db.dialect
	q := fmt.Sprintf(`INSERT INTO _ai_audit
		(id, user_id, user_login, task, model, query, rows_count, input_tokens, output_tokens, response)
		VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)`,
		d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4), d.Placeholder(5),
		d.Placeholder(6), d.Placeholder(7), d.Placeholder(8), d.Placeholder(9), d.Placeholder(10))
	_, _ = db.Exec(ctx, q,
		uuid.NewString(), e.UserID, e.UserLogin, e.Task, e.Model, e.Query,
		e.Rows, e.InputTokens, e.OutputTokens, e.Response)
}

// ListAIAudit возвращает последние записи журнала ИИ (новые первыми).
func (db *DB) ListAIAudit(ctx context.Context, limit int) ([]AIAuditEntry, error) {
	if err := db.EnsureAIAuditSchema(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(ctx, fmt.Sprintf(`SELECT id, user_id, user_login, task, model, query,
		rows_count, input_tokens, output_tokens, response, at
		FROM _ai_audit ORDER BY at DESC LIMIT %d`, limit))
	if err != nil {
		return nil, fmt.Errorf("ai_audit: list: %w", err)
	}
	defer rows.Close()
	var out []AIAuditEntry
	for rows.Next() {
		var e AIAuditEntry
		var at any
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserLogin, &e.Task, &e.Model, &e.Query,
			&e.Rows, &e.InputTokens, &e.OutputTokens, &e.Response, &at); err != nil {
			return nil, err
		}
		e.At = parseAuditTime(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// AITokensUsedSince — суммарный расход токенов (вход+выход) с указанного
// момента. Используется суточным потолком (ai.daily_token_cap).
func (db *DB) AITokensUsedSince(ctx context.Context, since time.Time) (int, error) {
	if err := db.EnsureAIAuditSchema(ctx); err != nil {
		return 0, err
	}
	d := db.dialect
	var total int
	err := db.QueryRow(ctx,
		`SELECT COALESCE(SUM(input_tokens + output_tokens), 0) FROM _ai_audit WHERE at >= `+d.Placeholder(1),
		since.UTC()).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("ai_audit: tokens used: %w", err)
	}
	return total, nil
}

// aiDailyTokenCapKey — ключ _settings суточного потолка токенов ИИ
// (0 или отсутствие = без лимита).
const aiDailyTokenCapKey = "ai.daily_token_cap"

// GetAIDailyTokenCap читает суточный потолок токенов ИИ; 0 = без лимита.
func (db *DB) GetAIDailyTokenCap(ctx context.Context) int {
	d := db.dialect
	var v string
	err := db.QueryRow(ctx,
		`SELECT value FROM _settings WHERE key = `+d.Placeholder(1), aiDailyTokenCapKey).Scan(&v)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// SaveAIDailyTokenCap сохраняет суточный потолок токенов ИИ (0 = без лимита).
func (db *DB) SaveAIDailyTokenCap(ctx context.Context, cap int) error {
	if err := db.EnsureSettingsSchema(ctx); err != nil {
		return err
	}
	if cap < 0 {
		cap = 0
	}
	d := db.dialect
	q := fmt.Sprintf(
		`INSERT INTO _settings (key, value) VALUES (%s, %s)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		d.Placeholder(1), d.Placeholder(2))
	if _, err := db.Exec(ctx, q, aiDailyTokenCapKey, strconv.Itoa(cap)); err != nil {
		return fmt.Errorf("settings: save %s: %w", aiDailyTokenCapKey, err)
	}
	return nil
}

// Режимы доступа ИИ-ассистента к данным (ключ _settings ai.data_scope, план 54).
const (
	aiDataScopeKey = "ai.data_scope"
	// AIDataScopeAdminOnly — инструменты данных только админам (дефолт).
	AIDataScopeAdminOnly = "admin_only"
	// AIDataScopeRBAC — инструменты доступны пользователям с флагом AIDataAccess,
	// но источники запроса фильтруются по правам чтения (s.can).
	AIDataScopeRBAC = "rbac"
	// AIDataScopeAll — доступ ко всем данным без объектной проверки прав.
	AIDataScopeAll = "all"
)

// GetAIDataScope читает режим доступа ИИ к данным; по умолчанию admin_only.
func (db *DB) GetAIDataScope(ctx context.Context) string {
	d := db.dialect
	var v string
	err := db.QueryRow(ctx,
		`SELECT value FROM _settings WHERE key = `+d.Placeholder(1), aiDataScopeKey).Scan(&v)
	if err != nil {
		return AIDataScopeAdminOnly
	}
	switch strings.TrimSpace(v) {
	case AIDataScopeRBAC:
		return AIDataScopeRBAC
	case AIDataScopeAll:
		return AIDataScopeAll
	default:
		return AIDataScopeAdminOnly
	}
}

// SaveAIDataScope сохраняет режим доступа ИИ к данным (admin_only|rbac|all).
func (db *DB) SaveAIDataScope(ctx context.Context, scope string) error {
	if err := db.EnsureSettingsSchema(ctx); err != nil {
		return err
	}
	switch scope {
	case AIDataScopeRBAC, AIDataScopeAll:
		// ok
	default:
		scope = AIDataScopeAdminOnly
	}
	d := db.dialect
	q := fmt.Sprintf(
		`INSERT INTO _settings (key, value) VALUES (%s, %s)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		d.Placeholder(1), d.Placeholder(2))
	if _, err := db.Exec(ctx, q, aiDataScopeKey, scope); err != nil {
		return fmt.Errorf("settings: save %s: %w", aiDataScopeKey, err)
	}
	return nil
}
