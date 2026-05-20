package storage

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
)

// Замечание #14: cross-ref внутри одного справочника в predefined.
// Розничная → Закупочная: Закупочная должна вставляться первой.
func TestTopoSortPredefined_LinearChain(t *testing.T) {
	items := []*metadata.PredefinedItem{
		{Name: "Розничная", Fields: map[string]any{"БазовыйТип": "Закупочная"}},
		{Name: "Закупочная", Fields: map[string]any{}},
	}
	selfRef := map[string]bool{"базовыйтип": true}
	ordered, err := topoSortPredefined(items, selfRef)
	if err != nil {
		t.Fatalf("topo sort: %v", err)
	}
	if len(ordered) != 2 {
		t.Fatalf("ожидалось 2 элемента, получили %d", len(ordered))
	}
	if ordered[0].Name != "Закупочная" || ordered[1].Name != "Розничная" {
		t.Errorf("неверный порядок: %s, %s", ordered[0].Name, ordered[1].Name)
	}
}

// Несколько уровней зависимости: Дилерская → Оптовая → Закупочная.
func TestTopoSortPredefined_MultiLevel(t *testing.T) {
	items := []*metadata.PredefinedItem{
		{Name: "Дилерская", Fields: map[string]any{"БазовыйТип": "Оптовая"}},
		{Name: "Оптовая", Fields: map[string]any{"БазовыйТип": "Закупочная"}},
		{Name: "Закупочная", Fields: map[string]any{}},
	}
	selfRef := map[string]bool{"базовыйтип": true}
	ordered, err := topoSortPredefined(items, selfRef)
	if err != nil {
		t.Fatalf("topo sort: %v", err)
	}
	// Первой должна быть Закупочная, последней — Дилерская.
	if ordered[0].Name != "Закупочная" {
		t.Errorf("первой ожидалась Закупочная, получили %s", ordered[0].Name)
	}
	if ordered[len(ordered)-1].Name != "Дилерская" {
		t.Errorf("последней ожидалась Дилерская, получили %s", ordered[len(ordered)-1].Name)
	}
}

// Параллельные ветви.
func TestTopoSortPredefined_Branching(t *testing.T) {
	items := []*metadata.PredefinedItem{
		{Name: "Розничная", Fields: map[string]any{"БазовыйТип": "Закупочная"}},
		{Name: "Оптовая", Fields: map[string]any{"БазовыйТип": "Закупочная"}},
		{Name: "Закупочная", Fields: map[string]any{}},
	}
	selfRef := map[string]bool{"базовыйтип": true}
	ordered, err := topoSortPredefined(items, selfRef)
	if err != nil {
		t.Fatal(err)
	}
	// Закупочная должна идти раньше обеих ссылающихся.
	idx := func(name string) int {
		for i, it := range ordered {
			if it.Name == name {
				return i
			}
		}
		return -1
	}
	if idx("Закупочная") > idx("Розничная") {
		t.Error("Закупочная должна быть раньше Розничной")
	}
	if idx("Закупочная") > idx("Оптовая") {
		t.Error("Закупочная должна быть раньше Оптовой")
	}
}

// Цикл должен давать ошибку с упоминанием цепочки.
func TestTopoSortPredefined_Cycle(t *testing.T) {
	items := []*metadata.PredefinedItem{
		{Name: "A", Fields: map[string]any{"БазовыйТип": "B"}},
		{Name: "B", Fields: map[string]any{"БазовыйТип": "A"}},
	}
	selfRef := map[string]bool{"базовыйтип": true}
	_, err := topoSortPredefined(items, selfRef)
	if err == nil {
		t.Fatal("цикл должен давать ошибку")
	}
	if !strings.Contains(err.Error(), "цикл") {
		t.Errorf("ошибка должна упоминать «цикл»: %v", err)
	}
}

// Ссылка на несуществующий элемент — игнорируется (значит запись осиротеет
// при FK, но это уже не проблема topo-sort'а).
func TestTopoSortPredefined_MissingRefIgnored(t *testing.T) {
	items := []*metadata.PredefinedItem{
		{Name: "A", Fields: map[string]any{"БазовыйТип": "НетТакого"}},
		{Name: "B", Fields: map[string]any{}},
	}
	selfRef := map[string]bool{"базовыйтип": true}
	ordered, err := topoSortPredefined(items, selfRef)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(ordered) != 2 {
		t.Errorf("ожидалось 2, получили %d", len(ordered))
	}
}

// Без self-ref полей вообще — сохраняется исходный порядок.
func TestTopoSortPredefined_NoSelfRef(t *testing.T) {
	items := []*metadata.PredefinedItem{
		{Name: "First"},
		{Name: "Second"},
		{Name: "Third"},
	}
	selfRef := map[string]bool{}
	ordered, err := topoSortPredefined(items, selfRef)
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].Name != "First" || ordered[1].Name != "Second" || ordered[2].Name != "Third" {
		t.Errorf("без зависимостей порядок должен сохраниться: %v",
			[]string{ordered[0].Name, ordered[1].Name, ordered[2].Name})
	}
}
