package launcher

import (
	"strings"
	"testing"
)

// newObjectContent("module") должен отдавать файл src/<имя>.module.os со
// стартовой экспортной процедурой. Раньше ветки "module" не было — функция
// возвращала пустой subdir, и создание общего модуля из дерева конфигуратора
// падало с «Неизвестный тип объекта: module» (issue #95).
func TestNewModuleContent(t *testing.T) {
	subdir, content := newObjectContent("module", "ОбщийМодуль")
	if subdir != "src" {
		t.Errorf("subdir = %q, ожидался src", subdir)
	}
	if !strings.Contains(content, "Процедура") || !strings.Contains(content, "Экспорт") {
		t.Errorf("в скелете модуля нет экспортной процедуры: %q", content)
	}
}
