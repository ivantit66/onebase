package query_test

import (
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
)

// hasSource reports whether res.Sources contains (kind, name).
func hasSource(srcs []query.SourceRef, kind, name string) bool {
	for _, s := range srcs {
		if s.Kind == kind && s.Name == name {
			return true
		}
	}
	return false
}

// TestCompile_Sources: компилятор выдаёт список объектов-источников запроса
// (kind, name) для проверки прав ИИ-ассистентом (план 54). Имя — исходное имя
// сущности/регистра (не имя таблицы); kind — секция прав (catalog/document/
// register/inforeg; пусто для бухрегистра — секции прав нет).
func TestCompile_Sources(t *testing.T) {
	cases := []struct {
		name string
		src  string
		opts query.CompileOpts
		want []query.SourceRef
	}{
		{
			"справочник",
			`ВЫБРАТЬ Наименование ИЗ Справочник.Товар`,
			query.CompileOpts{},
			[]query.SourceRef{{Kind: "catalog", Name: "Товар"}},
		},
		{
			"документ + справочник через соединение",
			`ВЫБРАТЬ Прод.Номер ИЗ Документ.Реализация КАК Прод
			   ВНУТРЕННЕЕ СОЕДИНЕНИЕ Справочник.Клиент КАК Клиент
			   ПО Прод.Покупатель = Клиент.Ссылка`,
			query.CompileOpts{},
			[]query.SourceRef{{Kind: "document", Name: "Реализация"}, {Kind: "catalog", Name: "Клиент"}},
		},
		{
			"регистр накопления (физическая таблица)",
			`ВЫБРАТЬ Количество ИЗ РегистрНакопления.ТоварноеДвижение`,
			query.CompileOpts{},
			[]query.SourceRef{{Kind: "register", Name: "ТоварноеДвижение"}},
		},
		{
			"регистр накопления (виртуальная таблица Остатки)",
			`ВЫБРАТЬ Номенклатура, КоличествоОстаток ИЗ РегистрНакопления.ТоварноеДвижение.Остатки()`,
			query.CompileOpts{Registers: []*metadata.Register{testReg()}},
			[]query.SourceRef{{Kind: "register", Name: "ТоварноеДвижение"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := query.Compile(c.src, c.opts)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			for _, w := range c.want {
				if !hasSource(res.Sources, w.Kind, w.Name) {
					t.Fatalf("ожидался источник %+v среди %+v", w, res.Sources)
				}
			}
		})
	}
}
