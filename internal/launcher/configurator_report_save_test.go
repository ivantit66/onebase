package launcher

import (
	"net/http"
	"net/url"
	"testing"
)

// Регрессия (issue #86, не закрытая в живом обработчике): сохранение отчёта
// через редактор round-trip'ило YAML через типизированную saveReport без поля
// Titles и молча стирало многоязычные titles. Фикс: точечная правка узла YAML
// (setYAMLMapField), прочие ключи сохраняются.
func TestConfiguratorSaveReport_PreservesTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "reports", "продажи.yaml", `name: Продажи
title: Продажи
titles:
  en: Sales
  de: Verkäufe
query: ВЫБРАТЬ 1 КАК Один
`)
	form := url.Values{}
	form.Set("report_name", "Продажи")
	form.Set("title", "Продажи")
	form.Set("query", "ВЫБРАТЬ 2 КАК Два")

	rec := postCfgRv(t, "test", "/bases/test/configurator/report", form, h.configuratorSaveReport)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p, "titles:", "en: Sales", "de: Verkäufe", "ВЫБРАТЬ 2 КАК Два")
}
