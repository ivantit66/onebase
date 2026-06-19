package launcher

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSaveFields_PersistsTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "catalogs", "Контрагент.yaml", `name: Контрагент
title: Контрагент
fields:
  - name: ИНН
    type: string
`)
	form := url.Values{}
	form.Set("entity", "Контрагент")
	form.Set("entity_kind", "Справочник")
	form.Set("titles.en", "Counterparty")
	form.Set("titles.de", "Geschäftspartner")
	form.Set("field.0.name", "ИНН")
	form.Set("field.0.type", "string")
	form.Set("field.0.titles.en", "TIN")

	rec := postCfgRv(t, "test", "/bases/test/configurator/fields", form, h.configuratorSaveFields)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p, "titles:", "en: Counterparty", "de: Geschäftspartner", "en: TIN")
}

func TestSaveFields_KeepsExistingFieldTitlesWhenResent(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "catalogs", "Товар.yaml", `name: Товар
title: Товар
fields:
  - name: Артикул
    type: string
    titles:
      en: SKU
`)
	form := url.Values{}
	form.Set("entity", "Товар")
	form.Set("entity_kind", "Справочник")
	form.Set("field.0.name", "Артикул")
	form.Set("field.0.type", "string")
	form.Set("field.0.titles.en", "SKU")

	rec := postCfgRv(t, "test", "/bases/test/configurator/fields", form, h.configuratorSaveFields)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	assertFileContainsRv(t, p, "en: SKU")
}
