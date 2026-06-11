# План 63 — реализация: фиксы ишью #48 и #49

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Закрыть ишью #48 (пять дефектов импорта конфигураций 1С) и #49 (локализация лаунчера и системных сообщений) двумя PR.

**Architecture:** PR 1 — точечные правки конвертера (`internal/converter`) и лексера DSL; PR 2 — однострочный фикс выбора языка лаунчера + новый пакет `i18nerr` (русский шаблон-ключ + аргументы, перевод на HTTP-границе) + механический прогон сообщений.

**Tech Stack:** Go, yaml.v3, существующий `internal/i18n` (русские строки как ключи словарей).

**Спека:** `Plans/63-issues-48-49-fixes.md`. Конвенции: коммиты `тип(scope): описание` по-русски; pre-commit `i18ncheck` блокирует коммит, если t-ключ не переведён ни в одной нерусской локали — лечится добавлением ключа в `internal/i18n/locales/en.json`.

---

## PR 1 — ветка `fix/48-converter-import` (уже создана, спека закоммичена)

### Task 1: Хелпер `objectNames` + плоские справочники

**Files:**
- Modify: `internal/converter/parser1c/metadata.go` (parseCatalogs:291, parseEnumerations:545)
- Test: `internal/converter/parser1c/parsedir_test.go`

- [ ] **Step 1: Написать падающий тест**

В `parsedir_test.go` добавить (используется существующий `catalogXML`):

```go
// Объект без подчинённых элементов (форм, макетов) лежит в выгрузке ОДНИМ
// файлом «Имя.xml» без папки-компаньона. Раньше все парсеры, кроме
// parseEnumerations, перебирали только подкаталоги и молча теряли такие
// объекты (issue #48 п.1: «не импортированы 2 справочника и 4 константы»).
func TestParseDirFlatCatalog(t *testing.T) {
	src := t.TempDir()
	catsDir := filepath.Join(src, "Catalogs")
	if err := os.MkdirAll(catsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Одиночный .xml без папки-компаньона.
	if err := os.WriteFile(filepath.Join(catsDir, "Контрагенты.xml"), []byte(catalogXML), 0o644); err != nil {
		t.Fatalf("write xml: %v", err)
	}

	dump, err := ParseDir(src)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(dump.Catalogs) != 1 {
		t.Fatalf("ожидался 1 справочник из плоского xml, получено %d", len(dump.Catalogs))
	}
	if dump.Catalogs[0].Name != "Контрагенты" {
		t.Errorf("имя справочника: %q", dump.Catalogs[0].Name)
	}
	if len(dump.Catalogs[0].Attributes) != 1 || dump.Catalogs[0].Attributes[0].Name != "ИНН" {
		t.Fatalf("реквизиты не разобраны: %+v", dump.Catalogs[0].Attributes)
	}
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/converter/parser1c/ -run TestParseDirFlatCatalog -v`
Expected: FAIL — «ожидался 1 справочник из плоского xml, получено 0».

- [ ] **Step 3: Добавить хелпер и переписать parseCatalogs**

В `metadata.go` перед `parseCatalogs` добавить:

```go
// objectNames возвращает имена объектов раздела выгрузки: объединение имён
// подкаталогов и одиночных *.xml-файлов (без расширения), без дубликатов.
// Объект без подчинённых элементов представлен в выгрузке 1С только файлом
// «Имя.xml» — перебор одних подкаталогов молча терял такие объекты
// (issue #16 для перечислений, issue #48 п.1 для остальных типов).
func objectNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}
	for _, e := range entries {
		if e.IsDir() {
			add(e.Name())
		} else if strings.EqualFold(filepath.Ext(e.Name()), ".xml") {
			add(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		}
	}
	return names
}
```

В `parseCatalogs` заменить заголовок цикла — вместо:

```go
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
```

написать:

```go
	for _, name := range objectNames(dir) {
```

(тело цикла не меняется: ветка `parseV83File` обрабатывает плоский xml, ветка
`Metadata.xml` — старый формат с каталогом; `scanForms` на отсутствующем
каталоге вернёт nil).

- [ ] **Step 4: Прогнать тесты**

Run: `go test ./internal/converter/... -v`
Expected: PASS (включая старые TestParseDirV83, TestParseDirSkipsNonApplied).

- [ ] **Step 5: Commit**

```
git add internal/converter/parser1c
git commit -m "fix(converter): плоские xml-справочники без папки-компаньона не терялись (#48)"
```

### Task 2: `objectNames` во всех остальных парсерах

**Files:**
- Modify: `internal/converter/parser1c/metadata.go` (parseDocuments:352, parseRegisters:408, parseEnumerations:545, parseConstants:587, parseInfoRegisters:611, parseAccountingRegisters:638, parseChartsOfAccounts:665, parseScheduledJobs:690, parseCommonModules:716, parseDataProcessors:753)
- Test: `internal/converter/parser1c/parsedir_test.go`

- [ ] **Step 1: Написать падающий тест на константы и регистры сведений**

```go
const constantXML = `<?xml version="1.0" encoding="UTF-8"?>
<MetaDataObject>
  <Constant>
    <Properties>
      <Name>ВалютаУчета</Name>
      <Type><Type xmlns="http://v8.1c.ru/8.1/data/core">xs:string</Type></Type>
    </Properties>
  </Constant>
</MetaDataObject>`

const infoRegFlatXML = `<?xml version="1.0" encoding="UTF-8"?>
<MetaDataObject>
  <InformationRegister>
    <Properties><Name>КурсыВалют</Name></Properties>
    <ChildObjects>
      <Dimension><Properties>
        <Name>Валюта</Name>
        <Type><Type xmlns="http://v8.1c.ru/8.1/data/core">xs:string</Type></Type>
      </Properties></Dimension>
      <Resource><Properties>
        <Name>Курс</Name>
        <Type><Type xmlns="http://v8.1c.ru/8.1/data/core">xs:decimal</Type></Type>
      </Properties></Resource>
    </ChildObjects>
  </InformationRegister>
</MetaDataObject>`

// Константы и регистры сведений тоже бывают «плоскими» (issue #48 п.1:
// «НЕ ИМПОРТИРОВАНЫ 4 КОНСТАНТЫ»).
func TestParseDirFlatConstantsAndInfoRegs(t *testing.T) {
	src := t.TempDir()
	for dir, files := range map[string]map[string]string{
		"Constants":            {"ВалютаУчета.xml": constantXML},
		"InformationRegisters": {"КурсыВалют.xml": infoRegFlatXML},
	} {
		if err := os.MkdirAll(filepath.Join(src, dir), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(src, dir, name), []byte(content), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
	}

	dump, err := ParseDir(src)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(dump.Constants) != 1 || dump.Constants[0].Name != "ВалютаУчета" {
		t.Fatalf("константа из плоского xml не разобрана: %+v", dump.Constants)
	}
	if len(dump.InfoRegisters) != 1 || dump.InfoRegisters[0].Name != "КурсыВалют" {
		t.Fatalf("регистр сведений из плоского xml не разобран: %+v", dump.InfoRegisters)
	}
	if len(dump.InfoRegisters[0].Dimensions) != 1 || dump.InfoRegisters[0].Dimensions[0].Name != "Валюта" {
		t.Fatalf("измерения регистра не разобраны: %+v", dump.InfoRegisters[0].Dimensions)
	}
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/converter/parser1c/ -run TestParseDirFlatConstantsAndInfoRegs -v`
Expected: FAIL — «константа из плоского xml не разобрана».

- [ ] **Step 3: Переписать остальные парсеры**

Во всех десяти функциях (`parseDocuments`, `parseRegisters`, `parseEnumerations`,
`parseConstants`, `parseInfoRegisters`, `parseAccountingRegisters`,
`parseChartsOfAccounts`, `parseScheduledJobs`, `parseCommonModules`,
`parseDataProcessors`) применить ту же замену, что в Task 1 Step 3:
блок `entries, err := os.ReadDir(dir) … if !e.IsDir() { continue } … name := e.Name()`
заменяется на `for _, name := range objectNames(dir) {`. Тела циклов не меняются.
В `parseEnumerations` при этом удаляется собственный локальный сбор имён
(строки 547–571 — `entries…seen…names`) — он заменяется хелпером.

- [ ] **Step 4: Прогнать тесты пакета**

Run: `go test ./internal/converter/... -v`
Expected: PASS, включая существующий `TestParseDirEnumerations` (поведение
перечислений не изменилось).

- [ ] **Step 5: Commit**

```
git add internal/converter/parser1c
git commit -m "fix(converter): objectNames для всех типов объектов — плоские xml не теряются (#48)"
```

### Task 3: `sanitizeBSL` — вырезание директив препроцессора

**Files:**
- Modify: `internal/converter/writer/dsl.go` (WriteModules:73, buildStub:56-66), `internal/converter/writer/yaml.go` (WriteProcessors:387)
- Test: создать `internal/converter/writer/sanitize_test.go`

- [ ] **Step 1: Написать падающий тест**

```go
package writer

import (
	"strings"
	"testing"
)

// Директивы препроцессора 1С (#Область, #Если…) не поддерживаются DSL —
// загрузка конфигурации падала на «expected Procedure or Function, got "#"»
// (issue #48 п.2). Конвертер обязан их вырезать, сохраняя содержимое блоков.
func TestSanitizeBSL(t *testing.T) {
	in := strings.Join([]string{
		"#Область Сервис",
		"Процедура Привет() Экспорт",
		"  #Если Сервер Тогда",
		"  а = 1;",
		"  #ИначеЕсли Клиент Тогда",
		"  а = 2;",
		"  #Иначе",
		"  а = 3;",
		"  #КонецЕсли",
		"КонецПроцедуры",
		"#КонецОбласти",
		"#Region English",
		"#EndRegion",
	}, "\n")
	got := sanitizeBSL(in)
	if strings.Contains(got, "#") {
		t.Fatalf("директивы не вырезаны:\n%s", got)
	}
	for _, want := range []string{"Процедура Привет() Экспорт", "а = 1;", "а = 2;", "а = 3;", "КонецПроцедуры"} {
		if !strings.Contains(got, want) {
			t.Fatalf("потеряно содержимое %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/converter/writer/ -run TestSanitizeBSL -v`
Expected: FAIL — «undefined: sanitizeBSL» (ошибка компиляции).

- [ ] **Step 3: Реализовать sanitizeBSL и применить**

В `writer/dsl.go` (импорт `regexp` добавить):

```go
// preprocDirective матчит строку-директиву препроцессора 1С (рус/англ).
var preprocDirective = regexp.MustCompile(`(?i)^\s*#\s*(Область|КонецОбласти|Если|ИначеЕсли|Иначе|КонецЕсли|Region|EndRegion|If|ElsIf|Else|EndIf)\b`)

// sanitizeBSL убирает из исходника 1С строки-директивы препроцессора:
// DSL OneBase их не поддерживает (issue #48 п.2). Содержимое блоков #Если
// сохраняется целиком (обе ветки).
func sanitizeBSL(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if preprocDirective.MatchString(line) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
```

Применить в трёх местах:
1. `WriteModules` (dsl.go:79): `source := mod.Source` → `source := sanitizeBSL(mod.Source)`.
2. `WriteProcessors` (yaml.go:387): `source := proc.Source` → `source := sanitizeBSL(proc.Source)`.
3. `buildStub` (dsl.go:58): `if bsl, err := os.ReadFile(bslPath); err == nil {` — строкой ниже
   заменить использование `string(bsl)` на `sanitizeBSL(string(bsl))`:
   `for _, line := range strings.Split(sanitizeBSL(string(bsl)), "\n") {`.

- [ ] **Step 4: Прогнать тесты**

Run: `go test ./internal/converter/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/converter/writer
git commit -m "fix(converter): вырезание директив препроцессора (#Область, #Если) из .bsl (#48)"
```

### Task 4: Лексер DSL — `#` как комментарий до конца строки

**Files:**
- Modify: `internal/dsl/lexer/lexer.go:47-61` (метод skip)
- Test: `internal/dsl/lexer/lexer_test.go`

- [ ] **Step 1: Написать падающий тест**

В `lexer_test.go` добавить:

```go
// Строки с директивами препроцессора 1С (#Область и т.п.) пропускаются как
// комментарии — спасает копипаст кода из 1С (issue #48 п.2).
func TestSkipsPreprocessorLines(t *testing.T) {
	src := "#Область Сервис\nПроцедура П()\nКонецПроцедуры\n#КонецОбласти\n"
	l := New(src, "t.os")
	tok := l.NextToken()
	if tok.Type != token.LookupIdent("Процедура") || tok.Literal != "Процедура" {
		t.Fatalf("первый токен: %+v, ожидалась Процедура", tok)
	}
	for tok.Type != token.EOF {
		if tok.Type == token.ILLEGAL {
			t.Fatalf("ILLEGAL токен: %+v", tok)
		}
		tok = l.NextToken()
	}
}
```

(если в файле нет импорта `token` — добавить
`"github.com/ivantit66/onebase/internal/dsl/token"`).

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/dsl/lexer/ -run TestSkipsPreprocessorLines -v`
Expected: FAIL — первый токен ILLEGAL `#`.

- [ ] **Step 3: Реализовать**

В `lexer.go`, метод `skip()`, добавить case между обработкой `//` и `default`:

```go
		case r == '#':
			// Директива препроцессора 1С (#Область, #Если…) — пропускаем
			// строку как комментарий: конвертер 1С→OneBase вырезает их сам,
			// но копипаст из 1С не должен валить разбор (issue #48 п.2).
			for l.pos < len(l.input) && l.peek() != '\n' {
				l.next()
			}
```

- [ ] **Step 4: Прогнать тесты DSL**

Run: `go test ./internal/dsl/... `
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/dsl/lexer
git commit -m "feat(dsl): лексер пропускает строки-директивы препроцессора # (#48)"
```

### Task 5: `EnumRef.X` → `enum:X`

**Files:**
- Modify: `internal/converter/parser1c/typemap.go:38-39`
- Test: `internal/converter/parser1c/parsedir_test.go`

- [ ] **Step 1: Написать падающий тест**

```go
// Платформа поддерживает тип enum:Имя (metadata/yaml.go), а конвертер
// деградировал ссылки на перечисления в string (issue #48 п.5).
func TestMapTypeEnumRef(t *testing.T) {
	for _, primary := range []string{"cfg:EnumRef.ВидКонтрагента", "ПеречислениеСсылка.ВидКонтрагента"} {
		got, note := MapType(FieldType{Primary: primary, RefObject: "ВидКонтрагента"})
		if got != "enum:ВидКонтрагента" {
			t.Errorf("%s → %q, ожидалось enum:ВидКонтрагента", primary, got)
		}
		if note != "" {
			t.Errorf("%s: неожиданное предупреждение %q", primary, note)
		}
	}
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/converter/parser1c/ -run TestMapTypeEnumRef -v`
Expected: FAIL — «cfg:EnumRef.ВидКонтрагента → "string"».

- [ ] **Step 3: Реализовать**

В `typemap.go` заменить:

```go
	case strings.HasPrefix(p, "EnumRef.") || strings.HasPrefix(p, "ПеречислениеСсылка."):
		return "string", "перечисление → string"
```

на:

```go
	case strings.HasPrefix(p, "EnumRef.") || strings.HasPrefix(p, "ПеречислениеСсылка."):
		obj := extractRefName(p)
		if t.RefObject != "" {
			obj = t.RefObject
		}
		return "enum:" + obj, ""
```

- [ ] **Step 4: Прогнать тесты**

Run: `go test ./internal/converter/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/converter/parser1c
git commit -m "fix(converter): EnumRef.X маппится в enum:X вместо string (#48)"
```

### Task 6: Формы обработок

**Files:**
- Modify: `internal/converter/parser1c/types.go:21-27,119-124`, `internal/converter/parser1c/metadata.go` (parseDataProcessors), `internal/converter/convert.go:134-143`
- Test: `internal/converter/parser1c/parsedir_test.go`

- [ ] **Step 1: Написать падающий тест**

```go
const processorXML = `<?xml version="1.0" encoding="UTF-8"?>
<MetaDataObject>
  <DataProcessor>
    <Properties><Name>ЗагрузкаКурсов</Name></Properties>
    <ChildObjects/>
  </DataProcessor>
</MetaDataObject>`

// Формы обработок поддерживаются платформой (loadProcessorForms грузит их из
// forms/<обработка>/), но конвертер их не сканировал (issue #48 п.3).
func TestParseDirProcessorForms(t *testing.T) {
	src := t.TempDir()
	writeV83(t, filepath.Join(src, "DataProcessors"), "ЗагрузкаКурсов", processorXML)
	extDir := filepath.Join(src, "DataProcessors", "ЗагрузкаКурсов", "Forms", "Форма", "Ext")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatalf("mkdir form: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "Form.xml"), []byte("<Form/>"), 0o644); err != nil {
		t.Fatalf("write form xml: %v", err)
	}

	dump, err := ParseDir(src)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(dump.Processors) != 1 {
		t.Fatalf("ожидалась 1 обработка, получено %d", len(dump.Processors))
	}
	p := dump.Processors[0]
	if len(p.Forms) != 1 {
		t.Fatalf("ожидалась 1 форма обработки, получено %d", len(p.Forms))
	}
	if p.Forms[0].Entity != "ЗагрузкаКурсов" || p.Forms[0].FormName != "Форма" {
		t.Errorf("источник формы: %+v", p.Forms[0])
	}
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/converter/parser1c/ -run TestParseDirProcessorForms -v`
Expected: FAIL — «p.Forms undefined» (ошибка компиляции).

- [ ] **Step 3: Реализовать**

1. `types.go`: в `ProcessorMeta` добавить поле `Forms []FormSource` (после `Source string`).
   Комментарий `FormSource` обновить: `// Entity — имя объекта-владельца OneBase (справочник/документ/обработка)`.
2. `metadata.go`, `parseDataProcessors`: после блока чтения `ObjectModule.bsl`
   (перед `result = append(result, proc)`) добавить:

```go
		proc.Forms = scanForms(filepath.Join(dir, name), proc.Name)
```

3. `convert.go`, `writeForms`: после цикла по `dump.Documents` добавить:

```go
	for _, p := range dump.Processors {
		all = append(all, p.Forms...)
	}
```

   и обновить комментарий функции: «…формы справочников, документов и обработок…».

- [ ] **Step 4: Прогнать тесты**

Run: `go test ./internal/converter/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/converter
git commit -m "feat(converter): импорт управляемых форм обработок (#48)"
```

### Task 7: Макеты обработок → layout-скаффолд

**Files:**
- Modify: `internal/converter/writer/templates.go`, `internal/converter/writer/report.go`
- Test: создать `internal/converter/writer/templates_test.go`

- [ ] **Step 1: Написать падающий тест**

```go
package writer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/printform"
)

func writeTemplateSrc(t *testing.T, sourceDir, kind, owner, name string) {
	t.Helper()
	ext := filepath.Join(sourceDir, kind, owner, "Templates", name, "Ext")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ext, "Template.mxl"), []byte("mxl"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// Макеты обработок шли в printforms/ с document:<Обработка>, но printform
// привязывается только к документам/справочникам — привязка была мёртвой
// (issue #48 п.4). Теперь они становятся src/<имя>.proc.layout.yaml.
func TestWriteTemplatesProcessorLayouts(t *testing.T) {
	src := t.TempDir()
	out := t.TempDir()
	writeTemplateSrc(t, src, "DataProcessors", "ЗагрузкаКурсов", "Заголовок")
	writeTemplateSrc(t, src, "DataProcessors", "ЗагрузкаКурсов", "Строка")
	writeTemplateSrc(t, src, "Documents", "Реализация", "Накладная")

	rep := &ConversionReport{}
	if err := WriteTemplates(src, out, rep); err != nil {
		t.Fatalf("WriteTemplates: %v", err)
	}

	// Макеты обработки — один layout с двумя областями.
	layoutPath := filepath.Join(out, "src", "загрузкакурсов.proc.layout.yaml")
	lt, err := printform.LoadLayout(layoutPath)
	if err != nil {
		t.Fatalf("layout не создан или не парсится: %v", err)
	}
	if len(lt.Areas) != 2 {
		t.Fatalf("ожидались 2 области, получено %d: %+v", len(lt.Areas), lt.Areas)
	}
	for _, area := range []string{"Заголовок", "Строка"} {
		if _, ok := lt.Areas[area]; !ok {
			t.Errorf("нет области %q", area)
		}
	}
	// Исходники скопированы рядом.
	if _, err := os.Stat(filepath.Join(out, "src", "загрузкакурсов_заголовок.src.mxl")); err != nil {
		t.Errorf("исходник макета не скопирован: %v", err)
	}
	// В printforms/ макеты обработки НЕ попали, а макет документа — попал.
	if _, err := os.Stat(filepath.Join(out, "printforms", "загрузкакурсов_заголовок.yaml")); err == nil {
		t.Errorf("макет обработки не должен попадать в printforms/")
	}
	if _, err := os.Stat(filepath.Join(out, "printforms", "реализация_накладная.yaml")); err != nil {
		t.Errorf("макет документа должен остаться в printforms/: %v", err)
	}
	// Отчёт упоминает layout-заготовку.
	if len(rep.ProcessorLayouts) != 1 || !strings.Contains(rep.String(), "загрузкакурсов.proc.layout.yaml") {
		t.Errorf("отчёт не упоминает layout: %+v", rep.ProcessorLayouts)
	}
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/converter/writer/ -run TestWriteTemplatesProcessorLayouts -v`
Expected: FAIL — «rep.ProcessorLayouts undefined» (ошибка компиляции).

- [ ] **Step 3: Реализовать**

1. `report.go`: в `ConversionReport` добавить поле
   `ProcessorLayouts []string // src/*.proc.layout.yaml — заготовки макетов обработок`.
   В `String()` после блока DSLStubs добавить:

```go
	if len(r.ProcessorLayouts) > 0 {
		sb.WriteString("\nМакеты обработок → заготовки макетов (перенесите оформление вручную):\n")
		for _, name := range r.ProcessorLayouts {
			sb.WriteString("  src/" + name + "\n")
		}
	}
```

2. `templates.go`:
   - в `templateSource` добавить поле `OwnerKind string // раздел выгрузки (Catalogs/DataProcessors/…)`;
   - `scanTemplateDir(templatesDir, owner string)` → `scanTemplateDir(templatesDir, owner, ownerKind string)`;
     внутри: `templateSource{Owner: owner, OwnerKind: ownerKind, Name: e.Name(), Src: src}`;
   - в `scanTemplates`: `scanTemplateDir(filepath.Join(kindDir, obj.Name(), "Templates"), obj.Name(), kind)` и
     `scanTemplateDir(filepath.Join(sourceDir, "CommonTemplates"), "", "")`;
   - в начале `WriteTemplates` (после `if len(tmpls) == 0`) разделить макеты:

```go
	// Макеты обработок → заготовки макетов src/<имя>.proc.layout.yaml
	// (issue #48 п.4): printform-привязка работает только для документов и
	// справочников, а layout подхватывается FindLayoutFile для .proc.os и
	// доступен из DSL через Макет.Область("<имя макета 1С>").
	procTmpls := map[string][]templateSource{}
	var rest []templateSource
	for _, t := range tmpls {
		if t.OwnerKind == "DataProcessors" && t.Owner != "" {
			procTmpls[t.Owner] = append(procTmpls[t.Owner], t)
		} else {
			rest = append(rest, t)
		}
	}
	if err := writeProcessorLayouts(procTmpls, outDir, notes); err != nil {
		return err
	}
	if len(rest) == 0 {
		return nil
	}
```

     и далее существующий цикл `for _, t := range tmpls` заменить на `for _, t := range rest`;
   - новая функция (импорты `sort`, `gopkg.in/yaml.v3`, `github.com/ivantit66/onebase/internal/printform` добавить):

```go
// writeProcessorLayouts пишет для каждой обработки src/<имя>.proc.layout.yaml:
// каждый макет 1С — именованная область с TODO-ячейкой; исходники макетов
// копируются рядом как src/<обработка>_<макет>.src.<ext>.
func writeProcessorLayouts(groups map[string][]templateSource, outDir string, notes *ConversionReport) error {
	if len(groups) == 0 {
		return nil
	}
	srcDir := filepath.Join(outDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return err
	}
	owners := make([]string, 0, len(groups))
	for o := range groups {
		owners = append(owners, o)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		lt := printform.LayoutTemplate{Name: owner, Areas: map[string]*printform.LayoutArea{}}
		for _, t := range groups[owner] {
			srcNote := ""
			if t.Src != "" {
				ext := filepath.Ext(t.Src)
				srcName := fileName(owner) + "_" + fileName(t.Name) + ".src" + ext
				if err := copyFileRaw(t.Src, filepath.Join(srcDir, srcName)); err != nil {
					notes.FormWarnings = append(notes.FormWarnings,
						fmt.Sprintf("макет %s/%s: не удалось скопировать исходник: %v", owner, t.Name, err))
				} else {
					srcNote = " (исходник: src/" + srcName + ")"
				}
			}
			lt.Areas[t.Name] = &printform.LayoutArea{Rows: []printform.LayoutRow{{
				Cells: []printform.LayoutCell{{Text: "TODO: перенесите оформление макета 1С «" + t.Name + "»" + srcNote}},
			}}}
			notes.Templates++
		}
		data, err := yaml.Marshal(&lt)
		if err != nil {
			return err
		}
		header := "# Заготовка макета обработки из 1С. Области доступны из DSL:\n" +
			"# Макет.Область(\"<имя макета 1С>\"). TODO: перенесите оформление вручную.\n"
		name := fileName(owner) + ".proc.layout.yaml"
		if err := os.WriteFile(filepath.Join(srcDir, name), append([]byte(header), data...), 0o644); err != nil {
			return err
		}
		notes.ProcessorLayouts = append(notes.ProcessorLayouts, name)
	}
	return nil
}
```

- [ ] **Step 4: Прогнать тесты**

Run: `go test ./internal/converter/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/converter/writer
git commit -m "feat(converter): макеты обработок импортируются как layout-заготовки у .proc.os (#48)"
```

### Task 8: Финал PR 1

- [ ] **Step 1: Полный прогон**

Run: `go test ./...` → PASS; `go build -o onebase.exe ./cmd/onebase` → OK
(если onebase.exe залочен запущенным сервером — `taskkill /IM onebase.exe /F`);
`.\onebase.exe check --project examples/trade` → без ошибок.

- [ ] **Step 2: Push и PR**

```
git push -u origin fix/48-converter-import
gh pr create --title "fix(converter): импорт 1С — плоские xml, #Область, enum, формы и макеты обработок (#48)" --body "Closes #48. <краткий список из спеки Plans/63-issues-48-49-fixes.md, PR 1>"
```

---

## PR 2 — ветка `fix/49-i18n-messages` (создать от main)

```
git checkout main
git checkout -b fix/49-i18n-messages
```

### Task 9: Лаунчер уважает Accept-Language

**Files:**
- Modify: `internal/launcher/handlers.go:636-641`
- Test: создать `internal/launcher/lang_test.go`

- [ ] **Step 1: Написать падающий тест**

```go
package launcher

import (
	"net/http/httptest"
	"testing"

	"github.com/ivantit66/onebase/internal/i18n"
)

// resolveLang передавал baseLang="ru" в i18n.Resolve, и Accept-Language
// никогда не учитывался — лаунчер всегда был русским (issue #49 п.1).
func TestResolveLangHonorsAcceptLanguage(t *testing.T) {
	saved := launcherBundle
	defer func() { launcherBundle = saved }()
	b, err := i18n.Load(i18n.EmbeddedLocales, "")
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	launcherBundle = b

	cases := []struct{ accept, want string }{
		{"de-DE,de;q=0.9,en;q=0.8", "de"},
		{"en-US,en;q=0.9", "en"},
		{"", "ru"},
		{"tlh", "ru"}, // нет словаря — фолбэк
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept-Language", c.accept)
		}
		if got := resolveLang(r); got != c.want {
			t.Errorf("Accept-Language=%q: got %q, want %q", c.accept, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/launcher/ -run TestResolveLangHonorsAcceptLanguage -v`
Expected: FAIL — для "de-DE…" получено "ru".

- [ ] **Step 3: Реализовать**

В `handlers.go:638` заменить:

```go
		return i18n.Resolve("", "ru", r.Header.Get("Accept-Language"), launcherBundle)
```

на:

```go
		// baseLang пустой: иначе Resolve возвращает его сразу и до
		// Accept-Language не доходит (issue #49 п.1); фолбэк "ru" встроен
		// в Resolve.
		return i18n.Resolve("", "", r.Header.Get("Accept-Language"), launcherBundle)
```

- [ ] **Step 4: Прогнать тесты**

Run: `go test ./internal/launcher/ ./internal/i18n/` → PASS.

- [ ] **Step 5: Commit**

```
git add internal/launcher
git commit -m "fix(launcher): Accept-Language учитывается при выборе языка (#49)"
```

### Task 10: `<html lang>` из данных шаблона

**Files:**
- Modify: `internal/launcher/templates.go:17`, `internal/launcher/configurator_tmpl.go:413`, `internal/launcher/forms_tmpl.go:55`, `internal/launcher/cfgauth.go:58`

- [ ] **Step 1: Заменить атрибуты**

- `templates.go:17`: `<html lang="ru">` → `<html lang="{{.Lang}}">` (render()
  всегда кладёт `Lang` в data — handlers.go:651).
- `configurator_tmpl.go:413`: → `<html lang="{{.Lang}}">` (`configuratorData.Lang`).
- `forms_tmpl.go:55` и `cfgauth.go:58`: проверить, есть ли в данных шаблона язык;
  если нет (cfgLoginData — map без Lang) — добавить `"Lang": resolveLang(r)` в
  `cfgLoginData` (cfgauth.go:94) и аналогично в данные forms_tmpl, затем заменить
  атрибут на `{{.Lang}}`. Если протащить язык некуда (шаблон рендерится без
  request) — оставить `lang="ru"` и зафиксировать это комментарием.

- [ ] **Step 2: Прогон тестов и ручная проверка**

Run: `go test ./internal/launcher/` → PASS (рендер-тесты configurator
используют Lang: "ru" — не ломаются).

- [ ] **Step 3: Commit**

```
git add internal/launcher
git commit -m "fix(launcher): атрибут html lang берётся из языка запроса (#49)"
```

### Task 11: Пакет `i18nerr`

**Files:**
- Create: `internal/i18n/i18nerr/i18nerr.go`
- Test: `internal/i18n/i18nerr/i18nerr_test.go`

- [ ] **Step 1: Написать падающие тесты**

```go
package i18nerr

import (
	"errors"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/ivantit66/onebase/internal/i18n"
)

func testBundle(t *testing.T) *i18n.Bundle {
	t.Helper()
	fsys := fstest.MapFS{
		"en.json": &fstest.MapFile{Data: []byte(`{
			"неизвестная таблица %s": "unknown table %s",
			"Деление на ноль": "Division by zero",
			"сохранение документа": "saving document"
		}`)},
	}
	b, err := i18n.Load(fsys, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return b
}

func TestErrorRendersRussian(t *testing.T) {
	err := Errorf("неизвестная таблица %s", "товары")
	if err.Error() != "неизвестная таблица товары" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestLocalizeTemplate(t *testing.T) {
	b := testBundle(t)
	err := Errorf("неизвестная таблица %s", "товары")
	if got := Localize(b, "en", err); got != "unknown table товары" {
		t.Fatalf("Localize = %q", got)
	}
	// ru и неизвестный язык — русский текст без изменений.
	if got := Localize(b, "ru", err); got != "неизвестная таблица товары" {
		t.Fatalf("Localize ru = %q", got)
	}
	if got := Localize(b, "zz", err); got != "неизвестная таблица товары" {
		t.Fatalf("Localize zz = %q", got)
	}
}

func TestLocalizeExactMatchForPlainErrors(t *testing.T) {
	b := testBundle(t)
	err := errors.New("Деление на ноль")
	if got := Localize(b, "en", err); got != "Division by zero" {
		t.Fatalf("Localize = %q", got)
	}
	// Непереводимое — как есть.
	err2 := errors.New("что-то совсем другое")
	if got := Localize(b, "en", err2); got != "что-то совсем другое" {
		t.Fatalf("Localize = %q", got)
	}
}

func TestLocalizeWrappedChain(t *testing.T) {
	b := testBundle(t)
	inner := Errorf("неизвестная таблица %s", "товары")
	outer := Wrapf(inner, "сохранение документа")
	if outer.Error() != "сохранение документа: неизвестная таблица товары" {
		t.Fatalf("Error() = %q", outer.Error())
	}
	if got := Localize(b, "en", outer); got != "saving document: unknown table товары" {
		t.Fatalf("Localize = %q", got)
	}
	// Обёртка через fmt.Errorf %w: i18nerr-звено переводится, префикс остаётся.
	wrapped := fmt.Errorf("контекст: %w", inner)
	if got := Localize(b, "en", wrapped); got != "контекст: unknown table товары" {
		t.Fatalf("Localize = %q", got)
	}
	// errors.Is работает сквозь цепочку.
	if !errors.Is(outer, inner) {
		t.Fatalf("errors.Is не видит wrapped")
	}
}
```

- [ ] **Step 2: Убедиться, что тесты падают**

Run: `go test ./internal/i18n/i18nerr/ -v`
Expected: FAIL (пакета нет — ошибка компиляции).

- [ ] **Step 3: Реализовать пакет**

`internal/i18n/i18nerr/i18nerr.go`:

```go
// Package i18nerr — ошибки платформы с локализуемым сообщением.
//
// Ключ — русский fmt-шаблон («неизвестная таблица %s»), как и все ключи
// i18n OneBase. Error() всегда рендерит по-русски (логи, CLI не меняются),
// Localize переводит сообщение на HTTP-границе: i18nerr-звенья цепочки —
// по шаблону с подстановкой аргументов, прочие ошибки — exact-match-ом
// полного текста; всё, что перевести нечем, остаётся русским.
package i18nerr

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/i18n"
)

// Error — ошибка с шаблоном-ключом и аргументами.
type Error struct {
	Key     string
	Args    []any
	wrapped error
}

// New создаёт ошибку со статическим ключом.
func New(key string) error { return &Error{Key: key} }

// Errorf создаёт ошибку с fmt-шаблоном и аргументами.
func Errorf(key string, args ...any) error { return &Error{Key: key, Args: args} }

// Wrapf оборачивает err локализуемым префиксом: «<key с args>: <err>».
func Wrapf(err error, key string, args ...any) error {
	return &Error{Key: key, Args: args, wrapped: err}
}

func (e *Error) Error() string {
	msg := e.render()
	if e.wrapped != nil {
		return msg + ": " + e.wrapped.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.wrapped }

// render — русское сообщение без wrapped-части.
func (e *Error) render() string {
	if len(e.Args) == 0 {
		return e.Key
	}
	return fmt.Sprintf(e.Key, e.Args...)
}

// localize — перевод шаблона и подстановка аргументов.
func (e *Error) localize(b *i18n.Bundle, lang string) string {
	tpl := b.T(lang, e.Key)
	if len(e.Args) == 0 {
		return tpl
	}
	return fmt.Sprintf(tpl, e.Args...)
}

// Localize переводит сообщение об ошибке для языка lang.
func Localize(b *i18n.Bundle, lang string, err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if b == nil || lang == "" || lang == "ru" {
		return msg
	}
	// Статическое сообщение целиком (включая ошибки без i18nerr).
	if t := b.T(lang, msg); t != msg {
		return t
	}
	// Перевести i18nerr-звенья в цепочке, сохранив остальной текст.
	for c := err; c != nil; c = errors.Unwrap(c) {
		if e, ok := c.(*Error); ok {
			ru := e.render()
			if loc := e.localize(b, lang); loc != ru {
				msg = strings.Replace(msg, ru, loc, 1)
			}
		}
	}
	return msg
}
```

- [ ] **Step 4: Прогнать тесты**

Run: `go test ./internal/i18n/... -v` → PASS.

- [ ] **Step 5: Commit**

```
git add internal/i18n/i18nerr
git commit -m "feat(i18n): пакет i18nerr — локализуемые ошибки платформы (#49)"
```

### Task 12: Расширение i18ncheck на Go-ключи

**Files:**
- Modify: `tools/i18ncheck/main.go:22,108-138`

- [ ] **Step 1: Реализовать**

Заменить одиночный `keyPattern` на список (строка 22):

```go
// Шаблонные ключи {{t $.Lang "..."}} и Go-ключи: i18nerr.New/Errorf/Wrapf и
// tr(lang, "...") / s.tr(lang, "...").
var keyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\{\{t\s+\$[.\w]*\s+"((?:[^"\\]|\\.)*)"\s*\}\}`),
	regexp.MustCompile(`i18nerr\.(?:New|Errorf)\(\s*"((?:[^"\\]|\\.)*)"`),
	regexp.MustCompile(`i18nerr\.Wrapf\([^,]+,\s*"((?:[^"\\]|\\.)*)"`),
	regexp.MustCompile(`\btr\(\s*\w+,\s*"((?:[^"\\]|\\.)*)"\s*\)`),
}
```

В `collectKeys` цикл по паттернам:

```go
			for _, p := range keyPatterns {
				for _, m := range p.FindAllSubmatch(data, -1) {
					seen[string(m[1])] = struct{}{}
				}
			}
```

В `main` расширить охват: `collectKeys(root, []string{"internal/ui", "internal/launcher", "internal/storage", "internal/dsl", "internal/query", "internal/entityservice"})`.

- [ ] **Step 2: Прогнать и добить словари**

Run: `go run ./tools/i18ncheck`
Паттерн `tr(...)` вскроет существующие Go-ключи (s.tr в internal/ui), часть
которых может отсутствовать во всех локалях → FAIL со списком. Каждый ключ из
списка добавить в `internal/i18n/locales/en.json` и `de.json` с переводом.
Повторять до «OK». Go-строки с `\"`-эскейпами в JSON-ключах пишутся как есть
(без бэкслеша) — сверяться с выводом i18ncheck (он печатает ключ в %q).

- [ ] **Step 3: Commit**

```
git add tools/i18ncheck internal/i18n/locales
git commit -m "feat(i18ncheck): проверка Go-ключей i18nerr и tr() (#49)"
```

### Task 13: Граница UI — `errText` + прогон internal/ui

**Files:**
- Modify: `internal/ui/server.go` (рядом с resolveLang:650), все handlers `internal/ui/*.go` с `http.Error` (16 файлов, ~95 мест — список даёт grep)
- Test: `internal/ui/` — существующие тесты должны остаться зелёными

- [ ] **Step 1: Хелпер**

В `server.go` после `resolveLang`:

```go
// errText локализует сообщение об ошибке для языка текущего запроса.
func (s *Server) errText(r *http.Request, err error) string {
	return i18nerr.Localize(s.cfg.Bundle, s.resolveLang(r), err)
}
```

(импорт `github.com/ivantit66/onebase/internal/i18n/i18nerr`).

- [ ] **Step 2: Механический прогон http.Error**

Найти все места: `rg -n "http\.Error" internal/ui --type go`.
Правила преобразования (применять только в методах `*Server`, где есть `r *http.Request`):

1. Сообщение из ошибки:
   `http.Error(w, err.Error(), 500)` → `http.Error(w, s.errText(r, err), 500)`.
2. Русская статика с конкатенацией:
   `http.Error(w, "Сущность не найдена: "+name, http.StatusNotFound)` →
   `http.Error(w, s.tr(s.resolveLang(r), "Сущность не найдена")+": "+name, http.StatusNotFound)`.
3. Русская статика без параметров:
   `http.Error(w, "Доступ запрещён", 403)` → `http.Error(w, s.tr(s.resolveLang(r), "Доступ запрещён"), 403)`.
4. Англоязычную статику ("invalid id" и т.п.) — не трогать.

Там, где в одной функции несколько замен, вычислить `lang := s.resolveLang(r)`
один раз. Свободные функции без `*Server` — пропустить и пометить комментарием
`// TODO(#49): нет доступа к bundle` только если такие найдутся.

- [ ] **Step 3: Русские сообщения internal/ui вне http.Error**

`rg -n '(Errorf|errors\.New|Sprintf)\("[^"]*[а-яА-ЯёЁ]' internal/ui --type go -g '!*_test.go'`
(~40 мест). Ошибки, уходящие в HTTP-ответ/JSON — перевести на
`i18nerr.Errorf`/`i18nerr.New` (импорт добавить):
`fmt.Errorf("проведение документа %s: %w", name, err)` →
`i18nerr.Wrapf(err, "проведение документа %s", name)`.
Строки, идущие только в логи, — не трогать.

- [ ] **Step 4: Тесты + словари**

Run: `go test ./internal/ui/...` → PASS (русский рендер не изменился).
Run: `go run ./tools/i18ncheck` → FAIL со списком новых ключей → добавить все
в `en.json` и `de.json` → повторить до «OK».

- [ ] **Step 5: Commit**

```
git add internal/ui internal/i18n/locales
git commit -m "feat(ui): локализация сообщений об ошибках через i18nerr (#49)"
```

### Task 14: Прогон internal/storage, internal/dsl, internal/query

**Files:**
- Modify: `internal/storage/*.go` (~31 место), `internal/dsl/**/*.go` (~17), `internal/query/*.go` (~2)

- [ ] **Step 1: Инвентаризация**

`rg -n '(Errorf|errors\.New)\("[^"]*[а-яА-ЯёЁ]' internal/storage internal/dsl internal/query --type go -g '!*_test.go'`

- [ ] **Step 2: Преобразование**

Те же правила, что в Task 13 Step 3:
- `fmt.Errorf("неизвестная таблица %s", x)` → `i18nerr.Errorf("неизвестная таблица %s", x)`;
- `fmt.Errorf("создание таблицы: %w", err)` → `i18nerr.Wrapf(err, "создание таблицы")`;
- `errors.New("…")` → `i18nerr.New("…")`.

Исключения (НЕ конвертировать, зафиксированы в спеке):
- `panic(userError{Msg: …})` в интерпретаторе: статические тексты
  («Деление на ноль», «Вычислить: ожидается строка-выражение») просто получают
  записи в словарях en/de — exact-match в Localize их переведёт; параметризованные
  конкатенации («Новый: неизвестный тип » + X) — ошибки разработчика конфигурации,
  остаются русскими;
- сообщения, которые видны только в CLI (`onebase check` и пр.).

- [ ] **Step 3: Тесты + словари**

Run: `go test ./internal/storage/... ./internal/dsl/... ./internal/query/...` → PASS.
Если тест сравнивает текст ошибки — текст НЕ изменился (render русский), но при
падении сверить и поправить тест только если он сравнивает тип ошибки.
Run: `go run ./tools/i18ncheck` → добавить новые ключи в en/de → «OK».

- [ ] **Step 4: Commit**

```
git add internal/storage internal/dsl internal/query internal/i18n/locales
git commit -m "feat(engine): локализуемые ошибки storage/dsl/query через i18nerr (#49)"
```

### Task 15: Прогон internal/launcher + страница входа конфигуратора

**Files:**
- Modify: `internal/launcher/*.go` (~24 места), `internal/launcher/cfgauth.go:57-90`

- [ ] **Step 1: Хелпер и http.Error**

В `handlers.go` рядом с `tr`:

```go
// errText локализует сообщение об ошибке для языка текущего запроса.
func errText(r *http.Request, err error) string {
	return i18nerr.Localize(launcherBundle, resolveLang(r), err)
}
```

Прогон `rg -n "http\.Error" internal/launcher --type go` по правилам Task 13
(вместо `s.errText`/`s.tr` — свободные `errText`/`tr(resolveLang(r), …)`).
Русские `fmt.Errorf` → `i18nerr` по правилам Task 14.

- [ ] **Step 2: Страница входа конфигуратора**

`cfgLoginTmpl` (cfgauth.go:57) — захардкоженный русский без t-вызовов:
- объявить с funcs: `template.Must(template.New("cfg-login").Funcs(template.FuncMap{"t": tr}).Parse(…))`;
- русские строки шаблона («Конфигуратор — Вход», «Только для администраторов»,
  «Имя пользователя», «Пароль», «Войти», «← Назад к списку баз») обернуть в
  `{{t $.Lang "…"}}`;
- в `cfgLoginData` (cfgauth.go:94) добавить `"Lang": resolveLang(r)`;
- `<html lang="ru">` → `<html lang="{{.Lang}}">` (если не сделано в Task 10).

- [ ] **Step 3: Тесты + словари**

Run: `go test ./internal/launcher/...` → PASS.
Run: `go run ./tools/i18ncheck` → добавить ключи в en/de → «OK».

- [ ] **Step 4: Commit**

```
git add internal/launcher internal/i18n/locales
git commit -m "feat(launcher): локализация системных сообщений и страницы входа (#49)"
```

### Task 16: Финал PR 2

- [ ] **Step 1: Полный прогон**

Run: `go test ./...` → PASS; `go run ./tools/i18ncheck` → OK;
`go build -o onebase.exe ./cmd/onebase` → OK;
ручная проверка: `./onebase.exe` (лаунчер), в браузере DevTools переключить
язык запроса (или curl с `Accept-Language: de`) — страница списка баз
по-немецки.

- [ ] **Step 2: Push и PR**

```
git push -u origin fix/49-i18n-messages
gh pr create --title "feat(i18n): Accept-Language в лаунчере и локализация системных сообщений (#49)" --body "Closes #49 (п.2 — кроме 14 локалей-фолбэков, см. Plans/63-issues-48-49-fixes.md)"
```

### Task 17: Ответы в ишью (после мержа обоих PR)

- [ ] **Step 1:** `gh issue comment 48` — перечислить пять исправлений с номерами
коммитов; отметить, что конвертация содержимого .mxl остаётся ручной (заготовки
+ исходники рядом).
- [ ] **Step 2:** `gh issue comment 49` — лаунчер берёт язык из Accept-Language
(16 локалей); системные сообщения локализованы через i18nerr (en/de полностью,
остальные локали — фолбэк на русский, добивка отдельной задачей); сообщения CLI
и прикладных конфигураций сознательно остаются русскими.
