package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFormatGuides_InSystemPrompt — все схемы объектов должны попадать в
// системный промпт генератора, иначе модель угадывает формат (управляемая
// форма, проведение, отчёты/виджеты/план счетов и т.д.).
func TestFormatGuides_InSystemPrompt(t *testing.T) {
	cases := map[string][]string{
		"управляемая форма": {"forms/<сущность-в-нижнем-регистре>/<Форма>.form.yaml", "ПОД ключом form:", "data_path", "АВТОГЕНЕРИРУЕТСЯ"},
		"проведение":        {"src/<Документ>.posting.os", "Движения.<Регистр>.Добавить()", "РегистрыНакопления.X.СоздатьДвижение() (такого API нет)", "this.<Имя>"},
		"прочие объекты":    {"reports/<Имя>.yaml", "type: kpi|list|chart|actions|recent", "accounts/<Имя>.yaml", "subconto", "permissions:", "root_url", "schedule:"},
		"пути файлов":       {"Пути файлов", "ПЛОСКО в src/", "forms/<сущность-в-нижнем-регистре>/<Форма>.form.yaml"},
	}
	for name, needles := range cases {
		for _, n := range needles {
			if !strings.Contains(aiGenerateSystem, n) {
				t.Errorf("[%s] в aiGenerateSystem нет %q", name, n)
			}
		}
	}
}

// TestShowObject_AllDirs — «показать_объект» ищет не только в 7 метаданных-
// папках, но и в reports/widgets/… и пробует оба регистра имени файла (нижний,
// как пишет генератор, и исходный, как в examples/).
func TestShowObject_AllDirs(t *testing.T) {
	tmp := t.TempDir()
	g := &genSession{overlay: tmp, changed: map[string]bool{}}

	mustWrite := func(rel, body string) {
		full := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Отчёт — имя файла в нижнем регистре (как пишет createObject).
	mustWrite("reports/тестотчёт.yaml", "name: ТестОтчёт\ntitle: Тест\n")
	// Виджет — имя файла в исходном регистре (как в examples/).
	mustWrite("widgets/Виджет1.yaml", "name: Виджет1\ntype: kpi\n")

	if got := g.showObject("ТестОтчёт"); !strings.Contains(got, "name: ТестОтчёт") {
		t.Errorf("showObject(ТестОтчёт) не нашёл отчёт: %q", got)
	}
	if got := g.showObject("Виджет1"); !strings.Contains(got, "type: kpi") {
		t.Errorf("showObject(Виджет1) не нашёл виджет по исходному регистру: %q", got)
	}
	if got := g.showObject("НетТакого"); strings.Contains(got, "name:") {
		t.Errorf("showObject(НетТакого) должен вернуть «не найден», а вернул: %q", got)
	}
}
