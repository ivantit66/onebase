package launcher

import (
	"strings"
	"testing"
)

func TestQueryHintSystem(t *testing.T) {
	s := queryHintSystem("Справочники:\n  Клиент: Наименование")
	for _, sub := range []string{"ВЫБРАТЬ", "Остатки", "Клиент", "Конфигурация базы"} {
		if !strings.Contains(s, sub) {
			t.Errorf("queryHintSystem не содержит %q:\n%s", sub, s)
		}
	}
	if got := queryHintSystem(""); strings.Contains(got, "Конфигурация базы") {
		t.Error("пустой schema не должен добавлять секцию конфигурации")
	}
}

func TestConfigurator_ExplainQueryWired(t *testing.T) {
	// HTML-элементы панелей остаются в шаблоне; вызовы эндпоинтов уехали в
	// /static/configurator.js (план 55 фаза 2b-2).
	html := renderCfgFoot(t)
	for _, sub := range []string{
		"qb-ai-desc", "qb-ai-gen", "mqb-qry",
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в cfg-foot нет %q — хук не подключён", sub)
		}
	}
	js := configuratorJS(t)
	for _, sub := range []string{
		"configurator/ai-explain", "configurator/ai-query", "explainCheckErrors",
	} {
		if !strings.Contains(js, sub) {
			t.Errorf("в configurator.js нет %q — хук не подключён", sub)
		}
	}
}
