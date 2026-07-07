package configcheck

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFullWithLintWarnings(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "catalogs", "клиент.yaml"), `name: Клиент
unknown_top_key: true
fields:
  - name: Наименование
    type: string
`)
	mkFile(t, filepath.Join(dir, "documents", "заказ.yaml"), `name: Заказ
fields:
  - name: Номер
    type: string
`)
	mkFile(t, filepath.Join(dir, "processors", "мусор.yaml"), `name: Мусор
params: []
`)
	mkFile(t, filepath.Join(dir, "src", "мусор.proc.os"), `Процедура Выполнить() Экспорт
  Перем Лишняя, Нужная;
  Нужная = 1;
  Сообщить(Нужная);
КонецПроцедуры

Процедура Мертвая()
КонецПроцедуры
`)
	mkFile(t, filepath.Join(dir, "roles", "оператор.yaml"), `name: Оператор
permissions:
  catalogs:
    Клиент: [read]
  processors: {}
`)

	plain := RunFull(dir)
	if !plain.OK {
		t.Fatalf("plain check should be OK: %+v", plain.Issues)
	}
	for _, w := range plain.Warnings {
		if w.Code == "metadata.unvalidated-key" || w.Code == "dsl.unused-var" ||
			w.Code == "dsl.dead-procedure" || w.Code == "rbac.object-without-role" {
			t.Fatalf("plain RunFull unexpectedly returned lint warning: %+v", w)
		}
	}

	lint := RunFullWithOptions(dir, Options{Lint: true})
	if !lint.OK {
		t.Fatalf("lint check should keep OK=true for warnings: %+v", lint.Issues)
	}
	want := map[string]bool{
		"metadata.unvalidated-key": false,
		"dsl.unused-var":           false,
		"dsl.dead-procedure":       false,
		"rbac.object-without-role": false,
	}
	for _, w := range lint.Warnings {
		if _, ok := want[w.Code]; ok {
			want[w.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Fatalf("lint warning %s not found; got %+v", code, lint.Warnings)
		}
	}
}

func TestLintYAML_ActivityKeyKnown(t *testing.T) {
	dir := t.TempDir()
	// Блок activity (активность справочников) читается загрузчиком — линт не
	// должен помечать его как неизвестный ключ.
	mkFile(t, filepath.Join(dir, "catalogs", "товар.yaml"), `name: Товар
fields:
  - name: Активный
    type: bool
activity:
  field: Активный
  default_scope: active
  hide_from_choice: true
`)
	for _, is := range CheckLintYAML(dir) {
		if is.Code == "metadata.unvalidated-key" {
			t.Fatalf("блок activity должен быть известен линту, получено: %+v", is)
		}
	}
}

func TestLintYAML_IndexesKeyKnown(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "catalogs", "товар.yaml"), `name: Товар
fields:
  - name: Артикул
    type: string
indexes:
  - fields: [Артикул]
    unique: true
`)
	for _, is := range CheckLintYAML(dir) {
		if is.Code == "metadata.unvalidated-key" {
			t.Fatalf("блок indexes должен быть известен линту, получено: %+v", is)
		}
	}
}

func TestLintProject_ListFormFieldWithoutIndex(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "catalogs", "товар.yaml"), `name: Товар
fields:
  - name: Артикул
    type: string
  - name: Наименование
    type: string
list_form: [Артикул, Наименование]
indexes:
  - fields: [Наименование]
`)

	lint := RunFullWithOptions(dir, Options{Lint: true})
	if !lint.OK {
		t.Fatalf("lint check should keep OK=true for warnings: %+v", lint.Issues)
	}
	var found *Issue
	for i := range lint.Warnings {
		if lint.Warnings[i].Code == "metadata.list-field-without-index" {
			found = &lint.Warnings[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("metadata.list-field-without-index not found; got %+v", lint.Warnings)
	}
	if !strings.Contains(found.Message, "Артикул") {
		t.Fatalf("warning should point to Артикул, got %+v", found)
	}
}

func TestLintProject_ListFormLeadingIndexSuppressesWarning(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "catalogs", "товар.yaml"), `name: Товар
fields:
  - name: Артикул
    type: string
list_form: [Артикул]
indexes:
  - fields: [Артикул]
`)

	lint := RunFullWithOptions(dir, Options{Lint: true})
	for _, w := range lint.Warnings {
		if w.Code == "metadata.list-field-without-index" {
			t.Fatalf("unexpected index warning: %+v", w)
		}
	}
}

func TestLintYAML_JournalConditionalKnown(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "journals", "ж.yaml"), `name: Ж
documents: [Док]
columns:
  - field: Сумма
conditional:
  - when: Сумма < 0
    field: Сумма
    style:
      color: "#c00"
conditional_formatting:
  - when: Документ = "Док"
    then:
      background: yellow
`)
	for _, is := range CheckLintYAML(dir) {
		if is.Code == "metadata.unvalidated-key" {
			t.Fatalf("условное оформление журнала должно быть известно линту, получено: %+v", is)
		}
	}
}

func TestLintYAML_FormConditionalKnown(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "forms", "заказ", "объекта.form.yaml"), `schema: onebase.form/v1
form:
  name: ФормаОбъекта
  kind: object
  entity: Заказ
elements:
  - kind: ТабличнаяЧасть
    name: ТаблицаТовары
    data_path: Объект.Товары
conditional:
  - target: Товары
    when: Количество < 0
    field: Сумма
    style:
      color: "#c00"
conditional_formatting:
  - element: ТаблицаТовары
    when: Сумма < 0
    then:
      background: yellow
`)
	for _, is := range CheckLintYAML(dir) {
		if is.Code == "metadata.unvalidated-key" {
			t.Fatalf("условное оформление формы должно быть известно линту, получено: %+v", is)
		}
	}
}

func TestLintYAML_FormAccessKeyKnown(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "forms", "заказ", "объекта.form.yaml"), `schema: onebase.form/v1
form:
  name: ФормаОбъекта
  kind: object
  entity: Заказ
elements:
  - kind: ПолеВвода
    name: ПолеНомер
    data_path: Объект.Номер
    accesskey: "N"
	  - kind: Кнопка
	    name: КнопкаКопировать
	    accesskey: "7"
	    hotkey: F7
`)
	for _, is := range CheckLintYAML(dir) {
		if is.Code == "metadata.unvalidated-key" {
			t.Fatalf("accesskey формы должен быть известен линту, получено: %+v", is)
		}
	}
}

func TestLintYAML_FormHotkeyWarnings(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "forms", "заказ", "объекта.form.yaml"), `schema: onebase.form/v1
form:
  name: ФормаОбъекта
  kind: object
  entity: Заказ
elements:
  - kind: Кнопка
    name: КнопкаКопировать
    hotkey: F7
  - kind: Кнопка
    name: КнопкаСоздатьНаОсновании
    hotkey: f7
  - kind: Кнопка
    name: КнопкаОбновить
    hotkey: F5
  - kind: ПолеВвода
    name: ПолеНомер
    data_path: Объект.Номер
    hotkey: F8
`)
	got := map[string]int{}
	for _, is := range CheckLintYAML(dir) {
		got[is.Code]++
	}
	for _, code := range []string{"form.duplicate-hotkey", "form.unsupported-hotkey", "form.ignored-hotkey"} {
		if got[code] == 0 {
			t.Fatalf("ожидался warning %s, получено: %+v", code, got)
		}
	}
	if got["metadata.unvalidated-key"] != 0 {
		t.Fatalf("hotkey формы должен быть известен линту, получено: %+v", got)
	}
}

func TestLintCrossScopeRead(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "processors", "касса.yaml"), `name: Касса
params: []
`)
	// ПрочитатьСекрет читает «Секрет», объявленную только в Выполнить: сегодня
	// работает лишь из-за утечки области видимости вызова.
	mkFile(t, filepath.Join(dir, "src", "касса.proc.os"), `Процедура Выполнить() Экспорт
  Секрет = 42;
  Сообщить(ПрочитатьСекрет());
КонецПроцедуры

Функция ПрочитатьСекрет()
  Возврат Секрет;
КонецФункции
`)

	lint := RunFullWithOptions(dir, Options{Lint: true})
	if !lint.OK {
		t.Fatalf("lint should keep OK=true for warnings: %+v", lint.Issues)
	}
	var found *Issue
	for i := range lint.Warnings {
		if lint.Warnings[i].Code == "dsl.cross-scope-read" {
			found = &lint.Warnings[i]
		}
	}
	if found == nil {
		t.Fatalf("dsl.cross-scope-read not found; got %+v", lint.Warnings)
	}
	if !strings.Contains(found.Message, "Секрет") {
		t.Errorf("message should name the leaked variable: %q", found.Message)
	}
	if found.Line != 7 {
		t.Errorf("expected warning at line 7 (Возврат Секрет), got %d", found.Line)
	}
}

func TestLintCrossScopeRead_ParamIsClean(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "processors", "касса.yaml"), `name: Касса
params: []
`)
	// Здесь «Секрет» передаётся параметром — утечки нет, предупреждения быть не должно.
	mkFile(t, filepath.Join(dir, "src", "касса.proc.os"), `Процедура Выполнить() Экспорт
  Секрет = 42;
  Сообщить(ПрочитатьСекрет(Секрет));
КонецПроцедуры

Функция ПрочитатьСекрет(Секрет)
  Возврат Секрет;
КонецФункции
`)

	lint := RunFullWithOptions(dir, Options{Lint: true})
	for _, w := range lint.Warnings {
		if w.Code == "dsl.cross-scope-read" {
			t.Fatalf("unexpected cross-scope-read for a parameter: %+v", w)
		}
	}
}
