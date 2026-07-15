package ui

import (
	"bytes"
	"strings"
	"testing"
)

// renderPage исполняет шаблон страницы с минимальным набором данных, которые
// иначе инжектит render() (Cfg/Nav/Subsystems/IsAdmin). Заодно ловит ошибки
// разбора всего набора шаблонов (template.Must в init).
func renderPage(t *testing.T, name string) string {
	t.Helper()
	var buf bytes.Buffer
	data := map[string]any{
		"Lang":             "ru", // в проде инжектит render(); nav использует {{t $.Lang …}}
		"Cfg":              Config{AppName: "TestApp"},
		"Nav":              nil,
		"Subsystems":       nil,
		"CurrentSubsystem": "",
		"IsAdmin":          true,
	}
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return buf.String()
}

// Мост onebaseDevice подключён общим layout и доступен на каждой странице.
func TestUI_DeviceBridge_InHead(t *testing.T) {
	html := renderPage(t, "page-index")
	if !strings.Contains(html, `src="/static/ui.js"`) {
		t.Fatalf("layout не подключает /static/ui.js")
	}
	js := string(uiJS)
	for _, want := range []string{"window.onebaseDevice", "obAgentURL", "X-Agent-Token", "/print", "/weight", "/pay", "/fiscal", "/events"} {
		if !strings.Contains(js, want) {
			t.Errorf("в ui.js нет %q", want)
		}
	}
}

func TestUI_IndexChartsUseJSONBootstrap(t *testing.T) {
	html := renderPage(t, "page-index")
	if !strings.Contains(html, `id="ob-widget-charts"`) {
		t.Fatalf("dashboard не содержит JSON-якорь графиков")
	}
	if strings.Contains(html, `window.__obWidgetCharts`) {
		t.Fatalf("dashboard снова генерирует inline JS для графиков")
	}
}

// РМК — платформенная функция: администратор открывает её через «Все
// функции», но прежний доступ обычного кассира не пропадает. Настройки агента
// остаются в системной группе администратора.
func TestUI_PlatformLinks(t *testing.T) {
	html := renderPage(t, "page-index")
	if !strings.Contains(html, `href="/ui/settings/agent"`) {
		t.Error("в меню нет настроек агента оборудования")
	}
	if strings.Contains(html, `href="/ui/pos"`) {
		t.Error("у администратора РМК не должен оставаться отдельным пунктом меню «Система»")
	}
	userNav := renderNav(t, false)
	if !strings.Contains(userNav, `href="/ui/pos"`) || !strings.Contains(userNav, "Платформенные возможности") {
		t.Error("обычный пользователь потерял доступ к РМК после переноса в «Все функции» администратора")
	}

	allFunctions := renderPage(t, "page-all-functions")
	for _, want := range []string{"Платформенные возможности", `href="/ui/pos"`, "Рабочее место кассира (РМК)"} {
		if !strings.Contains(allFunctions, want) {
			t.Errorf("в «Все функции» нет %q", want)
		}
	}
}

func TestUI_SystemMenuIsGroupedAndScrollable(t *testing.T) {
	html := renderPage(t, "page-index")
	for _, want := range []string{
		`class="sys-group"`,
		"Профиль и интерфейс",
		"Администрирование",
		"Интеграции и задания",
		"Обслуживание базы",
		"Расширения и оборудование",
		"Инструменты разработчика",
		"max-height:calc(100vh - 50px)",
		"overflow-y:auto",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("сгруппированное меню «Система» не содержит %q", want)
		}
	}
}

func TestUI_AgentSettingsPage(t *testing.T) {
	html := renderPage(t, "page-agent-settings")
	for _, want := range []string{"Настройки агента", "ag-url", "ag-token", "Проверить связь", "obAgentToken"} {
		if !strings.Contains(html, want) {
			t.Errorf("страница настроек агента без %q", want)
		}
	}
}

func TestUI_POSPage(t *testing.T) {
	html := renderPage(t, "page-pos")
	for _, want := range []string{
		"Рабочее место кассира",
		"Напечатать чек", "Открыть ящик",
		"Пробить фискальный чек",
		"Получить вес", "Оплатить картой",
		"Подключить сканер", "pos-barcode",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("РМК-страница без %q", want)
		}
	}
}
