package exchange

// Разрешение конфликтов версий при загрузке пакета (план 86). Конфликт —
// встречная правка: один и тот же объект изменён и на источнике, и на приёмнике
// со времени последней синхронизации. Правило задаётся в плане (conflict):
//   - by_time          — побеждает позже изменённый (по changed_at);
//   - by_node_priority — побеждает узел с большим priority;
//   - hook             — выбор делает DSL-обработчик ПриКонфликтеОбмена.

import (
	"context"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// HookResolver разрешает конфликт правилом hook (DSL ПриКонфликтеОбмена).
// Реализуется на уровне, где доступен интерпретатор (ui/cli); движок обмена от
// DSL не зависит. Возвращает true, если победило входящее изменение.
type HookResolver interface {
	ResolveConflict(ctx context.Context, plan *metadata.ExchangePlan, entity *metadata.Entity, local, incoming *PackageObject) (incomingWins bool, err error)
}

// ApplyOptions — необязательные параметры загрузки пакета.
type ApplyOptions struct {
	// Hook — обработчик правила conflict: hook. nil (например, headless-загрузка
	// без интерпретатора) → правило hook откатывается к by_time.
	Hook HookResolver
}

// resolveConflict решает, победит ли входящее изменение над локальным.
func resolveConflict(ctx context.Context, store *storage.DB, plan *metadata.ExchangePlan, thisNode, fromNode string, ent *metadata.Entity, id uuid.UUID, incoming PackageObject, localChangedAt int64, hook HookResolver) (bool, error) {
	switch plan.Conflict {
	case metadata.ConflictByNodePriority:
		from, to := plan.Node(fromNode), plan.Node(thisNode)
		if from != nil && to != nil {
			return from.Priority > to.Priority, nil
		}
		// Узлы не описаны — деградируем к сравнению по времени.
		return incoming.ChangedAt > localChangedAt, nil
	case metadata.ConflictByHook:
		if hook != nil {
			local, err := readLocalObject(ctx, store, ent, id)
			if err != nil {
				return false, err
			}
			return hook.ResolveConflict(ctx, plan, ent, local, &incoming)
		}
		// Хук не подключён — деградируем к by_time (загрузка не должна падать).
		return incoming.ChangedAt > localChangedAt, nil
	default: // by_time и всё неизвестное
		return incoming.ChangedAt > localChangedAt, nil
	}
}

// readLocalObject собирает локальную версию объекта как PackageObject для
// передачи в DSL-хук разрешения конфликта.
func readLocalObject(ctx context.Context, store *storage.DB, ent *metadata.Entity, id uuid.UUID) (*PackageObject, error) {
	row, err := store.GetByID(ctx, ent.Name, id, ent)
	if err != nil {
		return nil, err
	}
	version, _ := store.EntityVersionExists(ctx, ent.Name, id)
	obj := &PackageObject{
		Type:    ent.Name,
		ID:      id.String(),
		Version: version,
		Fields:  canonicalHeader(ent, row),
	}
	for _, tp := range ent.TableParts {
		rows, err := store.GetTablePartRows(ctx, ent.Name, tp.Name, id, tp)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		canon := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			canon = append(canon, canonicalRow(tp.Fields, r))
		}
		if obj.TableParts == nil {
			obj.TableParts = map[string][]map[string]any{}
		}
		obj.TableParts[tp.Name] = canon
	}
	return obj, nil
}
