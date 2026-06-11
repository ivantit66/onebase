package project

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMiniProcProject создаёт минимальный файловый проект с одной обработкой
// «ЗагрузкаКурсов»: processors/<имя>.yaml + src/<имя>.proc.os, и опционально
// заготовку макета src/<имя>.proc.layout.yaml. Возвращает корень проекта.
func writeMiniProcProject(t *testing.T, withLayout bool) string {
	t.Helper()
	dir := t.TempDir()

	mustMkdir := func(sub string) string {
		p := filepath.Join(dir, sub)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		return p
	}
	procDir := mustMkdir("processors")
	srcDir := mustMkdir("src")

	procYAML := "name: ЗагрузкаКурсов\ntitle: Загрузка курсов\n"
	if err := os.WriteFile(filepath.Join(procDir, "загрузкакурсов.yaml"), []byte(procYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	procOS := "Процедура Выполнить()\nКонецПроцедуры\n"
	if err := os.WriteFile(filepath.Join(srcDir, "загрузкакурсов.proc.os"), []byte(procOS), 0o644); err != nil {
		t.Fatal(err)
	}
	if withLayout {
		layout := "name: ЗагрузкаКурсов\nareas:\n  Заголовок:\n    rows:\n      - cells:\n          - text: \"Привет\"\n"
		if err := os.WriteFile(filepath.Join(srcDir, "загрузкакурсов.proc.layout.yaml"), []byte(layout), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLoadProcessorLayout проверяет п.1 issue #48: при загрузке проекта
// заготовка макета src/<имя>.proc.layout.yaml подхватывается в proc.Layout.
func TestLoadProcessorLayout(t *testing.T) {
	dir := writeMiniProcProject(t, true)

	proj, err := Load(dir)
	if err != nil {
		t.Fatalf("project.Load: %v", err)
	}
	defer proj.Close()

	if len(proj.Processors) != 1 {
		t.Fatalf("ожидалась 1 обработка, получено %d", len(proj.Processors))
	}
	proc := proj.Processors[0]
	if proc.Layout == nil {
		t.Fatal("proc.Layout == nil — макет обработки не загружен")
	}
	if _, ok := proc.Layout.Areas["Заголовок"]; !ok {
		t.Errorf("в макете нет области «Заголовок»: %+v", proc.Layout.Areas)
	}
}

// TestLoadProcessorNoLayout проверяет, что без файла макета proc.Layout
// остаётся nil (поведение обработок без макета не меняется).
func TestLoadProcessorNoLayout(t *testing.T) {
	dir := writeMiniProcProject(t, false)

	proj, err := Load(dir)
	if err != nil {
		t.Fatalf("project.Load: %v", err)
	}
	defer proj.Close()

	if len(proj.Processors) != 1 {
		t.Fatalf("ожидалась 1 обработка, получено %d", len(proj.Processors))
	}
	if proj.Processors[0].Layout != nil {
		t.Error("без файла макета proc.Layout должен быть nil")
	}
}
