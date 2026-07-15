package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExchangePlanIncludes(t *testing.T) {
	p := &ExchangePlan{
		Name:    "ФилиалыЦентр",
		Content: []string{"Справочник.Номенклатура", "Документ.Реализация", "Контрагенты"},
	}
	p.Normalize()

	cases := []struct {
		name string
		kind Kind
		want bool
	}{
		{"Номенклатура", KindCatalog, true},   // Справочник.Номенклатура
		{"номенклатура", KindCatalog, true},   // регистронезависимо
		{"Реализация", KindDocument, true},    // Документ.Реализация
		{"Номенклатура", KindDocument, false}, // вид не совпал (в составе справочник)
		{"Контрагенты", KindCatalog, true},    // без префикса — любой вид
		{"Контрагенты", KindDocument, true},   // без префикса — любой вид
		{"Прочее", KindCatalog, false},        // нет в составе
	}
	for _, c := range cases {
		got := p.Includes(&Entity{Name: c.name, Kind: c.kind})
		if got != c.want {
			t.Errorf("Includes(%s/%s) = %v, want %v", c.name, c.kind, got, c.want)
		}
	}
}

func TestExchangePlanNormalizeDefaults(t *testing.T) {
	p := &ExchangePlan{Name: "  План  ", Nodes: []ExchangeNode{{Code: " center "}}}
	p.Normalize()
	if p.Conflict != ConflictByTime {
		t.Errorf("Conflict по умолчанию = %q, want %q", p.Conflict, ConflictByTime)
	}
	if p.Name != "План" {
		t.Errorf("Name не затримлен: %q", p.Name)
	}
	if p.Nodes[0].Code != "center" {
		t.Errorf("код узла не затримлен: %q", p.Nodes[0].Code)
	}
	if p.Node("CENTER") == nil {
		t.Error("Node ищет регистронезависимо, но не нашёл center")
	}
}

func TestLoadExchangePlanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "филиалы.yaml")
	yaml := `name: ФилиалыЦентр
title: Филиалы ↔ Центр
conflict: by_node_priority
content:
  - Справочник.Номенклатура
  - Документ.Реализация
nodes:
  - { code: center, name: Центральная, priority: 10 }
  - { code: fil01,  name: Филиал 1,    priority: 1 }
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadExchangePlanFile(path)
	if err != nil {
		t.Fatalf("LoadExchangePlanFile: %v", err)
	}
	if p.Name != "ФилиалыЦентр" || p.Conflict != ConflictByNodePriority {
		t.Errorf("неожиданные поля: name=%q conflict=%q", p.Name, p.Conflict)
	}
	if len(p.ParsedContent()) != 2 {
		t.Fatalf("ParsedContent = %d, want 2", len(p.ParsedContent()))
	}
	if got := p.ParsedContent()[0]; got.Kind != KindCatalog || got.Name != "Номенклатура" {
		t.Errorf("первая запись состава = %+v", got)
	}
	if n := p.Node("fil01"); n == nil || n.Priority != 1 {
		t.Errorf("узел fil01 не разобран: %+v", n)
	}
}

func TestLoadExchangePlanDirMissing(t *testing.T) {
	plans, err := LoadExchangePlanDir(filepath.Join(t.TempDir(), "nope"))
	if err != nil || plans != nil {
		t.Errorf("отсутствующий каталог должен давать (nil, nil), получено (%v, %v)", plans, err)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestTopologyTargets(t *testing.T) {
	// Плоская топология: регистрируем всем, кроме себя; транзита нет.
	flat := &ExchangePlan{Nodes: []ExchangeNode{{Code: "center"}, {Code: "fil01"}, {Code: "fil02"}}}
	flat.Normalize()
	if got := flat.RegistrationTargets("center"); !sameSet(got, []string{"fil01", "fil02"}) {
		t.Errorf("плоская: RegistrationTargets(center) = %v", got)
	}
	if got := flat.TransitTargets("center", "fil01"); got != nil {
		t.Errorf("плоская: транзита быть не должно, got %v", got)
	}

	// Звезда: hub + две спицы.
	star := &ExchangePlan{Nodes: []ExchangeNode{
		{Code: "hub", Role: RoleHub}, {Code: "filA", Role: RoleSpoke}, {Code: "filB", Role: RoleSpoke},
	}}
	star.Normalize()
	if got := star.RegistrationTargets("filA"); !sameSet(got, []string{"hub"}) {
		t.Errorf("звезда: спица регистрирует только хабу, got %v", got)
	}
	if got := star.RegistrationTargets("hub"); !sameSet(got, []string{"filA", "filB"}) {
		t.Errorf("звезда: хаб регистрирует всем спицам, got %v", got)
	}
	if got := star.TransitTargets("hub", "filA"); !sameSet(got, []string{"filB"}) {
		t.Errorf("звезда: хаб ретранслирует остальным спицам (кроме источника), got %v", got)
	}
	if got := star.TransitTargets("filA", "hub"); got != nil {
		t.Errorf("звезда: спица не ретранслирует, got %v", got)
	}
}
