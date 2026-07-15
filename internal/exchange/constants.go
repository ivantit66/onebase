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

// applyConstant идемпотентно применяет константу из пакета. Возвращает true, если
// значение было записано (для транзитной ретрансляции хабом).
func applyConstant(ctx context.Context, store *storage.DB, plan *metadata.ExchangePlan, thisNode, fromNode string, obj PackageObject, res *LoadResult) (bool, error) {
	name := obj.Type
	var value any
	if obj.Fields != nil {
		value = obj.Fields["value"]
	}

	// Встречная правка: приёмник менял ту же константу и ещё не отправил источнику.
	local, hasLocal, err := store.GetExchangeChange(ctx, plan.Name, name, "", fromNode)
	if err != nil {
		return false, err
	}
	if hasLocal {
		res.Conflicts++
		if !resolveScalarConflict(plan, thisNode, fromNode, obj.ChangedAt, local.ChangedAt) {
			res.Skipped++ // локальное изменение победило
			return false, nil
		}
		if err := applyConstantValue(ctx, store, plan.Name, name, value, obj.ChangedAt, res); err != nil {
			return false, err
		}
		if err := store.DeleteExchangeChange(ctx, plan.Name, name, "", fromNode); err != nil {
			return false, err
		}
		return true, nil
	}

	// Нет встречной правки — идемпотентность по «водяному знаку».
	at, ok, err := store.ExchangeAppliedAt(ctx, plan.Name, name, "")
	if err != nil {
		return false, err
	}
	if ok && obj.ChangedAt <= at {
		res.Skipped++
		return false, nil
	}
	return true, applyConstantValue(ctx, store, plan.Name, name, value, obj.ChangedAt, res)
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
