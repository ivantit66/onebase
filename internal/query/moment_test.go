package query_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
	"github.com/ivantit66/onebase/internal/storage"
)

// momentValue реализует интерфейс query.momentTimeValue для теста —
// без импорта runtime (избегаем цикла).
type momentValue struct {
	period time.Time
	docID  string
}

func (m *momentValue) PointInTime() (time.Time, string) { return m.period, m.docID }

// Замечание #1: .Остатки(МоментВремени) должна давать period < @ OR (period = @ AND recorder != @doc).
func TestCompile_MomentTime_Balances(t *testing.T) {
	src := `ВЫБРАТЬ Номенклатура, КоличествоОстаток
ИЗ РегистрНакопления.ОстаткиТоваров.Остатки(&МВ)`
	reg := &metadata.Register{
		Name: "ОстаткиТоваров",
		Dimensions: []metadata.Field{
			{Name: "Номенклатура", Type: "string"},
		},
		Resources: []metadata.Field{
			{Name: "Количество", Type: "number"},
		},
	}
	mt := &momentValue{
		period: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
		docID:  "11111111-1111-1111-1111-111111111111",
	}
	r, err := query.Compile(src, query.CompileOpts{
		Registers: []*metadata.Register{reg},
		Params:    map[string]any{"МВ": mt},
		Dialect:   storage.SQLiteDialect{},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Должно быть условие на period < и recorder !=
	if !strings.Contains(r.SQL, "period <") || !strings.Contains(r.SQL, "recorder !=") {
		t.Errorf("ожидалось «period < ... AND recorder != ...» в SQL:\n%s", r.SQL)
	}
	// Должно быть 2 args: period + docID
	if len(r.Args) != 2 {
		t.Errorf("ожидалось 2 args, получили %d: %v", len(r.Args), r.Args)
	}
}

// МоментВремени без docID — упрощённое period <= ...
func TestCompile_MomentTime_NoDocFallback(t *testing.T) {
	src := `ВЫБРАТЬ Номенклатура, КоличествоОстаток
ИЗ РегистрНакопления.ОстаткиТоваров.Остатки(&МВ)`
	reg := &metadata.Register{
		Name:       "ОстаткиТоваров",
		Dimensions: []metadata.Field{{Name: "Номенклатура", Type: "string"}},
		Resources:  []metadata.Field{{Name: "Количество", Type: "number"}},
	}
	mt := &momentValue{
		period: time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		docID:  "", // нет документа
	}
	r, err := query.Compile(src, query.CompileOpts{
		Registers: []*metadata.Register{reg},
		Params:    map[string]any{"МВ": mt},
		Dialect:   storage.SQLiteDialect{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.SQL, "period <= ") {
		t.Errorf("без docID ожидалось period <= ...: %s", r.SQL)
	}
	if strings.Contains(r.SQL, "recorder") {
		t.Errorf("без docID recorder не должен упоминаться: %s", r.SQL)
	}
}

// Обычный параметр-дата (не moment) работает как раньше: period <= ...
func TestCompile_PlainDate_StillWorks(t *testing.T) {
	src := `ВЫБРАТЬ Номенклатура, КоличествоОстаток
ИЗ РегистрНакопления.ОстаткиТоваров.Остатки(&Дата)`
	reg := &metadata.Register{
		Name:       "ОстаткиТоваров",
		Dimensions: []metadata.Field{{Name: "Номенклатура", Type: "string"}},
		Resources:  []metadata.Field{{Name: "Количество", Type: "number"}},
	}
	r, err := query.Compile(src, query.CompileOpts{
		Registers: []*metadata.Register{reg},
		Params:    map[string]any{"Дата": "2026-05-20"},
		Dialect:   storage.SQLiteDialect{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.SQL, "period <= ") {
		t.Errorf("plain date: ожидалось period <= ...: %s", r.SQL)
	}
	if strings.Contains(r.SQL, "recorder") {
		t.Errorf("plain date: recorder не должен упоминаться: %s", r.SQL)
	}
}

// МоментВремени для info-регистра — берёт только Period, без recorder
// (info-reg не имеет recorder колонки в этом контексте).
func TestCompile_MomentTime_InfoSlice(t *testing.T) {
	src := `ВЫБРАТЬ Цена ИЗ РегистрСведений.ЦеныНоменклатуры.СрезПоследних(&МВ)`
	ir := &metadata.InfoRegister{
		Name:     "ЦеныНоменклатуры",
		Periodic: true,
		Dimensions: []metadata.Field{
			{Name: "Номенклатура", Type: "string"},
		},
		Resources: []metadata.Field{
			{Name: "Цена", Type: "number"},
		},
	}
	mt := &momentValue{
		period: time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		docID:  uuid.New().String(),
	}
	r, err := query.Compile(src, query.CompileOpts{
		InfoRegs: []*metadata.InfoRegister{ir},
		Params:   map[string]any{"МВ": mt},
		Dialect:  storage.SQLiteDialect{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.SQL, "period <=") {
		t.Errorf("info-slice ожидалось period <= ...: %s", r.SQL)
	}
	// recorder для info-reg не нужен
	if strings.Contains(r.SQL, "recorder !=") {
		t.Errorf("info-slice не должен использовать recorder: %s", r.SQL)
	}
}
