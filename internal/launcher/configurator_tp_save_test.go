package launcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// saveFieldsForm POST-ит форму редактора реквизитов в configuratorSaveFields
// с заголовком X-Onebase-Ajax (renderCfg вернёт JSON, без полного рендера).
func saveFieldsForm(t *testing.T, h *handler, id string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/bases/"+id+"/configurator/fields",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Onebase-Ajax", "1")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.configuratorSaveFields(rec, req)
	return rec
}

// Регрессия (issue: правки ТЧ не сохраняются). Покрывает разом:
//   - Баг A: поле, добавленное в СУЩЕСТВУЮЩУЮ ТЧ (префикс new_tp.<имя>.field.*),
//     теряется, потому что ни один серверный цикл его не читал;
//   - Баг B: новая ТЧ (new_tp_name + new_tp.<имя>.field.*) не сохранялась
//     (applyFieldEdits не добавлял новые ТЧ; имя/idx расходились);
//   - точность децимала и тип «дата» у добавляемых полей ТЧ.
func TestConfiguratorSaveFields_TablePartEdits(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	if err := os.MkdirAll(filepath.Join(cfgDir, "catalogs"), 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(cfgDir, "catalogs", "номенклатура.yaml")
	initial := `name: Номенклатура
fields:
  - name: Наименование
    type: string
tableparts:
  - name: Цены
    fields:
      - name: Сумма
        type: number
`
	if err := os.WriteFile(yamlPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("entity", "Номенклатура")
	form.Set("entity_kind", "Справочник")
	// шапка
	form.Set("field.0.name", "Наименование")
	form.Set("field.0.type", "string")
	// существующая ТЧ «Цены»: исходное поле + добавленный реквизит-децимал
	form.Set("tp_names", "Цены")
	form.Set("tp.Цены.field.0.name", "Сумма")
	form.Set("tp.Цены.field.0.type", "number")
	form.Set("new_tp.Цены.field.1.name", "Скидка")
	form.Set("new_tp.Цены.field.1.type", "number")
	form.Set("new_tp.Цены.field.1.length", "15")
	form.Set("new_tp.Цены.field.1.scale", "2")
	// новая ТЧ «Состав» с числом и датой
	form.Set("new_tp_name", "Состав")
	form.Set("new_tp.Состав.field.2.name", "Количество")
	form.Set("new_tp.Состав.field.2.type", "number")
	form.Set("new_tp.Состав.field.3.name", "ДатаОтгрузки")
	form.Set("new_tp.Состав.field.3.type", "date")

	rec := saveFieldsForm(t, h, "test", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("код ответа %d: %s", rec.Code, rec.Body.String())
	}

	out, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("чтение YAML: %v", err)
	}
	got := string(out)

	for _, must := range []string{
		// исходное поле ТЧ не потеряно
		"name: Сумма",
		// Баг A: реквизит, добавленный в существующую ТЧ, сохранён с точностью
		"name: Скидка",
		"type: number(15,2)",
		// Баг B: новая ТЧ сохранена
		"name: Состав",
		"name: Количество",
		// тип «дата» в новой ТЧ сохранён
		"name: ДатаОтгрузки",
		"type: date",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("в YAML после сохранения нет ожидаемого фрагмента %q\nполучилось:\n%s", must, got)
		}
	}
}
