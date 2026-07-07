package storage

// Политика сессий (план 78, п. 1.6): «Максимум одновременных сессий на
// пользователя» — не режим совместимости, а осознанная политика безопасности
// (борьба с шарингом учёток: оператор не должен работать с одной учёткой на
// двух рабочих местах). 0/пусто = безлимит (по умолчанию), 1 = фактически
// поведение прежних версий, N — на вырост. Свойство ИНСТАНСА базы, поэтому
// _settings, а не app.yaml.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// maxSessionsPerUserKey — ключ _settings лимита сессий.
const maxSessionsPerUserKey = "auth.max_sessions_per_user"

// GetMaxSessionsPerUser возвращает лимит одновременных enterprise-сессий на
// пользователя. Отсутствие ключа/таблицы или мусор → 0 (безлимит).
func (db *DB) GetMaxSessionsPerUser(ctx context.Context) int {
	d := db.dialect
	var v string
	err := db.QueryRow(ctx,
		`SELECT value FROM _settings WHERE key = `+d.Placeholder(1), maxSessionsPerUserKey).Scan(&v)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// SaveMaxSessionsPerUser сохраняет лимит сессий (0 = безлимит).
func (db *DB) SaveMaxSessionsPerUser(ctx context.Context, n int) error {
	if n < 0 {
		n = 0
	}
	if err := db.EnsureSettingsSchema(ctx); err != nil {
		return err
	}
	d := db.dialect
	q := fmt.Sprintf(
		`INSERT INTO _settings (key, value) VALUES (%s, %s)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		d.Placeholder(1), d.Placeholder(2))
	if _, err := db.Exec(ctx, q, maxSessionsPerUserKey, strconv.Itoa(n)); err != nil {
		return fmt.Errorf("settings: save %s: %w", maxSessionsPerUserKey, err)
	}
	return nil
}
