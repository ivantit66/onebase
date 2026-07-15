package exchange

// Константы в составе обмена (план 86, фаза 2). Константа — единственное глобальное
// значение без _version, поэтому идемпотентность и порядок доставки на приёмнике
// опираются на «водяной знак» _exchange_applied (аналог сравнения _version у
// сущностей), а не на ревизию объекта. Встречная правка (конфликт) обнаруживается
// по неотправленной строке очереди источнику и разрешается правилом плана.

import (
	"context"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// applyConstant идемпотентно применяет константу из пакета.
func applyConstant(ctx context.Context, store *storage.DB, plan *metadata.ExchangePlan, thisNode, fromNode string, obj PackageObject, res *LoadResult) error {
	name := obj.Type
	var value any
	if obj.Fields != nil {
		value = obj.Fields["value"]
	}

	// Встречная правка: приёмник менял ту же константу и ещё не отправил источнику.
	local, hasLocal, err := store.GetExchangeChange(ctx, plan.Name, name, "", fromNode)
	if err != nil {
		return err
	}
	if hasLocal {
		res.Conflicts++
		if !resolveScalarConflict(plan, thisNode, fromNode, obj.ChangedAt, local.ChangedAt) {
			res.Skipped++ // локальное изменение победило
			return nil
		}
		return applyConstantValue(ctx, store, plan.Name, name, value, obj.ChangedAt, res)
	}

	// Нет встречной правки — идемпотентность по «водяному знаку».
	if at, ok := store.ExchangeAppliedAt(ctx, plan.Name, name, ""); ok && obj.ChangedAt <= at {
		res.Skipped++
		return nil
	}
	return applyConstantValue(ctx, store, plan.Name, name, value, obj.ChangedAt, res)
}

func applyConstantValue(ctx context.Context, store *storage.DB, plan, name string, value any, changedAt int64, res *LoadResult) error {
	if err := store.SetConstant(ctx, name, value); err != nil {
		return err
	}
	if err := store.SetExchangeApplied(ctx, plan, name, "", changedAt); err != nil {
		return err
	}
	res.Applied++
	return nil
}
