package ui

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/richtext"
)

func richtextEntity() *metadata.Entity {
	return &metadata.Entity{
		Name: "Задача",
		Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Результат", Type: metadata.FieldTypeRichText},
		},
	}
}

// TestFormToFields_SanitizesRichText: на записи richtext-поле прогоняется через
// санитайзер — script/onerror вырезаны, форматирование сохранено.
func TestFormToFields_SanitizesRichText(t *testing.T) {
	body := url.Values{}
	body.Set("Наименование", "Тест")
	body.Set("Результат", `<p>ok</p><script>alert(1)</script><img src="x" onerror="alert(2)">`)

	req := httptest.NewRequest("POST", "/", nil)
	req.PostForm = body

	fields := formToFields(req, richtextEntity())
	got, _ := fields["Результат"].(string)
	if got == "" {
		t.Fatalf("Результат пуст, ожидался санитизированный HTML")
	}
	low := strings.ToLower(got)
	if strings.Contains(low, "script") {
		t.Errorf("script не вырезан: %q", got)
	}
	if strings.Contains(low, "onerror") {
		t.Errorf("onerror не вырезан: %q", got)
	}
	if !strings.Contains(got, "<p>ok</p>") {
		t.Errorf("форматирование потеряно: %q", got)
	}
}

// TestCheckRichTextLimits_Oversize: значение больше MaxBytes → ошибка формы.
func TestCheckRichTextLimits_Oversize(t *testing.T) {
	body := url.Values{}
	body.Set("Наименование", "Тест")
	body.Set("Результат", strings.Repeat("a", richtext.MaxBytes+1))

	req := httptest.NewRequest("POST", "/", nil)
	req.PostForm = body

	if err := checkRichTextLimits(req, richtextEntity()); err == nil {
		t.Fatalf("ожидалась ошибка превышения размера, получили nil")
	}
}

// TestCheckRichTextLimits_WithinLimit: значение в пределах лимита → без ошибки.
func TestCheckRichTextLimits_WithinLimit(t *testing.T) {
	body := url.Values{}
	body.Set("Результат", "<p>небольшой текст</p>")

	req := httptest.NewRequest("POST", "/", nil)
	req.PostForm = body

	if err := checkRichTextLimits(req, richtextEntity()); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}
