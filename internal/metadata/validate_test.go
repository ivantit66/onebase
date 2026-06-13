package metadata

import (
	"strings"
	"testing"
)

// TestValidate_RichTextInTablePart: richtext в табличной части запрещён.
func TestValidate_RichTextInTablePart(t *testing.T) {
	ent := &Entity{
		Name: "Документ",
		Kind: KindDocument,
		TableParts: []TablePart{
			{
				Name: "Строки",
				Fields: []Field{
					{Name: "Комментарий", Type: FieldTypeRichText},
				},
			},
		},
	}
	err := Validate([]*Entity{ent}, nil)
	if err == nil {
		t.Fatalf("ожидалась ошибка: richtext в ТЧ")
	}
	if !strings.Contains(err.Error(), "richtext") || !strings.Contains(err.Error(), "Строки.Комментарий") {
		t.Errorf("неожиданный текст ошибки: %v", err)
	}
}

// TestValidate_RichTextInHeader: richtext в реквизитах шапки разрешён.
func TestValidate_RichTextInHeader(t *testing.T) {
	ent := &Entity{
		Name: "Документ",
		Kind: KindDocument,
		Fields: []Field{
			{Name: "Результат", Type: FieldTypeRichText},
		},
	}
	if err := Validate([]*Entity{ent}, nil); err != nil {
		t.Fatalf("richtext в шапке должен быть разрешён, получили: %v", err)
	}
}

func TestIsRichText(t *testing.T) {
	if !IsRichText(FieldTypeRichText) {
		t.Error("IsRichText(richtext) = false")
	}
	if IsRichText(FieldTypeString) {
		t.Error("IsRichText(string) = true")
	}
}
