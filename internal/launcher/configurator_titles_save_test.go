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
	// UI при очистке поля шлёт ключ titles.en с пустым значением —
	// гейт formHasMapField срабатывает, parseMapForm возвращает nil (пусто).
	form.Set("titles.en", "")

	rec := postCfgRv(t, "test", "/bases/test/configurator/fields", form, h.configuratorSaveFields)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	out, _ := os.ReadFile(p)
	if strings.Contains(string(out), "Warehouse") {
		t.Errorf("перевод объекта должен был удалиться, но остался:\n%s", out)
	}
}

func TestSaveConstant_PersistsLabels(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "constants", "общие.yaml", `constants:
  - name: СтавкаНДС
    type: number
    label: Ставка НДС
`)
	form := url.Values{}
	form.Set("const_name", "СтавкаНДС")
	form.Set("type", "number")
	form.Set("label", "Ставка НДС")
	form.Set("labels.en", "VAT rate")

	rec := postCfgRv(t, "test", "/bases/test/configurator/constant", form, h.configuratorSaveConstant)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p, "labels:", "en: VAT rate")
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

// Регрессия #3: при сохранении обработки из конфигуратора (правка заголовка/
// параметров) round-trip node-edit обязан сохранить ключи, которых нет в форме —
// table_parts обработки и default/options параметров. Усечённый yaml.Marshal
// типизированной структуры молча их стирал (потеря данных).
func TestSaveProcessor_PreservesTablePartsAndParamDefaults(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	procPath := writeCfgFileRv(t, cfgDir, "processors", nameToFilename("Загрузка")+".yaml", `name: Загрузка
title: Загрузка
params:
  - name: Режим
    type: choice
    default: Быстрый
    options:
      - Быстрый
      - Полный
table_parts:
  - name: Строки
    fields:
      - name: Колонка
        type: string
`)
	form := url.Values{}
	form.Set("processor_name", "Загрузка")
	form.Set("title", "Загрузка данных") // правим только заголовок
	form.Set("source", "Процедура Старт() КонецПроцедуры")
	form.Set("param.0.name", "Режим")
	form.Set("param.0.type", "choice")

	rec := postCfgRv(t, "test", "/bases/test/configurator/processor", form, h.configuratorSaveProcessor)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	// Заголовок обновился, а table_parts и default/options параметра сохранились.
	assertFileContainsRv(t, procPath,
		"title: Загрузка данных",
		"table_parts:",
		"name: Строки",
		"name: Колонка",
		"default: Быстрый",
		"options:",
		"- Быстрый",
		"- Полный",
	)
}

func TestSaveSubsystem_PersistsTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	form := url.Values{}
	form.Set("subsystem_name", "Продажи")
	form.Set("title", "Продажи")
	form.Set("order", "1")
	form.Set("titles.en", "Sales")

	rec := postCfgRv(t, "test", "/bases/test/configurator/subsystem", form, h.configuratorSaveSubsystem)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, cfgDir+"/subsystems/"+nameToFilename("Продажи")+".yaml", "titles:", "en: Sales")
}

func TestSaveInfoReg_PersistsTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "inforegs", "Курсы.yaml", `name: Курсы
title: Курсы
dimensions:
  - name: Валюта
    type: string
resources:
  - name: Курс
    type: number
`)
	form := url.Values{}
	form.Set("inforeg", "Курсы")
	form.Set("titles.en", "Rates")
	form.Set("dim.0.name", "Валюта")
	form.Set("dim.0.type", "string")
	form.Set("dim.0.titles.en", "Currency")
	form.Set("res.0.name", "Курс")
	form.Set("res.0.type", "number")
	form.Set("res.0.titles.en", "Exchange Rate")

	rec := postCfgRv(t, "test", "/bases/test/configurator/inforeg-fields", form, h.configuratorSaveInfoRegFields)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p, "titles:", "en: Rates", "en: Currency", "en: Exchange Rate")
}

func TestSaveRegisterFields_PersistsTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "registers", "Остатки.yaml", `name: Остатки
title: Остатки
dimensions:
  - name: Товар
    type: string
resources:
  - name: Кол
    type: number
`)
	form := url.Values{}
	form.Set("register", "Остатки")
	form.Set("title", "Остатки")
	form.Set("titles.en", "Stock")
	form.Set("dim.0.name", "Товар")
	form.Set("dim.0.type", "string")
	form.Set("dim.0.titles.en", "Product")
	form.Set("res.0.name", "Кол")
	form.Set("res.0.type", "number")
	form.Set("res.0.titles.en", "Quantity")

	rec := postCfgRv(t, "test", "/bases/test/configurator/register-fields", form, h.configuratorSaveRegisterFields)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p, "titles:", "en: Stock", "en: Product", "en: Quantity")
}

func TestSaveAccountRegister_PersistsTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "accountregs", "Хозрасчетный.yaml", `name: Хозрасчетный
title: Хозрасчетный
accounts: ПланСчетов
resources:
  - name: Сумма
    type: number
`)
	form := url.Values{}
	form.Set("accountreg", "Хозрасчетный")
	form.Set("title", "Хозрасчетный")
	form.Set("accounts", "ПланСчетов")
	form.Set("titles.en", "Self-supporting")
	form.Set("res.0.name", "Сумма")
	form.Set("res.0.type", "number")
	form.Set("res.0.titles.en", "Amount")

	rec := postCfgRv(t, "test", "/bases/test/configurator/account-register", form, h.configuratorSaveAccountRegister)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p, "titles:", "en: Self-supporting", "en: Amount")
}

func TestSavePage_PersistsTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	form := url.Values{}
	form.Set("page_name", "Панель")
	form.Set("title", "Панель")
	form.Set("source", "Процедура ПриФормировании() КонецПроцедуры")
	form.Set("titles.en", "Dashboard")

	rec := postCfgRv(t, "test", "/bases/test/configurator/page", form, h.configuratorSavePage)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, cfgDir+"/pages/Панель.yaml", "titles:", "en: Dashboard")
}

func TestSaveHomePage_PersistsTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	form := url.Values{}
	form.Set("home_title", "Главная")
	form.Set("titles.en", "Home")

	rec := postCfgRv(t, "test", "/bases/test/configurator/home-page", form, h.configuratorSaveHomePage)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, cfgDir+"/config/home_page.yaml", "titles:", "en: Home")
}
