package storage

import (
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
)

// Замечание #7: явный Map имеет приоритет над exact match и fallback.
func TestColExprForDoc_MapWins(t *testing.T) {
	entity := &metadata.Entity{
		Name: "ПоступлениеТоваров",
		Fields: []metadata.Field{
			{Name: "Контрагент", Type: metadata.FieldTypeString},
			{Name: "Поставщик", Type: metadata.FieldTypeString},
		},
	}
	jcol := metadata.JournalColumn{
		Field: "Контрагент",
		Map: map[string]string{
			"ПоступлениеТоваров": "Поставщик",
		},
		Fallback: []string{"Поставщик", "Покупатель"},
	}
	expr, f := colExprForDoc(jcol, entity)
	// Map → Поставщик
	if expr != "поставщик" {
		t.Errorf("Map должен выиграть: ожидался «поставщик», получили %q", expr)
	}
	if f == nil || f.Name != "Поставщик" {
		t.Errorf("вернулось не то поле: %v", f)
	}
}

// Без Map — exact match.
func TestColExprForDoc_ExactMatch(t *testing.T) {
	entity := &metadata.Entity{
		Name: "ПоступлениеТоваров",
		Fields: []metadata.Field{
			{Name: "Контрагент", Type: metadata.FieldTypeString},
		},
	}
	jcol := metadata.JournalColumn{Field: "Контрагент"}
	expr, f := colExprForDoc(jcol, entity)
	if expr != "контрагент" {
		t.Errorf("exact match не сработал: %q", expr)
	}
	if f.Name != "Контрагент" {
		t.Errorf("не то поле: %s", f.Name)
	}
}

// Без Map и без exact — fallback.
func TestColExprForDoc_FallbackUsed(t *testing.T) {
	entity := &metadata.Entity{
		Name: "ПоступлениеТоваров",
		Fields: []metadata.Field{
			{Name: "Поставщик", Type: metadata.FieldTypeString},
		},
	}
	jcol := metadata.JournalColumn{
		Field:    "Контрагент",
		Fallback: []string{"Поставщик", "Покупатель"},
	}
	expr, _ := colExprForDoc(jcol, entity)
	if expr != "поставщик" {
		t.Errorf("fallback не сработал: %q", expr)
	}
}

// Map указывает на несуществующее поле — NULL, не молчаливый fallback.
func TestColExprForDoc_MapMissingFieldGivesNull(t *testing.T) {
	entity := &metadata.Entity{
		Name: "ПоступлениеТоваров",
		Fields: []metadata.Field{
			{Name: "Поставщик", Type: metadata.FieldTypeString},
		},
	}
	jcol := metadata.JournalColumn{
		Field: "Контрагент",
		Map: map[string]string{
			"ПоступлениеТоваров": "НетТакого",
		},
		Fallback: []string{"Поставщик"}, // не должен сработать — Map важнее
	}
	expr, _ := colExprForDoc(jcol, entity)
	if expr != "NULL" {
		t.Errorf("неверная колонка в Map → должна быть NULL, получили %q", expr)
	}
}

// Несколько fallback — COALESCE.
func TestColExprForDoc_MultiFallbackCoalesce(t *testing.T) {
	entity := &metadata.Entity{
		Name: "СоставнойДокумент",
		Fields: []metadata.Field{
			{Name: "Поставщик", Type: metadata.FieldTypeString},
			{Name: "Покупатель", Type: metadata.FieldTypeString},
		},
	}
	jcol := metadata.JournalColumn{
		Field:    "Контрагент",
		Fallback: []string{"Поставщик", "Покупатель"},
	}
	expr, _ := colExprForDoc(jcol, entity)
	if expr != "COALESCE(поставщик, покупатель)" {
		t.Errorf("ожидался COALESCE, получили %q", expr)
	}
}
