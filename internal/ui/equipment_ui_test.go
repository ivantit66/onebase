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

// Мост onebaseDevice встроен в общий layout (head) и доступен на каждой странице.
func TestUI_DeviceBridge_InHead(t *testing.T) {
	html := renderPage(t, "page-index")
	for _, want := range []string{"window.onebaseDevice", "obAgentURL", "X-Agent-Token", "/print", "/weight", "/pay", "/fiscal", "/events"} {
		if !strings.Contains(html, want) {
			t.Errorf("в layout нет %q", want)
		}
	}
}

// Пункты меню РМК и настроек агента попадают в общий nav.
func TestUI_NavLinks(t *testing.T) {
	html := renderPage(t, "page-index")
	for _, want := range []string{`href="/ui/pos"`, `href="/ui/settings/agent"`} {
		if !strings.Contains(html, want) {
			t.Errorf("в меню нет ссылки %s", want)
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
