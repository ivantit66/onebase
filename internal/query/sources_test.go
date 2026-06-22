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

// Регрессия #14: авто-JOIN ссылочного поля (Контрагент.Наименование) подтягивает
// таблицу связанного справочника/документа и отдаёт её наименование/номер. Раньше
// addSource для неё не вызывался → пользователь с правом на Документ, но без права
// на справочник Контрагент, читал наименования контрагентов в обход RBAC. Теперь
// связанная сущность обязана попасть в Sources.
func TestCompile_Sources_RefDimAutoJoin(t *testing.T) {
	order := &metadata.Entity{
		Name: "Заказ",
		Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Контрагент", Type: "reference:Контрагент", RefEntity: "Контрагент"},
		},
	}
	cp := &metadata.Entity{
		Name:   "Контрагент",
		Kind:   metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	res, err := query.Compile(`ВЫБРАТЬ Контрагент.Наименование ИЗ Документ.Заказ`,
		query.CompileOpts{Entities: []*metadata.Entity{order, cp}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !hasSource(res.Sources, "document", "Заказ") {
		t.Fatalf("ожидался источник {document Заказ} среди %+v", res.Sources)
	}
	if !hasSource(res.Sources, "catalog", "Контрагент") {
		t.Fatalf("связанный справочник Контрагент должен попасть в Sources (RBAC), среди %+v", res.Sources)
	}
}

// Авто-JOIN ссылки на документ (не справочник) тоже регистрирует источник с
// верным kind=document, чтобы RBAC проверял право чтения связанного документа.
func TestCompile_Sources_RefDimToDocument(t *testing.T) {
	order := &metadata.Entity{
		Name: "Оплата",
		Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "ОснованиеЗаказ", Type: "reference:Заказ", RefEntity: "Заказ"},
		},
	}
	base := &metadata.Entity{
		Name:   "Заказ",
		Kind:   metadata.KindDocument,
		Fields: []metadata.Field{{Name: "Сумма", Type: metadata.FieldTypeNumber}},
	}
	res, err := query.Compile(`ВЫБРАТЬ ОснованиеЗаказ.Номер ИЗ Документ.Оплата`,
		query.CompileOpts{Entities: []*metadata.Entity{order, base}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !hasSource(res.Sources, "document", "Заказ") {
		t.Fatalf("связанный документ Заказ должен попасть в Sources как document, среди %+v", res.Sources)
	}
}
