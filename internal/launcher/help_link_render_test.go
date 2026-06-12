package launcher

import (
	"bytes"
	"strings"
	"testing"
)

// Issue #42: ссылка на синтакс-помощник должна быть в меню и топбаре конфигуратора.
func renderCfgHead(t *testing.T) string {
	t.Helper()
	data := &configuratorData{Base: &Base{ID: "test-base", Name: "Тест", ConfigSource: "file"}, Lang: "ru"}
	var buf bytes.Buffer
	if err := cfgTmpl.ExecuteTemplate(&buf, "cfg-head", data); err != nil {
		t.Fatalf("ExecuteTemplate cfg-head: %v", err)
	}
	return buf.String()
}

// Пункт меню «Справка по встроенному языку» должен вызывать toggleSyntaxRef.
func TestConfigurator_HelpLinkInMenu(t *testing.T) {
	html := renderCfgHead(t)
	if !strings.Contains(html, "Справка по встроенному языку") {
		t.Error("в cfg-head нет пункта меню «Справка по встроенному языку»")
	}
	if !strings.Contains(html, `onclick="toggleSyntaxRef()`) {
		t.Error("в cfg-head пункт меню не вызывает toggleSyntaxRef()")
	}
}

// Кнопка справки должна быть в топбаре.
func TestConfigurator_HelpButtonInTopbar(t *testing.T) {
	html := renderCfgHead(t)
	if !strings.Contains(html, "Справка") {
		t.Error("в cfg-head нет кнопки «Справка» в топбаре")
	}
	if !strings.Contains(html, `onclick="toggleSyntaxRef()"`) {
		t.Error("в cfg-head кнопка топбара не вызывает toggleSyntaxRef()")
	}
}
