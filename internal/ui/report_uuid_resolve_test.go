package ui

import "testing"

// detail_link колонка хранит UUID регистратора для ссылки на документ; её UUID
// нельзя подменять наименованием, иначе drill-down строит /ui/.../<имя> вместо
// /<uuid> и ссылка ведёт в никуда (issue #87). applyResolvedLabels должна
// пропускать указанную колонку.
func TestApplyResolvedLabelsSkipsDetailLinkColumn(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	rows := []map[string]any{
		{"Контрагент": id, "Регистратор": id},
	}
	labels := map[string]string{id: "ООО Ромашка"}

	applyResolvedLabels(rows, labels, "Регистратор")

	if got := rows[0]["Контрагент"]; got != "ООО Ромашка" {
		t.Fatalf("обычная колонка должна резолвиться в имя: %v", got)
	}
	if got := rows[0]["Регистратор"]; got != id {
		t.Fatalf("detail_link колонка должна сохранить UUID: %v", got)
	}
}

// Без skipCol поведение прежнее — резолвятся все колонки.
func TestApplyResolvedLabelsNoSkip(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	rows := []map[string]any{{"Контрагент": id}}
	labels := map[string]string{id: "ООО Ромашка"}

	applyResolvedLabels(rows, labels, "")

	if got := rows[0]["Контрагент"]; got != "ООО Ромашка" {
		t.Fatalf("без skip колонка должна резолвиться: %v", got)
	}
}

// Сравнение имени пропускаемой колонки регистронезависимо: колонки запроса
// приходят в нижнем регистре, а detail_link в composition — в исходном.
func TestApplyResolvedLabelsSkipCaseInsensitive(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	rows := []map[string]any{{"регистратор": id}}
	labels := map[string]string{id: "Накладная №1"}

	applyResolvedLabels(rows, labels, "Регистратор")

	if got := rows[0]["регистратор"]; got != id {
		t.Fatalf("skip должен быть регистронезависимым, UUID сохранён: %v", got)
	}
}
