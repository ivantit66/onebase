// Package exchange реализует планы обмена данными между базами OneBase
// (план 86): регистрацию изменений объектов из состава плана, сборку и разбор
// пакетов выгрузки/загрузки, разрешение конфликтов версий.
//
// Пакет — чистая логика поверх storage и metadata; он не знает про HTTP, CLI и
// DSL (их обвязка живёт в internal/cli, internal/ui, internal/dsl/interpreter).
package exchange

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// RegisterOnSave регистрирует изменение объекта во всех планах обмена, чей
// состав включает его сущность. Для каждого такого плана строка очереди
// (_exchange_changes) ставится всем узлам плана, кроме текущего (this_node).
// Если текущий узел для плана не задан (база не инициализирована как участник) —
// план пропускается.
//
// Вызывается внутри той же транзакции, что и запись объекта (entityservice.Save
// и путь пометки на удаление), поэтому ошибки возвращаются наверх: сбой
// регистрации должен откатывать запись, а не молча терять изменение из обмена.
func RegisterOnSave(ctx context.Context, store *storage.DB, plans []*metadata.ExchangePlan, entity *metadata.Entity, id uuid.UUID, deletion bool) error {
	if store == nil || entity == nil || len(plans) == 0 {
		return nil
	}
	// Версию объекта читаем не более одного раза — она общая для всех планов и
	// узлов; ленивая, чтобы не ходить в БД, если сущность не входит ни в один план.
	var version int64
	var haveVersion bool
	changedAt := time.Now().UnixMilli()

	for _, plan := range plans {
		if !plan.Includes(entity) {
			continue
		}
		thisNode, err := store.GetExchangeThisNode(ctx, plan.Name)
		if err != nil {
			return err
		}
		if thisNode == "" {
			continue // база не участвует в этом плане обмена
		}
		if !haveVersion {
			version, err = store.EntityVersion(ctx, entity.Name, id)
			if err != nil {
				return err
			}
			haveVersion = true
		}
		for _, node := range plan.Nodes {
			if strings.EqualFold(node.Code, thisNode) {
				continue // источнику изменения регистрировать не нужно
			}
			if err := store.RegisterExchangeChange(ctx, storage.ExchangeChange{
				Plan:       plan.Name,
				ObjectType: entity.Name,
				ObjectID:   id.String(),
				NodeCode:   node.Code,
				Version:    version,
				Deletion:   deletion,
				ChangedAt:  changedAt,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
