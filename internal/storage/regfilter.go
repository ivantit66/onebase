package storage

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

// RegFilter — отбор для списков регистров: точные значения измерений
// (для ссылочных — UUID строкой, для прочих — строковое значение) и
// период от/до включительно (issue #45).
type RegFilter struct {
	Dims      map[string]string // имя измерения (как в метаданных) → значение
	From      *time.Time
	To        *time.Time
	RowFilter *Predicate // additional SQL-side row-level access predicate
}

// IsEmpty сообщает, задан ли хоть один критерий отбора.
func (f RegFilter) IsEmpty() bool {
	return len(f.Dims) == 0 && f.From == nil && f.To == nil
}

// dimWhereClause строит условия WHERE по измерениям регистра и периоду.
// Принимает только измерения, фактически принадлежащие dims (защита от
// инъекции имён колонок). Значения подставляются через плейсхолдеры.
// includeFrom/includeTo управляют включением границ периода (для остатков
// From игнорируется). startIdx — номер первого плейсхолдера (с 1).
func dimWhereClause(d Dialect, dims []metadata.Field, f RegFilter, startIdx int, includeFrom, includeTo bool) (string, []any) {
	var conds []string
	var args []any
	idx := startIdx

	for _, fld := range dims {
		val, ok := f.Dims[fld.Name]
		if !ok || val == "" {
			continue
		}
		col := metadata.ColumnName(fld)
		arg := any(val)
		// Для ссылочного измерения колонка хранит UUID — оборачиваем idArg,
		// чтобы PG получил uuid.UUID, а SQLite — строку (как при записи).
		if fld.RefEntity != "" {
			id, err := uuid.Parse(val)
			if err != nil {
				// Значение не UUID (например ручной ?Измерение=мусор в URL) —
				// ссылочная колонка хранит UUID, совпадений быть не может.
				// Подставляем заведомо ложное условие (пустой результат на обоих
				// диалектах), а не сырую строку: на PostgreSQL `col(uuid) = 'мусор'`
				// упал бы с 500 (invalid input syntax for uuid).
				conds = append(conds, "1=0")
				continue
			}
			arg = idArg(d, id)
		}
		conds = append(conds, fmt.Sprintf("%s = %s", col, d.Placeholder(idx)))
		args = append(args, arg)
		idx++
	}

	if includeFrom && f.From != nil {
		conds = append(conds, fmt.Sprintf("period >= %s", d.Placeholder(idx)))
		args = append(args, *f.From)
		idx++
	}
	if includeTo && f.To != nil {
		conds = append(conds, fmt.Sprintf("period <= %s", d.Placeholder(idx)))
		args = append(args, *f.To)
	}

	if len(conds) == 0 {
		return "", nil
	}
	return strings.Join(conds, " AND "), args
}
