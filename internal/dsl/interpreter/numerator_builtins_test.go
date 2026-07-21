package interpreter

import (
	"context"
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

type fakeNumStore struct {
	seq    map[string]int
	legacy map[string]int64
}

func (f *fakeNumStore) NextNumber(_ context.Context, entity, periodKey string) (int, error) {
	if f.seq == nil {
		f.seq = map[string]int{}
	}
	f.seq[entity+"|"+periodKey]++
	return f.seq[entity+"|"+periodKey], nil
}

func (f *fakeNumStore) NextNum(_ context.Context, entity string) (int64, error) {
	if f.legacy == nil {
		f.legacy = map[string]int64{}
	}
	f.legacy[entity]++
	return f.legacy[entity], nil
}

type fakeNumReg struct{ ents map[string]*metadata.Entity }

func (f *fakeNumReg) GetEntity(n string) *metadata.Entity { return f.ents[n] }

func TestNumeratorsRoot_NextNumber(t *testing.T) {
	reg := &fakeNumReg{ents: map[string]*metadata.Entity{
		"Договоры": {Name: "Договоры", Kind: metadata.KindCatalog, Numerator: &metadata.Numerator{Prefix: "Д-", Length: 6, Period: "none"}},
		"Простой":  {Name: "Простой", Kind: metadata.KindCatalog},
	}}
	root := NewNumeratorsRoot(context.Background(), &fakeNumStore{}, reg)

	// numerator: с префиксом и паддингом — последовательные номера.
	if got := root.CallMethod("СледующийНомер", []any{"Договоры"}); got != "Д-000001" {
		t.Fatalf("первый номер = %v, ожидался Д-000001", got)
	}
	if got := root.CallMethod("следующийномер", []any{"Договоры"}); got != "Д-000002" {
		t.Fatalf("второй номер = %v, ожидался Д-000002 (метод регистронезависим)", got)
	}

	// Без numerator — legacy-последовательность 000001.
	if got := root.CallMethod("СледующийНомер", []any{"Простой"}); got != "000001" {
		t.Fatalf("legacy-номер = %v, ожидался 000001", got)
	}
}

func TestNumeratorsRoot_PeriodByDate(t *testing.T) {
	reg := &fakeNumReg{ents: map[string]*metadata.Entity{
		"Заявка": {Name: "Заявка", Numerator: &metadata.Numerator{Length: 5, Period: "year"}},
	}}
	root := NewNumeratorsRoot(context.Background(), &fakeNumStore{}, reg)

	y2025 := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	y2026 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	// Разные годы → независимые бакеты счётчика, оба стартуют с 1.
	if got := root.CallMethod("СледующийНомер", []any{"Заявка", y2025}); got != "00001" {
		t.Fatalf("2025 = %v, ожидался 00001", got)
	}
	if got := root.CallMethod("СледующийНомер", []any{"Заявка", y2026}); got != "00001" {
		t.Fatalf("2026 = %v, ожидался 00001 (свой бакет года)", got)
	}
	if got := root.CallMethod("СледующийНомер", []any{"Заявка", y2025}); got != "00002" {
		t.Fatalf("2025 повторно = %v, ожидался 00002", got)
	}
}

func TestNumeratorsRoot_Errors(t *testing.T) {
	reg := &fakeNumReg{ents: map[string]*metadata.Entity{}}
	root := NewNumeratorsRoot(context.Background(), &fakeNumStore{}, reg)

	assertPanics(t, "без аргумента", func() { root.CallMethod("СледующийНомер", nil) })
	assertPanics(t, "неизвестная сущность", func() { root.CallMethod("СледующийНомер", []any{"НетТакой"}) })
	assertPanics(t, "неизвестный метод", func() { root.CallMethod("Чтоугодно", nil) })
}

// End-to-end: глобал Нумераторы реально резолвится и вызывается из DSL-исходника
// (инжекция через extraVars + диспетчеризация obj.Method(args) в интерпретаторе).
func TestNumeratorsRoot_FromDSLSource(t *testing.T) {
	reg := &fakeNumReg{ents: map[string]*metadata.Entity{
		"Договоры": {Name: "Договоры", Numerator: &metadata.Numerator{Prefix: "Д-", Length: 6, Period: "none"}},
	}}
	root := NewNumeratorsRoot(context.Background(), &fakeNumStore{}, reg)

	src := `Процедура Выполнить()
  this.Рез = Нумераторы.СледующийНомер("Договоры");
КонецПроцедуры`
	l := lexer.New(src, "test.os")
	prog, err := parser.New(l).ParseProgram()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	obj := runtime.NewObject("X", metadata.KindCatalog)
	if err := New().Run(prog.Procedures[0], obj, map[string]any{"Нумераторы": root}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := obj.Get("Рез"); got != "Д-000001" {
		t.Fatalf("this.Рез = %v, ожидался Д-000001", got)
	}
}

func assertPanics(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: ожидалась паника (userError), её не было", name)
		}
	}()
	fn()
}
