package ui

import (
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/metadata"
)

// Регрессия issue #361: reference-picker для документа без строкового реквизита
// раньше проваливался в сырой UUID (row["id"]). Теперь подпись строится из
// Номера, а если его нет — синтезируется из дат и чисел.
func TestFirstStringField_DocumentFallback(t *testing.T) {
	docFinancial := &metadata.Entity{
		Name: "Начисление",
		Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "ФинансовыйОбъект", Type: "reference:ФО", RefEntity: "ФО"},
			{Name: "Период", Type: metadata.FieldTypeDate},
			{Name: "Сумма", Type: metadata.FieldTypeNumber},
		},
	}
	docWithNumber := &metadata.Entity{
		Name: "Заявка",
		Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Номер", Type: metadata.FieldTypeString},
			{Name: "Дата", Type: metadata.FieldTypeDate},
		},
	}
	docWithName := &metadata.Entity{
		Name: "Приказ",
		Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Тема", Type: metadata.FieldTypeString},
			{Name: "Сумма", Type: metadata.FieldTypeNumber},
		},
	}
	catNoString := &metadata.Entity{
		Name: "Счёт",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Код", Type: metadata.FieldTypeNumber},
		},
	}

	uid := "e594c3fb-1fdc-43be-9126-03e81f3ad4ed"
	period := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)

	tests := []struct {
		name   string
		row    map[string]any
		entity *metadata.Entity
		want   string
	}{
		{
			name:   "документ без строкового реквизита — синтез из даты и числа, не UUID",
			row:    map[string]any{"id": uid, "ФинансовыйОбъект": "abc", "Период": period, "Сумма": 1500.0},
			entity: docFinancial,
			want:   "01.07.2026 · 1500",
		},
		{
			name:   "документ с Номером — берём Номер",
			row:    map[string]any{"id": uid, "Номер": "СН-0007", "Дата": period},
			entity: docWithNumber,
			want:   "СН-0007",
		},
		{
			name:   "документ со строковым реквизитом — поведение не меняется",
			row:    map[string]any{"id": uid, "Тема": "Отпуск", "Сумма": 1500.0},
			entity: docWithName,
			want:   "Отпуск",
		},
		{
			name:   "документ вообще без полезных полей — сырой id как последний фолбэк",
			row:    map[string]any{"id": uid},
			entity: docFinancial,
			want:   uid,
		},
		{
			name:   "справочник без строкового реквизита — поведение не меняется (id)",
			row:    map[string]any{"id": uid, "Код": 42.0},
			entity: catNoString,
			want:   uid,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstStringField(tc.row, tc.entity); got != tc.want {
				t.Errorf("firstStringField = %q, ожидалось %q", got, tc.want)
			}
		})
	}
}
