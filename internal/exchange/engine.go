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

// validateExchangePlan допускает прямой обмен пары баз и многоузловую звезду с
// единственным хабом. Плоский обмен трёх и более узлов со скалярной версией
// объекта небезопасен: конкурентные изменения не имеют единого координатора.
func validateExchangePlan(plan *metadata.ExchangePlan) error {
	if plan == nil {
		return fmt.Errorf("exchange: plan is nil")
	}
	if len(plan.Nodes) < 2 {
		return fmt.Errorf("exchange: план %q должен содержать минимум два узла", plan.Name)
	}
	seen := make(map[string]struct{}, len(plan.Nodes))
	hubs := 0
	for _, node := range plan.Nodes {
		code := strings.ToLower(strings.TrimSpace(node.Code))
		if code == "" {
			return fmt.Errorf("exchange: план %q содержит узел без кода", plan.Name)
		}
		if _, ok := seen[code]; ok {
			return fmt.Errorf("exchange: план %q содержит повторяющийся код узла %q", plan.Name, node.Code)
		}
		seen[code] = struct{}{}
		switch node.Role {
		case "", metadata.RoleSpoke:
		case metadata.RoleHub:
			hubs++
		default:
			return fmt.Errorf("exchange: план %q содержит неизвестную роль узла %q", plan.Name, node.Role)
		}
	}
	if hubs > 1 {
		return fmt.Errorf("exchange: план %q должен содержать не более одного хаба", plan.Name)
	}
	if len(plan.Nodes) > 2 && hubs != 1 {
		return fmt.Errorf("exchange: план %q с тремя и более узлами должен использовать топологию с одним хабом", plan.Name)
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
		if err := validateExchangePlan(plan); err != nil {
			return err
		}
		thisNode, err := store.GetExchangeThisNode(ctx, plan.Name)
		if err != nil {
			return err
		}
		if thisNode == "" {
			continue // база не участвует в этом плане обмена
		}
		thisNodeDef := plan.Node(thisNode)
		if thisNodeDef == nil {
			return fmt.Errorf("exchange: текущий узел %q не описан в плане %q", thisNode, plan.Name)
		}
		thisNode = thisNodeDef.Code
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
		// Кому регистрировать — по топологии плана (плоская: всем; звезда: спица →
		// только хабу, хаб → спицам). Источник исключён внутри RegistrationTargets.
		for _, target := range plan.RegistrationTargets(thisNode) {
			if err := store.RegisterExchangeChange(ctx, storage.ExchangeChange{
				Plan:       plan.Name,
				ObjectType: entity.Name,
				ObjectID:   id.String(),
				NodeCode:   target,
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

// RegisterConstantOnSave регистрирует изменение константы во всех планах обмена,
// чей состав включает её (Константа.X). Строка очереди ставится всем узлам плана,
// кроме текущего. Вызывается в транзакции записи константы.
//
// У констант нет _version: идемпотентность и by_time на приёмнике опираются на
// changed_at (момент записи) и «водяной знак» _exchange_applied. Version в строке
// очереди тоже несёт changed_at — только для единообразия с сущностным путём.
func RegisterConstantOnSave(ctx context.Context, store *storage.DB, plans []*metadata.ExchangePlan, name string) error {
	if store == nil || name == "" || len(plans) == 0 {
		return nil
	}
	changedAt := time.Now().UnixMilli()
	for _, plan := range plans {
		if !plan.IncludesConstant(name) {
			continue
		}
		if err := validateExchangePlan(plan); err != nil {
			return err
		}
		thisNode, err := store.GetExchangeThisNode(ctx, plan.Name)
		if err != nil {
			return err
		}
		if thisNode == "" {
			continue
		}
		thisNodeDef := plan.Node(thisNode)
		if thisNodeDef == nil {
			return fmt.Errorf("exchange: текущий узел %q не описан в плане %q", thisNode, plan.Name)
		}
		thisNode = thisNodeDef.Code
		for _, target := range plan.RegistrationTargets(thisNode) {
			if err := store.RegisterExchangeChange(ctx, storage.ExchangeChange{
				Plan:       plan.Name,
				ObjectType: name,
				ObjectID:   "", // единственное глобальное значение
				NodeCode:   target,
				Kind:       storage.ExchangeKindConstant,
				Version:    changedAt,
				ChangedAt:  changedAt,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
