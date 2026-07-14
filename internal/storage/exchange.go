package storage

// Хранилище планов обмена (план 86): очередь регистрации изменений (outbox),
// счётчики сообщений по узлам и код текущего узла базы.
//
// changed_at хранится как Unix-миллисекунды (BIGINT) — портируемо между SQLite
// и PostgreSQL без плясок с форматами timestamp и путешествует в пакете как
// целое; правило конфликта by_time сравнивает эти числа напрямую.

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

// ExchangeChange — одна строка очереди регистрации: объект из состава плана,
// ждущий отправки узлу NodeCode. SentNo — номер сообщения, в котором строка была
// в последний раз выгружена (0 = ещё не выгружалась); сбрасывается в 0 при новой
// правке объекта.
type ExchangeChange struct {
	Plan       string
	ObjectType string
	ObjectID   string
	NodeCode   string
	Version    int64
	Deletion   bool
	ChangedAt  int64 // Unix-миллисекунды
	SentNo     int64
}

// ExchangePeer — счётчики обмена с одним узлом плана.
type ExchangePeer struct {
	Plan     string
	NodeCode string
	SentNo   int64 // последний выданный номер сообщения этому узлу
	AckNo    int64 // номер сообщения, приём которого узел подтвердил
	RecvNo   int64 // последний номер сообщения, принятого ОТ этого узла
}

// EnsureExchangeSchema создаёт служебные таблицы обмена. Идемпотентно.
func (db *DB) EnsureExchangeSchema(ctx context.Context) error {
	if _, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _exchange_changes (
			plan        TEXT NOT NULL,
			object_type TEXT NOT NULL,
			object_id   TEXT NOT NULL,
			node_code   TEXT NOT NULL,
			version     BIGINT NOT NULL,
			deletion    INTEGER NOT NULL DEFAULT 0,
			changed_at  BIGINT NOT NULL,
			sent_no     BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (plan, object_type, object_id, node_code)
		)`); err != nil {
		return fmt.Errorf("exchange: create _exchange_changes: %w", err)
	}
	if _, err := db.Exec(ctx,
		`CREATE INDEX IF NOT EXISTS idx_exchange_changes_node ON _exchange_changes (plan, node_code)`); err != nil {
		return fmt.Errorf("exchange: index _exchange_changes: %w", err)
	}
	if _, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _exchange_peers (
			plan      TEXT NOT NULL,
			node_code TEXT NOT NULL,
			sent_no   BIGINT NOT NULL DEFAULT 0,
			ack_no    BIGINT NOT NULL DEFAULT 0,
			recv_no   BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (plan, node_code)
		)`); err != nil {
		return fmt.Errorf("exchange: create _exchange_peers: %w", err)
	}
	// Настройка «этот узел» живёт в _settings — создаём заодно.
	return db.EnsureSettingsSchema(ctx)
}

// exchangeThisNodeKey — ключ _settings для кода текущего узла в рамках плана.
// Код per-plan: одна база может быть «center» в одном плане и «hub» в другом.
func exchangeThisNodeKey(plan string) string {
	return "exchange.this_node." + strings.ToLower(plan)
}

// GetExchangeThisNode возвращает код текущего узла базы для плана ("" если не
// задан). Отсутствие ключа/таблицы — не ошибка.
func (db *DB) GetExchangeThisNode(ctx context.Context, plan string) (string, error) {
	d := db.dialect
	var v string
	err := db.QueryRow(ctx,
		`SELECT value FROM _settings WHERE key = `+d.Placeholder(1),
		exchangeThisNodeKey(plan)).Scan(&v)
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(v), nil
}

// SaveExchangeThisNode задаёт код текущего узла базы для плана (onebase exchange
// init). Пустой код удаляет настройку.
func (db *DB) SaveExchangeThisNode(ctx context.Context, plan, code string) error {
	if err := db.EnsureSettingsSchema(ctx); err != nil {
		return err
	}
	d := db.dialect
	code = strings.TrimSpace(code)
	if code == "" {
		_, err := db.Exec(ctx, `DELETE FROM _settings WHERE key = `+d.Placeholder(1), exchangeThisNodeKey(plan))
		return err
	}
	q := fmt.Sprintf(
		`INSERT INTO _settings (key, value) VALUES (%s, %s)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		d.Placeholder(1), d.Placeholder(2))
	if _, err := db.Exec(ctx, q, exchangeThisNodeKey(plan), code); err != nil {
		return fmt.Errorf("exchange: save this_node: %w", err)
	}
	return nil
}

// RegisterExchangeChange добавляет/обновляет строку очереди для одного узла.
// Новая правка (upsert по PK) обновляет версию/пометку/время и сбрасывает
// sent_no в 0 — объект нужно выгрузить заново, даже если он уже был отправлен.
func (db *DB) RegisterExchangeChange(ctx context.Context, ch ExchangeChange) error {
	d := db.dialect
	q := fmt.Sprintf(`
		INSERT INTO _exchange_changes (plan, object_type, object_id, node_code, version, deletion, changed_at, sent_no)
		VALUES (%s, %s, %s, %s, %s, %s, %s, 0)
		ON CONFLICT (plan, object_type, object_id, node_code) DO UPDATE SET
			version = EXCLUDED.version,
			deletion = EXCLUDED.deletion,
			changed_at = EXCLUDED.changed_at,
			sent_no = 0`,
		d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4),
		d.Placeholder(5), d.Placeholder(6), d.Placeholder(7))
	if _, err := db.Exec(ctx, q, ch.Plan, ch.ObjectType, ch.ObjectID, ch.NodeCode,
		ch.Version, boolToInt(ch.Deletion), ch.ChangedAt); err != nil {
		return fmt.Errorf("exchange: register change: %w", err)
	}
	return nil
}

// PendingExchangeChanges возвращает все строки очереди для узла (в т.ч. уже
// выгруженные, но не подтверждённые — чтобы потерянный пакет переотправлялся),
// в хронологическом порядке.
func (db *DB) PendingExchangeChanges(ctx context.Context, plan, nodeCode string) ([]ExchangeChange, error) {
	d := db.dialect
	q := fmt.Sprintf(`
		SELECT plan, object_type, object_id, node_code, version, deletion, changed_at, sent_no
		FROM _exchange_changes
		WHERE plan = %s AND node_code = %s
		ORDER BY changed_at ASC, object_type ASC, object_id ASC`,
		d.Placeholder(1), d.Placeholder(2))
	rows, err := db.Query(ctx, q, plan, nodeCode)
	if err != nil {
		return nil, fmt.Errorf("exchange: pending changes: %w", err)
	}
	defer rows.Close()
	var out []ExchangeChange
	for rows.Next() {
		var ch ExchangeChange
		var del int64
		if err := rows.Scan(&ch.Plan, &ch.ObjectType, &ch.ObjectID, &ch.NodeCode,
			&ch.Version, &del, &ch.ChangedAt, &ch.SentNo); err != nil {
			return nil, fmt.Errorf("exchange: scan change: %w", err)
		}
		ch.Deletion = del != 0
		out = append(out, ch)
	}
	return out, rows.Err()
}

// MarkExchangeChangesSent проставляет sent_no выгруженным строкам (по точным
// первичным ключам, чтобы не задеть строки, зарегистрированные после выборки).
func (db *DB) MarkExchangeChangesSent(ctx context.Context, changes []ExchangeChange, messageNo int64) error {
	d := db.dialect
	q := fmt.Sprintf(`UPDATE _exchange_changes SET sent_no = %s
		WHERE plan = %s AND object_type = %s AND object_id = %s AND node_code = %s`,
		d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4), d.Placeholder(5))
	for _, ch := range changes {
		if _, err := db.Exec(ctx, q, messageNo, ch.Plan, ch.ObjectType, ch.ObjectID, ch.NodeCode); err != nil {
			return fmt.Errorf("exchange: mark sent: %w", err)
		}
	}
	return nil
}

// AckExchangeChanges удаляет из очережди строки узла, выгруженные в сообщениях с
// номером ≤ uptoMessageNo (подтверждён приём), и запоминает подтверждённый номер.
// Возвращает число снятых строк.
func (db *DB) AckExchangeChanges(ctx context.Context, plan, nodeCode string, uptoMessageNo int64) (int64, error) {
	d := db.dialect
	q := fmt.Sprintf(`DELETE FROM _exchange_changes
		WHERE plan = %s AND node_code = %s AND sent_no > 0 AND sent_no <= %s`,
		d.Placeholder(1), d.Placeholder(2), d.Placeholder(3))
	tag, err := db.Exec(ctx, q, plan, nodeCode, uptoMessageNo)
	if err != nil {
		return 0, fmt.Errorf("exchange: ack changes: %w", err)
	}
	if err := db.setPeerCounter(ctx, plan, nodeCode, "ack_no", uptoMessageNo); err != nil {
		return 0, err
	}
	return tag.RowsAffected, nil
}

// NextExchangeMessageNo атомарно увеличивает и возвращает номер следующего
// сообщения для узла (аналог NextNum). Безопасно при конкурентных выгрузках.
func (db *DB) NextExchangeMessageNo(ctx context.Context, plan, nodeCode string) (int64, error) {
	d := db.dialect
	q := fmt.Sprintf(`
		INSERT INTO _exchange_peers (plan, node_code, sent_no) VALUES (%s, %s, 1)
		ON CONFLICT (plan, node_code) DO UPDATE SET sent_no = _exchange_peers.sent_no + 1
		RETURNING sent_no`,
		d.Placeholder(1), d.Placeholder(2))
	var n int64
	if err := db.QueryRow(ctx, q, plan, nodeCode).Scan(&n); err != nil {
		return 0, fmt.Errorf("exchange: next message no: %w", err)
	}
	return n, nil
}

// SetExchangeRecvNo запоминает номер последнего сообщения, принятого от узла
// (монотонно: меньшие номера не откатывают счётчик).
func (db *DB) SetExchangeRecvNo(ctx context.Context, plan, nodeCode string, recvNo int64) error {
	return db.setPeerCounter(ctx, plan, nodeCode, "recv_no", recvNo)
}

// GetExchangePeer возвращает счётчики обмена с узлом (нули, если строки нет).
func (db *DB) GetExchangePeer(ctx context.Context, plan, nodeCode string) (ExchangePeer, error) {
	d := db.dialect
	p := ExchangePeer{Plan: plan, NodeCode: nodeCode}
	err := db.QueryRow(ctx,
		fmt.Sprintf(`SELECT sent_no, ack_no, recv_no FROM _exchange_peers WHERE plan = %s AND node_code = %s`,
			d.Placeholder(1), d.Placeholder(2)),
		plan, nodeCode).Scan(&p.SentNo, &p.AckNo, &p.RecvNo)
	if err != nil {
		return ExchangePeer{Plan: plan, NodeCode: nodeCode}, nil
	}
	return p, nil
}

// setPeerCounter монотонно поднимает один счётчик узла (ack_no/recv_no) до value.
// col — доверенное имя колонки из констант этого пакета, не пользовательский ввод.
func (db *DB) setPeerCounter(ctx context.Context, plan, nodeCode, col string, value int64) error {
	d := db.dialect
	q := fmt.Sprintf(`
		INSERT INTO _exchange_peers (plan, node_code, %s) VALUES (%s, %s, %s)
		ON CONFLICT (plan, node_code) DO UPDATE SET %s = CASE
			WHEN _exchange_peers.%s > EXCLUDED.%s THEN _exchange_peers.%s ELSE EXCLUDED.%s END`,
		col, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3),
		col, col, col, col, col)
	if _, err := db.Exec(ctx, q, plan, nodeCode, value); err != nil {
		return fmt.Errorf("exchange: set %s: %w", col, err)
	}
	return nil
}

// EntityVersion возвращает текущую ревизию (_version) объекта. Регистрация
// обмена снимает по ней версию в момент записи (в той же транзакции, поэтому
// значение уже инкрементировано Upsert'ом).
func (db *DB) EntityVersion(ctx context.Context, entityName string, id uuid.UUID) (int64, error) {
	d := db.dialect
	var v int64
	err := db.QueryRow(ctx,
		fmt.Sprintf("SELECT _version FROM %s WHERE id = %s", metadata.TableName(entityName), d.Placeholder(1)),
		idArg(d, id)).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("exchange: read version %s: %w", entityName, err)
	}
	return v, nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
