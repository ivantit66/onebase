package query_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
	"github.com/ivantit66/onebase/internal/storage"
)

func testReg() *metadata.Register {
	return &metadata.Register{
		Name: "ТоварноеДвижение",
		Dimensions: []metadata.Field{
			{Name: "Номенклатура"},
			{Name: "Склад"},
		},
		Resources: []metadata.Field{
			{Name: "Количество"},
			{Name: "Сумма"},
		},
	}
}

func testInfoReg(periodic bool) *metadata.InfoRegister {
	return &metadata.InfoRegister{
		Name:     "КурсыВалют",
		Periodic: periodic,
		Dimensions: []metadata.Field{
			{Name: "Валюта"},
		},
		Resources: []metadata.Field{
			{Name: "Курс"},
		},
	}
}

func TestCompile_Balances_NoDate(t *testing.T) {
	src := `ВЫБРАТЬ Номенклатура, КоличествоОстаток
ИЗ РегистрНакопления.ТоварноеДвижение.Остатки()`

	r, err := query.Compile(src, query.CompileOpts{
		Registers: []*metadata.Register{testReg()},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := r.SQL
	if !strings.Contains(sql, "SUM(CASE WHEN вид_движения = 'Приход' THEN количество ELSE -количество END) AS количествоостаток") {
		t.Errorf("missing balance SUM for количество, got:\n%s", sql)
	}
	if !strings.Contains(sql, "FROM рег_товарноедвижение") {
		t.Errorf("missing FROM рег_товарноедвижение, got:\n%s", sql)
	}
	if !strings.Contains(sql, "GROUP BY номенклатура, склад") {
		t.Errorf("missing GROUP BY, got:\n%s", sql)
	}
	if strings.Contains(sql, "WHERE") {
		t.Errorf("unexpected WHERE clause when no date given, got:\n%s", sql)
	}
}

func TestCompile_Balances_WithDate(t *testing.T) {
	d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src := `ВЫБРАТЬ Номенклатура, КоличествоОстаток
ИЗ РегистрНакопления.ТоварноеДвижение.Остатки(&НаДату)`

	r, err := query.Compile(src, query.CompileOpts{
		Params:    map[string]any{"НаДату": d},
		Registers: []*metadata.Register{testReg()},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := r.SQL
	if !strings.Contains(sql, "period <= $1::timestamptz") {
		t.Errorf("missing date condition, got:\n%s", sql)
	}
	if len(r.Args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(r.Args))
	}
}

func TestCompile_Balances_WithDateAndFilter(t *testing.T) {
	d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src := `ВЫБРАТЬ Номенклатура, КоличествоОстаток
ИЗ РегистрНакопления.ТоварноеДвижение.Остатки(&НаДату, Склад = &Склад)`

	r, err := query.Compile(src, query.CompileOpts{
		Params:    map[string]any{"НаДату": d, "Склад": "Основной"},
		Registers: []*metadata.Register{testReg()},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := r.SQL
	if !strings.Contains(sql, "period <= $1") {
		t.Errorf("missing date condition, got:\n%s", sql)
	}
	if !strings.Contains(sql, "склад = $2") {
		t.Errorf("missing filter condition, got:\n%s", sql)
	}
	if len(r.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(r.Args))
	}
}

func TestCompile_Turnovers(t *testing.T) {
	d1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	src := `ВЫБРАТЬ Номенклатура, КоличествоПриход, КоличествоРасход
ИЗ РегистрНакопления.ТоварноеДвижение.Обороты(&Начало, &Конец)`

	r, err := query.Compile(src, query.CompileOpts{
		Params:    map[string]any{"Начало": d1, "Конец": d2},
		Registers: []*metadata.Register{testReg()},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := r.SQL
	if !strings.Contains(sql, "SUM(CASE WHEN вид_движения = 'Приход' THEN количество ELSE 0 END) AS количествоприход") {
		t.Errorf("missing приход column, got:\n%s", sql)
	}
	if !strings.Contains(sql, "SUM(CASE WHEN вид_движения = 'Расход' THEN количество ELSE 0 END) AS количестворасход") {
		t.Errorf("missing расход column, got:\n%s", sql)
	}
	if !strings.Contains(sql, "period >= $1") {
		t.Errorf("missing start condition, got:\n%s", sql)
	}
	if !strings.Contains(sql, "period <= $2") {
		t.Errorf("missing end condition, got:\n%s", sql)
	}
	if len(r.Args) != 2 {
		t.Errorf("expected 2 args, got %d: %v", len(r.Args), r.Args)
	}
}

func TestCompile_Turnovers_Oborot(t *testing.T) {
	src := `ВЫБРАТЬ Номенклатура, КоличествоОборот
ИЗ РегистрНакопления.ТоварноеДвижение.Обороты()`

	r, err := query.Compile(src, query.CompileOpts{
		Registers: []*metadata.Register{testReg()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.SQL, "SUM(CASE WHEN вид_движения = 'Приход' THEN количество ELSE -количество END) AS количествооборот") {
		t.Errorf("missing оборот column, got:\n%s", r.SQL)
	}
}

func TestCompile_LastSlice_Periodic_SQLite(t *testing.T) {
	d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src := `ВЫБРАТЬ Валюта, Курс
ИЗ РегистрСведений.КурсыВалют.СрезПоследних(&НаДату)`

	r, err := query.Compile(src, query.CompileOpts{
		Params:   map[string]any{"НаДату": d},
		InfoRegs: []*metadata.InfoRegister{testInfoReg(true)},
		Dialect:  storage.SQLiteDialect{},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := r.SQL
	if strings.Contains(sql, "DISTINCT ON") {
		t.Errorf("SQLite: should NOT use DISTINCT ON, got:\n%s", sql)
	}
	if !strings.Contains(sql, "ROW_NUMBER() OVER (PARTITION BY валюта") {
		t.Errorf("SQLite: missing ROW_NUMBER() OVER, got:\n%s", sql)
	}
	if !strings.Contains(sql, "WHERE _rn = 1") {
		t.Errorf("SQLite: missing rn=1 filter, got:\n%s", sql)
	}
	if !strings.Contains(sql, "period <= ?") {
		t.Errorf("SQLite: should use ? placeholder, got:\n%s", sql)
	}
}

func TestCompile_LastSlice_Periodic(t *testing.T) {
	d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src := `ВЫБРАТЬ Валюта, Курс
ИЗ РегистрСведений.КурсыВалют.СрезПоследних(&НаДату)`

	r, err := query.Compile(src, query.CompileOpts{
		Params:   map[string]any{"НаДату": d},
		InfoRegs: []*metadata.InfoRegister{testInfoReg(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := r.SQL
	if !strings.Contains(sql, "DISTINCT ON") {
		t.Errorf("expected DISTINCT ON for periodic, got:\n%s", sql)
	}
	if !strings.Contains(sql, "FROM инфо_курсывалют") {
		t.Errorf("missing FROM инфо_курсывалют, got:\n%s", sql)
	}
	if !strings.Contains(sql, "period <= $1") {
		t.Errorf("missing date condition, got:\n%s", sql)
	}
	if !strings.Contains(sql, "ORDER BY валюта, period DESC") {
		t.Errorf("missing ORDER BY, got:\n%s", sql)
	}
}

func TestCompile_LastSlice_NonPeriodic(t *testing.T) {
	src := `ВЫБРАТЬ Валюта, Курс
ИЗ РегистрСведений.КурсыВалют.СрезПоследних()`

	r, err := query.Compile(src, query.CompileOpts{
		InfoRegs: []*metadata.InfoRegister{testInfoReg(false)},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := r.SQL
	if strings.Contains(sql, "DISTINCT ON") {
		t.Errorf("unexpected DISTINCT ON for non-periodic, got:\n%s", sql)
	}
	if strings.Contains(sql, "ORDER BY") {
		t.Errorf("unexpected ORDER BY for non-periodic, got:\n%s", sql)
	}
	if !strings.Contains(sql, "FROM инфо_курсывалют") {
		t.Errorf("missing FROM, got:\n%s", sql)
	}
}

func TestCompile_LastSlice_WithFilter(t *testing.T) {
	d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src := `ВЫБРАТЬ Курс ИЗ РегистрСведений.КурсыВалют.СрезПоследних(&НаДату, Валюта = &Вал)`

	r, err := query.Compile(src, query.CompileOpts{
		Params:   map[string]any{"НаДату": d, "Вал": "USD"},
		InfoRegs: []*metadata.InfoRegister{testInfoReg(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := r.SQL
	if !strings.Contains(sql, "валюта = $2") {
		t.Errorf("missing filter, got:\n%s", sql)
	}
	if len(r.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(r.Args))
	}
}

func TestCompile_InfoReg_Direct(t *testing.T) {
	// РегистрСведений.X without virtual table → инфо_ prefix
	src := `ВЫБРАТЬ Валюта, Курс ИЗ РегистрСведений.КурсыВалют`
	r, err := query.Compile(src, query.CompileOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.SQL, "инфо_курсывалют") {
		t.Errorf("expected инфо_курсывалют, got: %s", r.SQL)
	}
}

func TestCompile_Balances_RefDim_Aliased(t *testing.T) {
	// Register with a reference dimension — ColumnName returns "номенклатура_id".
	// The VT subquery must alias it back to "номенклатура" so DSL code like
	// Стр.Номенклатура resolves correctly, and outer WHERE uses the logical name.
	reg := &metadata.Register{
		Name: "ПартииТоваров",
		Dimensions: []metadata.Field{
			{Name: "Номенклатура", RefEntity: "Номенклатура"},
			{Name: "ДатаПоставки"},
		},
		Resources: []metadata.Field{
			{Name: "Количество"},
			{Name: "Сумма"},
		},
	}
	src := `ВЫБРАТЬ Номенклатура, КоличествоОстаток
ИЗ РегистрНакопления.ПартииТоваров.Остатки()
ГДЕ Номенклатура В (&СписокНом)`

	r, err := query.Compile(src, query.CompileOpts{
		Params:    map[string]any{"СписокНом": []any{"uuid-1", "uuid-2"}},
		Registers: []*metadata.Register{reg},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := r.SQL
	// VT subquery must alias the _id column back to the logical field name.
	if !strings.Contains(sql, "номенклатура_id AS номенклатура") {
		t.Errorf("expected 'номенклатура_id AS номенклатура' in VT subquery, got:\n%s", sql)
	}
	// GROUP BY must use the actual DB column name.
	if !strings.Contains(sql, "GROUP BY номенклатура_id") {
		t.Errorf("expected 'GROUP BY номенклатура_id', got:\n%s", sql)
	}
	// Outer WHERE must reference the aliased DSL name, not _id.
	if !strings.Contains(sql, "WHERE номенклатура IN") {
		t.Errorf("expected outer 'WHERE номенклатура IN', got:\n%s", sql)
	}
	if strings.Contains(sql, "WHERE номенклатура_id IN") {
		t.Errorf("outer WHERE should use aliased 'номенклатура', not 'номенклатура_id', got:\n%s", sql)
	}
}

func TestCompile_MissingRegister_Error(t *testing.T) {
	src := `ВЫБРАТЬ Ном ИЗ РегистрНакопления.Неизвестный.Остатки()`
	_, err := query.Compile(src, query.CompileOpts{})
	if err == nil {
		t.Error("expected error for unknown register")
	}
	if !strings.Contains(err.Error(), "Неизвестный") {
		t.Errorf("error should mention register name, got: %v", err)
	}
}
