package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/metadata"
)

// Итоги регистра накопления (план 80, этап 1): таблица итоги_<рег> хранит
// текущий чистый знаковый остаток ресурсов по каждому набору измерений.
// Поддерживается в той же транзакции, что и движения (WriteMovements), поэтому
// всегда согласована с рег_<рег>. Знаковая сумма зеркалит query.genBalances
// дословно (`SUM(CASE WHEN вид_движения='Приход' THEN r ELSE -r END)`), чтобы
// быстрый путь Остатки() давал тот же результат, что расчёт на лету.
//
// Итоги независимы от времени: ускоряют текущие Остатки() (без момента) с
// O(все движения) до O(число комбинаций измерений) и корректны при проведении
// задним числом. Остатки на дату/ОстаткиИОбороты (периодические итоги) — этап 2.

// signedResourceSum возвращает выражение знаковой суммы ресурса (как в
// genBalances). alias — псевдоним колонки в INSERT ... SELECT.
func signedResourceSum(col string) string {
	return "SUM(CASE WHEN вид_движения = 'Приход' THEN " + col + " ELSE -" + col + " END)"
}

// CreateRegisterTotalsSQL — DDL таблицы итогов: измерения (ключ) + ресурсы
// (числовой остаток). Ресурсы объявлены NUMERIC, чтобы на SQLite, где сырые
// колонки регистра могут иметь TEXT-аффинность, хранить именно числа.
func CreateRegisterTotalsSQL(d Dialect, reg *metadata.Register) string {
	table := metadata.RegisterTotalsTableName(reg.Name)
	var cols []string
	for _, f := range reg.Dimensions {
		cols = append(cols, metadata.ColumnName(f)+" "+fieldType(d, f))
	}
	for _, f := range reg.Resources {
		cols = append(cols, metadata.ColumnName(f)+" NUMERIC")
	}
	var sb strings.Builder
	sb.WriteString("CREATE TABLE IF NOT EXISTS ")
	sb.WriteString(table)
	sb.WriteString(" (\n    ")
	sb.WriteString(strings.Join(cols, ",\n    "))
	if len(reg.Dimensions) > 0 {
		sb.WriteString(",\n    PRIMARY KEY (")
		sb.WriteString(strings.Join(dimColNames(reg.Dimensions), ", "))
		sb.WriteString(")")
	}
	sb.WriteString("\n)")
	return sb.String()
}

func dimColNames(dims []metadata.Field) []string {
	names := make([]string, len(dims))
	for i, f := range dims {
		names[i] = metadata.ColumnName(f)
	}
	return names
}

// insertTotalsSelectSQL строит `INSERT INTO итоги (dims, res) SELECT dims,
// signedSum(res) FROM рег [WHERE ...] [GROUP BY dims] HAVING COUNT(*)>0`.
// where — уже готовое условие отбора (по кортежу измерений) или пусто.
func insertTotalsSelectSQL(reg *metadata.Register, where string) string {
	totals := metadata.RegisterTotalsTableName(reg.Name)
	src := metadata.RegisterTableName(reg.Name)
	dims := dimColNames(reg.Dimensions)

	insertCols := append([]string{}, dims...)
	selectCols := append([]string{}, dims...)
	for _, f := range reg.Resources {
		col := metadata.ColumnName(f)
		insertCols = append(insertCols, col)
		selectCols = append(selectCols, signedResourceSum(col))
	}

	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(totals)
	sb.WriteString(" (")
	sb.WriteString(strings.Join(insertCols, ", "))
	sb.WriteString(") SELECT ")
	sb.WriteString(strings.Join(selectCols, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(src)
	if where != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(where)
	}
	if len(dims) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(dims, ", "))
	}
	// HAVING COUNT(*)>0 отсекает пустой агрегат (для регистра без измерений
	// SELECT по пустому источнику вернул бы одну строку из NULL).
	sb.WriteString(" HAVING COUNT(*) > 0")
	return sb.String()
}

// ensureRegisterTotals создаёт таблицу итогов и наполняет её при первом
// включении. Если итоги уже ведутся (строки есть) — только создаёт таблицу
// (idempotent). Если итоги пусты, а движения есть — полный пересчёт (переход
// существующего регистра на итоги).
func (db *DB) ensureRegisterTotals(ctx context.Context, reg *metadata.Register) error {
	if _, err := db.Exec(ctx, CreateRegisterTotalsSQL(db.dialect, reg)); err != nil {
		return fmt.Errorf("create totals table %s: %w", reg.Name, err)
	}
	var totalsCount int
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM "+metadata.RegisterTotalsTableName(reg.Name)).Scan(&totalsCount); err != nil {
		return fmt.Errorf("count totals %s: %w", reg.Name, err)
	}
	if totalsCount > 0 {
		return nil
	}
	var movesCount int
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM "+metadata.RegisterTableName(reg.Name)).Scan(&movesCount); err != nil {
		return fmt.Errorf("count movements %s: %w", reg.Name, err)
	}
	if movesCount == 0 {
		return nil
	}
	return db.RecalcRegisterTotals(ctx, reg)
}

// RecalcRegisterTotals полностью пересчитывает итоги регистра из движений.
// Идемпотентно: DELETE всех строк + INSERT SELECT. Используется командой
// recalc-totals и при первом включении итогов (миграция).
func (db *DB) RecalcRegisterTotals(ctx context.Context, reg *metadata.Register) error {
	if !reg.TotalsEnabled() {
		return nil
	}
	totals := metadata.RegisterTotalsTableName(reg.Name)
	if err := db.exec(ctx, "DELETE FROM "+totals); err != nil {
		return fmt.Errorf("recalc totals %s: clear: %w", reg.Name, err)
	}
	if err := db.exec(ctx, insertTotalsSelectSQL(reg, "")); err != nil {
		return fmt.Errorf("recalc totals %s: fill: %w", reg.Name, err)
	}
	return nil
}

// dimTupleWhere строит условие отбора одного кортежа измерений
// (`d1 = ? AND d2 IS NULL AND ...`), начиная с плейсхолдера startPh.
func dimTupleWhere(d Dialect, dims []metadata.Field, tuple []any, startPh int) (string, []any) {
	var conds []string
	var args []any
	ph := startPh
	for i, f := range dims {
		col := metadata.ColumnName(f)
		if i >= len(tuple) || tuple[i] == nil {
			conds = append(conds, col+" IS NULL")
			continue
		}
		conds = append(conds, col+" = "+d.Placeholder(ph))
		args = append(args, tuple[i])
		ph++
	}
	return strings.Join(conds, " AND "), args
}

// distinctDimTuples возвращает уникальные кортежи измерений движений
// регистратора (для определения затронутых итогов). Значения берутся из самой
// таблицы рег_* — уже в нормализованном виде, что совпадает с ключом итогов.
func (db *DB) distinctDimTuples(ctx context.Context, reg *metadata.Register, recorderType string, recorderID any) ([][]any, error) {
	if len(reg.Dimensions) == 0 {
		return [][]any{{}}, nil // единственный кортеж — «без измерений»
	}
	d := db.dialect
	cols := dimColNames(reg.Dimensions)
	sql := fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE recorder = %s AND recorder_type = %s",
		strings.Join(cols, ", "), metadata.RegisterTableName(reg.Name), d.Placeholder(1), d.Placeholder(2))
	rows, err := db.Query(ctx, sql, recorderID, recorderType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tuples [][]any
	for rows.Next() {
		dest := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		tuples = append(tuples, dest)
	}
	return tuples, rows.Err()
}

// recomputeTupleTotals пересчитывает строку итогов для одного кортежа измерений:
// удаляет её и заново считает из движений. Если движений для кортежа не осталось
// — строка просто исчезает (остаток 0 = отсутствие строки, как в genBalances).
func (db *DB) recomputeTupleTotals(ctx context.Context, reg *metadata.Register, tuple []any) error {
	totals := metadata.RegisterTotalsTableName(reg.Name)
	where, args := dimTupleWhere(db.dialect, reg.Dimensions, tuple, 1)
	delSQL := "DELETE FROM " + totals
	if where != "" {
		delSQL += " WHERE " + where
	}
	if err := db.exec(ctx, delSQL, args...); err != nil {
		return fmt.Errorf("totals %s: delete tuple: %w", reg.Name, err)
	}
	// INSERT ... SELECT с тем же условием кортежа. Плейсхолдеры условия идут
	// после — но условие ссылается на исходную таблицу, а значения те же.
	insWhere, insArgs := dimTupleWhere(db.dialect, reg.Dimensions, tuple, 1)
	if err := db.exec(ctx, insertTotalsSelectSQL(reg, insWhere), insArgs...); err != nil {
		return fmt.Errorf("totals %s: recompute tuple: %w", reg.Name, err)
	}
	return nil
}

// updateTotalsForRecorder поддерживает итоги после замены движений
// регистратора: пересчитывает кортежи измерений, затронутые старыми (oldTuples,
// снятыми до удаления) и новыми движениями. Вызывается из WriteMovements внутри
// той же транзакции.
func (db *DB) updateTotalsForRecorder(ctx context.Context, reg *metadata.Register, recorderType string, recorderID any, oldTuples [][]any) error {
	newTuples, err := db.distinctDimTuples(ctx, reg, recorderType, recorderID)
	if err != nil {
		return fmt.Errorf("totals %s: new tuples: %w", reg.Name, err)
	}
	for _, t := range dedupTuples(append(oldTuples, newTuples...)) {
		if err := db.recomputeTupleTotals(ctx, reg, t); err != nil {
			return err
		}
	}
	return nil
}

// dedupTuples убирает повторяющиеся кортежи (по строковому представлению
// значений). Порядок значений в кортеже фиксирован (порядок измерений).
func dedupTuples(tuples [][]any) [][]any {
	seen := make(map[string]bool, len(tuples))
	var out [][]any
	for _, t := range tuples {
		key := fmt.Sprintf("%v", t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}
