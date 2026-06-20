package launcher

import (
	"bytes"
	"strings"
	"testing"
)

// TestConfigurator_BootstrapWired проверяет, что главный скрипт конфигуратора
// перестал зависеть от серверной интерполяции: данные он читает из
// window.__cfg, переводы — из window.__cfgI18n через хелпер T() (план 55,
// фаза 2b-1). richCfgData("tree") задаёт реальные имена сущностей, которые
// должны попасть в bootstrap-JSON как массив, а не как '...'-литералы.
func TestConfigurator_BootstrapWired(t *testing.T) {
	data := richCfgData("tree")
	// Эмулируем заполнение bootstrap-полей, которое в проде делает loadCfgData.
	populateBootstrap(data, "ru")

	var buf bytes.Buffer
	if err := cfgTmpl.ExecuteTemplate(&buf, "cfg-main", data); err != nil {
		t.Fatalf("рендер cfg-main: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "window.__cfg") {
		t.Error("нет window.__cfg")
	}
	if !strings.Contains(out, "window.__cfgI18n") {
		t.Error("нет window.__cfgI18n")
	}
	// Данные пробрасываются: имя сущности из richCfgData есть в bootstrap JSON.
	if !strings.Contains(out, "Номенклатура") {
		t.Error("entityNames не в bootstrap")
	}
	// Внутри главного скрипта больше нет серверного {{t}} — косвенно: используется
	// рантайм-хелпер T().
	if !strings.Contains(out, "T(") {
		t.Error("нет рантайм-хелпера T()")
	}
	// Чтение данных идёт из __cfg, а не из серверного литерала. Главный скрипт
	// вынесен в /static/configurator.js (план 55 фаза 2b-2) — ищем там.
	if !strings.Contains(configuratorJS(t), "window.__cfg.entityNames") {
		t.Error("_cfgEntityNames не читает window.__cfg.entityNames")
	}
}

// TestConfigurator_BootstrapEmptySlices: пустые срезы в bootstrap кодируются как
// [] (а не null) — фронт делает .map()/.length без guard'ов (фикс ревью I1,
// регресс относительно старого {{range}}, который отдавал []).
func TestConfigurator_BootstrapEmptySlices(t *testing.T) {
	data := &configuratorData{Base: &Base{ID: "b", Name: "T", ConfigSource: "file"}, Lang: "ru"}
	populateBootstrap(data, "ru")
	boot := string(data.Bootstrap)
	for _, k := range []string{`"entityNames":[]`, `"enumNames":[]`, `"groupOrder":[]`} {
		if !strings.Contains(boot, k) {
			t.Errorf("ожидался %s в bootstrap, получено: %s", k, boot)
		}
	}
	if strings.Contains(boot, "null") {
		t.Errorf("в bootstrap есть null (срез не сведён к []): %s", boot)
	}
}
