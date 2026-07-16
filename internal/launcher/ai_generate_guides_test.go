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

// TestExampleForType — инструмент «пример_объекта» отдаёт корректные эталоны для
// самых промахоопасных типов и false для неизвестного.
func TestExampleForType(t *testing.T) {
	cases := map[string]string{
		"форма":      "form:\n  name: ФормаОбъекта",              // обёртка form:, не верхний уровень
		"проведение": "Движения.ОстаткиТоваров.Добавить()",        // верный API движений
		"отчёт":      "query: |",                                  // отчёт = params + query
		"виджет":     "type: kpi",                                 // виджет с обязательным type
		"план счетов": "kind: active",                             // счета
		"журнал":     "documents: [РеализацияТоваров",             // journals
	}
	for kind, needle := range cases {
		ex, ok := exampleForType(kind)
		if !ok {
			t.Errorf("exampleForType(%q) = false, ожидался пример", kind)
			continue
		}
		if !strings.Contains(ex, needle) {
			t.Errorf("пример для %q не содержит %q:\n%s", kind, needle, ex)
		}
	}
	// Синонимы и регистр.
	if _, ok := exampleForType("  ФОРМА  "); !ok {
		t.Error("exampleForType не нормализует регистр/пробелы")
	}
	if _, ok := exampleForType("выдуманныйтип"); ok {
		t.Error("exampleForType для неизвестного типа должен вернуть false")
	}
}

// TestExampleObjectTool_Wired — инструмент «пример_объекта» подключён к генератору.
func TestExampleObjectTool_Wired(t *testing.T) {
	g := &genSession{overlay: t.TempDir(), changed: map[string]bool{}}
	tools, _ := g.tools()
	var found bool
	for _, tl := range tools {
		if tl.Name == "пример_объекта" {
			found = true
			break
		}
	}
	if !found {
		t.Error("инструмент «пример_объекта» не зарегистрирован в g.tools()")
	}
}
