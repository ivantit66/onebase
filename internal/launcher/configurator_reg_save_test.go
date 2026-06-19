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

// postCfg POST-ит форму в произвольный обработчик конфигуратора с заголовком
// X-Onebase-Ajax (renderCfg отвечает JSON, без полного рендера страницы).
func postCfg(t *testing.T, id, path string, form url.Values, fn http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Onebase-Ajax", "1")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func writeCfgFile(t *testing.T, dir, sub, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, sub, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertFileContains(t *testing.T, path string, fragments ...string) {
	t.Helper()
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	got := string(out)
	for _, must := range fragments {
		if !strings.Contains(got, must) {
			t.Errorf("в %s нет фрагмента %q\nполучилось:\n%s", filepath.Base(path), must, got)
		}
	}
}

// Регистр накопления: точность ресурса (number(L,P)) и добавленное измерение
// должны сохраняться; title регистра — не теряться при round-trip.
func TestConfiguratorSaveRegisterFields_PrecisionNewDimTitle(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFile(t, cfgDir, "registers", "остатки.yaml", `name: Остатки
title: Остатки товаров
dimensions:
  - name: Товар
    type: string
resources:
  - name: Сумма
    type: number
`)
	form := url.Values{}
	form.Set("register", "Остатки")
	form.Set("dim.0.name", "Товар")
	form.Set("dim.0.type", "string")
	form.Set("res.0.name", "Сумма")
	form.Set("res.0.type", "number")
	form.Set("res.0.length", "15")
	form.Set("res.0.scale", "2")
	form.Set("new_dim.1.name", "Склад")
	form.Set("new_dim.1.type", "string")

	rec := postCfg(t, "test", "/bases/test/configurator/register-fields", form, h.configuratorSaveRegisterFields)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContains(t, p, "type: number(15,2)", "name: Склад", "title: Остатки товаров")
}

// Регистр сведений: точность ресурса и добавленное измерение сохраняются.
func TestConfiguratorSaveInfoRegFields_PrecisionNewDim(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFile(t, cfgDir, "inforegs", "курсы.yaml", `name: Курсы
title: Курсы валют
dimensions:
  - name: Валюта
    type: string
resources:
  - name: Курс
    type: number
`)
	form := url.Values{}
	form.Set("inforeg", "Курсы")
	form.Set("dim.0.name", "Валюта")
	form.Set("dim.0.type", "string")
	form.Set("res.0.name", "Курс")
	form.Set("res.0.type", "number")
	form.Set("res.0.length", "10")
	form.Set("res.0.scale", "4")
	form.Set("new_dim.1.name", "Регион")
	form.Set("new_dim.1.type", "string")

	rec := postCfg(t, "test", "/bases/test/configurator/inforeg-fields", form, h.configuratorSaveInfoRegFields)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContains(t, p, "type: number(10,4)", "name: Регион", "title: Курсы валют")
}

// План счетов: точность числового ресурса сохраняется.
func TestConfiguratorSaveAccountRegister_ResourcePrecision(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFile(t, cfgDir, "accountregs", "бухучёт.yaml", `name: Бухучёт
resources:
  - name: Сумма
    type: number
`)
	form := url.Values{}
	form.Set("accountreg", "Бухучёт")
	form.Set("res.0.name", "Сумма")
	form.Set("res.0.type", "number")
	form.Set("res.0.length", "18")
	form.Set("res.0.scale", "2")

	rec := postCfg(t, "test", "/bases/test/configurator/account-register", form, h.configuratorSaveAccountRegister)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContains(t, p, "type: number(18,2)")
}

// Константа: точность числа сохраняется, а многоязычные labels не стираются
// при сохранении через редактор (раньше локальный rawConst не имел поля Labels).
func TestConfiguratorSaveConstant_PrecisionKeepsLabels(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFile(t, cfgDir, "constants", "константы.yaml", `constants:
  - name: СтавкаНДС
    type: number
    label: Ставка НДС
    labels:
      en: VAT rate
`)
	form := url.Values{}
	form.Set("const_name", "СтавкаНДС")
	form.Set("type", "number")
	form.Set("length", "5")
	form.Set("scale", "2")
	form.Set("label", "Ставка НДС")

	rec := postCfg(t, "test", "/bases/test/configurator/constant", form, h.configuratorSaveConstant)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContains(t, p, "type: number(5,2)", "labels:", "en: VAT rate")
}
