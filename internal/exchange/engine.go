// Package exchange реализует планы обмена данными между базами OneBase
// (план 86): регистрацию изменений объектов из состава плана, сборку и разбор
// пакетов выгрузки/загрузки, разрешение конфликтов версий.
//
// Пакет — чистая логика поверх storage и metadata; он не знает про HTTP, CLI и
// DSL (их обвязка живёт в internal/cli, internal/ui, internal/dsl/interpreter).
package exchange

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// validatePairPlan deliberately limits the phase-1 protocol to a pair of
// nodes. A scalar object version cannot make concurrent edits converge through
// a third node; accepting larger topologies would silently corrupt replicas.
func validatePairPlan(plan *metadata.ExchangePlan) error {
	if plan == nil {
		return fmt.Errorf("exchange: plan is nil")
	}
	if len(plan.Nodes) != 2 {
		return fmt.Errorf("exchange: план %q должен содержать ровно два узла; многоузловой транзит этим форматом не поддерживается", plan.Name)
	}
	if strings.TrimSpace(plan.Nodes[0].Code) == "" || strings.TrimSpace(plan.Nodes[1].Code) == "" ||
		strings.EqualFold(plan.Nodes[0].Code, plan.Nodes[1].Code) {
		return fmt.Errorf("exchange: план %q содержит пустые или повторяющиеся коды узлов", plan.Name)
	}
	return nil
}

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
	return registerChange(ctx, store, plans, entity, id, deletion, false)
}

// RegisterOnDelete records a physical delete before the row disappears. It
// reserves the next object version so a peer that already has the current
// version applies the tombstone instead of treating it as an idempotent replay.
// The caller must execute this and storage.Delete in the same transaction.
func RegisterOnDelete(ctx context.Context, store *storage.DB, plans []*metadata.ExchangePlan, entity *metadata.Entity, id uuid.UUID) error {
	return registerChange(ctx, store, plans, entity, id, true, true)
}

func registerChange(ctx context.Context, store *storage.DB, plans []*metadata.ExchangePlan, entity *metadata.Entity, id uuid.UUID, deletion, nextVersion bool) error {
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
		if err := validatePairPlan(plan); err != nil {
			return err
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
			if nextVersion {
				version++
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
