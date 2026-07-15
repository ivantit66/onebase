package configcheck

// Тесты валидации планов обмена (план 86).

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/project"
)

func mkPlan(name, conflict string, content []string, nodes ...metadata.ExchangeNode) *metadata.ExchangePlan {
	p := &metadata.ExchangePlan{Name: name, Conflict: conflict, Content: content, Nodes: nodes}
	p.Normalize()
	return p
}

func TestCheckExchangePlans(t *testing.T) {
	entities := []*metadata.Entity{
		{Name: "Номенклатура", Kind: metadata.KindCatalog},
		{Name: "Реализация", Kind: metadata.KindDocument},
	}
	center := metadata.ExchangeNode{Code: "center", Priority: 10}
	fil := metadata.ExchangeNode{Code: "fil01", Priority: 1}

	proj := &project.Project{
		Entities: entities,
		ExchangePlans: []*metadata.ExchangePlan{
			mkPlan("Хороший", "by_time", []string{"Справочник.Номенклатура", "Документ.Реализация"}, center, fil),
			mkPlan("ПлохоеПравило", "по_времени", []string{"Справочник.Номенклатура"}, center, fil),
			mkPlan("НетСущности", "by_time", []string{"Справочник.НетТакой"}, center, fil),
			mkPlan("НеТотВид", "by_time", []string{"Документ.Номенклатура"}, center, fil),
			mkPlan("ОдинУзел", "by_time", []string{"Справочник.Номенклатура"}, center),
			mkPlan("ДубльУзла", "by_time", []string{"Справочник.Номенклатура"}, center, metadata.ExchangeNode{Code: "CENTER"}),
			mkPlan("ПустойСостав", "by_time", nil, center, fil),
			mkPlan("ДваХаба", "by_time", []string{"Справочник.Номенклатура"},
				metadata.ExchangeNode{Code: "hub1", Role: metadata.RoleHub},
				metadata.ExchangeNode{Code: "hub2", Role: metadata.RoleHub},
				metadata.ExchangeNode{Code: "fil", Role: metadata.RoleSpoke}),
			mkPlan("ТриБезХаба", "by_time", []string{"Справочник.Номенклатура"},
				metadata.ExchangeNode{Code: "a"}, metadata.ExchangeNode{Code: "b"}, metadata.ExchangeNode{Code: "c"}),
		},
	}

	issues := CheckExchangePlans(proj)
	for _, want := range []string{
		"неизвестное правило конфликта",
		"несуществующая сущность",
		"нет документа с таким именем",
		"минимум два узла",
		"код узла \"CENTER\" дублируется",
		"пустой состав",
		"ровно один хаб",
		"тремя и более узлами",
	} {
		found := false
		for _, is := range issues {
			if strings.Contains(is.Message, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("не найдена ожидаемая ошибка %q среди: %+v", want, issues)
		}
	}

	// «Хороший» план не должен давать ни одной ошибки.
	for _, is := range issues {
		if is.Object == "Хороший" {
			t.Errorf("корректный план дал ошибку: %+v", is)
		}
	}
}

func TestCheckExchangePlansDuplicateName(t *testing.T) {
	center := metadata.ExchangeNode{Code: "center"}
	fil := metadata.ExchangeNode{Code: "fil01"}
	proj := &project.Project{
		Entities: []*metadata.Entity{{Name: "Номенклатура", Kind: metadata.KindCatalog}},
		ExchangePlans: []*metadata.ExchangePlan{
			mkPlan("Обмен", "by_time", []string{"Номенклатура"}, center, fil),
			mkPlan("обмен", "by_time", []string{"Номенклатура"}, center, fil),
		},
	}
	found := false
	for _, is := range CheckExchangePlans(proj) {
		if strings.Contains(is.Message, "дублируется") {
			found = true
		}
	}
	if !found {
		t.Error("дублирующееся имя плана не обнаружено")
	}
}
