package exchange

// Регистры сведений в составе обмена (план 86, фаза 2). Синхронизируются
// НЕЗАВИСИМЫЕ записи регистра сведений (введённые формой регистра, без recorder-
// документа; движения проведения — это фаза с переносом движений). Запись
// идентифицируется каноничным ключом измерений; у неё нет _version, поэтому
// идемпотентность и порядок опираются на «водяной знак» _exchange_applied.
//
// Периодические регистры пока не поддержаны: их ключ включает period (time.Time),
// а смешение форменной (локальная зона) и обменной (UTC) записи делает сравнение
// периода на SQLite ненадёжным. Такой состав пропускается регистрацией и
// помечается предупреждением configcheck.

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// RegisterInfoRegOnSave регистрирует изменение записи регистра сведений во всех
// планах обмена, где регистр в составе (РегистрСведений.X). deletion=true — запись
// удалена. Вызывается в транзакции записи/удаления записи из формы регистра.
func RegisterInfoRegOnSave(ctx context.Context, store *storage.DB, plans []*metadata.ExchangePlan, ir *metadata.InfoRegister, dims map[string]any, deletion bool) error {
	if store == nil || ir == nil || len(plans) == 0 || ir.Periodic {
		return nil
	}
	key := encodeInfoRegKey(ir, dims)
	changedAt := time.Now().UnixMilli()
	for _, plan := range plans {
		if !plan.IncludesInfoRegister(ir.Name) {
			continue
		}
		thisNode, err := store.GetExchangeThisNode(ctx, plan.Name)
		if err != nil {
			return err
		}
		if thisNode == "" {
			continue
		}
		for _, node := range plan.Nodes {
			if strings.EqualFold(node.Code, thisNode) {
				continue
			}
			if err := store.RegisterExchangeChange(ctx, storage.ExchangeChange{
				Plan:       plan.Name,
				ObjectType: ir.Name,
				ObjectID:   key,
				NodeCode:   node.Code,
				Kind:       storage.ExchangeKindInfoReg,
				Version:    changedAt,
				Deletion:   deletion,
				ChangedAt:  changedAt,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// encodeInfoRegKey строит детерминированный ключ записи из каноничных значений
// измерений (json.Marshal сортирует ключи map — порядок стабилен). Ключ служит и
// object_id строки очереди, и способом восстановить измерения при сборке пакета.
func encodeInfoRegKey(ir *metadata.InfoRegister, dims map[string]any) string {
	b, _ := json.Marshal(canonicalRow(ir.Dimensions, dims))
	return string(b)
}

func decodeInfoRegKey(key string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(key), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// applyInfoReg идемпотентно применяет запись регистра сведений из пакета.
// Конфликт (встречная неотправленная правка источнику) разрешается правилом плана
// (by_time/by_node_priority; hook к записи-без-структуры не применяется).
func applyInfoReg(ctx context.Context, store *storage.DB, resolver EntityResolver, plan *metadata.ExchangePlan, thisNode, fromNode string, obj PackageObject, res *LoadResult) error {
	ir := resolver.GetInfoRegister(obj.Type)
	if ir == nil || ir.Periodic {
		res.Skipped++ // регистр неизвестен приёмнику или периодический (не поддержан)
		return nil
	}
	dims := make(map[string]any, len(ir.Dimensions))
	for _, df := range ir.Dimensions {
		dims[df.Name] = obj.Fields[df.Name]
	}
	resources := make(map[string]any, len(ir.Resources))
	for _, rf := range ir.Resources {
		resources[rf.Name] = obj.Fields[rf.Name]
	}

	apply := func() error {
		if err := store.InfoRegApplyExchange(ctx, ir, dims, resources, nil, obj.Deletion); err != nil {
			return err
		}
		if err := store.SetExchangeApplied(ctx, plan.Name, obj.Type, obj.ID, obj.ChangedAt); err != nil {
			return err
		}
		if obj.Deletion {
			res.Deleted++
		} else {
			res.Applied++
		}
		return nil
	}

	local, hasLocal, err := store.GetExchangeChange(ctx, plan.Name, obj.Type, obj.ID, fromNode)
	if err != nil {
		return err
	}
	if hasLocal {
		res.Conflicts++
		if !resolveScalarConflict(plan, thisNode, fromNode, obj.ChangedAt, local.ChangedAt) {
			res.Skipped++ // локальное изменение победило
			return nil
		}
		return apply()
	}
	if at, ok := store.ExchangeAppliedAt(ctx, plan.Name, obj.Type, obj.ID); ok && obj.ChangedAt <= at {
		res.Skipped++
		return nil
	}
	return apply()
}
