package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

// ErrVersionConflict возвращается из UpsertVersioned, когда фактическая
// ревизия (_version) объекта в БД не совпадает с ожидаемой — то есть
// между загрузкой формы и сохранением кто-то другой уже обновил объект.
//
// Вызывающий код (например, HTTP-handler формы редактирования) должен
// перехватывать через errors.Is и показывать пользователю сообщение
// «Объект был изменён другим пользователем, обновите форму».
var ErrVersionConflict = errors.New("storage: объект изменён другим пользователем")

// UpsertVersioned записывает объект с проверкой оптимистической блокировки.
//
// Если expectedVersion == nil — поведение идентично Upsert (без проверки).
// Используется для записи новых объектов из UI и для DSL-кода
// (Документы.X.Создать().Записать()), где код авторитативный.
//
// Если expectedVersion != nil — перед записью проверяется, что текущая
// ревизия объекта в БД совпадает с ожидаемой. Если объект уже изменён
// (другим пользователем или фоновой задачей) — возвращается
// ErrVersionConflict, ничего не пишется.
//
// Вызов обязан выполняться внутри транзакции (см. WithTx) — иначе SELECT
// и UPDATE могут разорваться, и между ними кто-то ещё успеет записать.
// Для SQLite одиночный пишущий коннект гарантирует сериализацию даже без
// FOR UPDATE; для PostgreSQL транзакция со стандартным READ COMMITTED
// гарантирует, что повторный SELECT после UPDATE не увидит чужих
// промежуточных изменений в той же транзакции.
func (db *DB) UpsertVersioned(ctx context.Context, entityName string, id uuid.UUID, fields map[string]any, entity *metadata.Entity, expectedVersion *int64) error {
	if expectedVersion == nil {
		return db.Upsert(ctx, entityName, id, fields, entity)
	}

	d := db.dialect
	table := metadata.TableName(entityName)

	var current int64
	sql := fmt.Sprintf("SELECT _version FROM %s WHERE id = %s", table, d.Placeholder(1))
	if err := db.QueryRow(ctx, sql, idArg(d, id)).Scan(&current); err != nil {
		// Запись могла быть удалена другим пользователем — тоже конфликт.
		return ErrVersionConflict
	}
	if current != *expectedVersion {
		return ErrVersionConflict
	}
	return db.Upsert(ctx, entityName, id, fields, entity)
}
