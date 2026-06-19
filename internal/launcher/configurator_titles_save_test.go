package launcher

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestSaveReport_PersistsTitlesAndParamLabels(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "reports", "Продажи.yaml", `name: Продажи
title: Продажи
query: ВЫБРАТЬ 1 КАК Один
params:
  - name: Период
    type: date
`)
	form := url.Values{}
	form.Set("report_name", "Продажи")
	form.Set("title", "Продажи")
	form.Set("query", "ВЫБРАТЬ 1 КАК Один")
	form.Set("titles.en", "Sales")
	form.Set("param.0.name", "Период")
	form.Set("param.0.type", "date")
	form.Set("param.0.labels.en", "Period")

	rec := postCfgRv(t, "test", "/bases/test/configurator/report", form, h.configuratorSaveReport)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p, "titles:", "en: Sales", "labels:", "en: Period")
}

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

func TestSaveFields_KeepsTablePartFieldTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "documents", "Заказ.yaml", `name: Заказ
title: Заказ
fields:
  - name: Номер
    type: string
tableparts:
  - name: Товары
    fields:
      - name: Цена
        type: number
        titles:
          en: Price
`)
	form := url.Values{}
	form.Set("entity", "Заказ")
	form.Set("entity_kind", "Документ")
	form.Set("based_on_present", "1")
	form.Set("tp_names", "Товары")
	form.Set("field.0.name", "Номер")
	form.Set("field.0.type", "string")
	form.Set("tp.Товары.field.0.name", "Цена")
	form.Set("tp.Товары.field.0.type", "number")
	form.Set("tp.Товары.field.0.titles.en", "Price")

	rec := postCfgRv(t, "test", "/bases/test/configurator/fields", form, h.configuratorSaveFields)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p, "en: Price")
}

func TestSaveFields_ClearingAllObjectTitlesRemovesKey(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "catalogs", "Склад.yaml", `name: Склад
title: Склад
titles:
  en: Warehouse
fields:
  - name: Код
    type: string
`)
	form := url.Values{}
	form.Set("entity", "Склад")
	form.Set("entity_kind", "Справочник")
	form.Set("field.0.name", "Код")
	form.Set("field.0.type", "string")
	// titles.* НЕ отправляем — пользователь очистил все переводы объекта

	rec := postCfgRv(t, "test", "/bases/test/configurator/fields", form, h.configuratorSaveFields)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	out, _ := os.ReadFile(p)
	if strings.Contains(string(out), "Warehouse") {
		t.Errorf("перевод объекта должен был удалиться, но остался:\n%s", out)
	}
}

func TestSaveProcessor_PersistsTitlesAndParamLabels(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	form := url.Values{}
	form.Set("processor_name", "Загрузка")
	form.Set("title", "Загрузка")
	form.Set("source", "Процедура Старт() КонецПроцедуры")
	form.Set("titles.en", "Import")
	form.Set("param.0.name", "Файл")
	form.Set("param.0.type", "string")
	form.Set("param.0.labels.en", "File")

	rec := postCfgRv(t, "test", "/bases/test/configurator/processor", form, h.configuratorSaveProcessor)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	procPath := cfgDir + "/processors/" + nameToFilename("Загрузка") + ".yaml"
	assertFileContainsRv(t, procPath, "titles:", "en: Import", "labels:", "en: File")
}
