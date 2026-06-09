# План 49: Вкладки в редакторе объектов конфигуратора

**Дата:** 2026-06-09
**Статус:** ✅ Реализовано
**Issue:** [#35 — Вкладки в конфигураторе](https://github.com/ivanarama/onebase/issues/35)

## Проблема

При редактировании объекта метаданных в веб-конфигураторе (лаунчер) свойства
показываются одним длинным вертикальным столбцом секций. Чтобы добраться до
нужного раздела (например, до форм или печатных форм у документа), приходится
долго скроллить.

Редактор справочника/документа (`entity-detail`) рендерит подряд:
**Модули → Свойства → Ввод на основании → Реквизиты → Табличные части →
Предопределённые → Печатные формы → Формы → Управляемая форма**.

## Цель

Разнести секции редактора по вкладкам, чтобы убрать вертикальный скролл и
приблизить UX к 1С:Конфигуратору. Охват — все объекты с многосекционным
редактором: **справочники, документы, отчёты, обработки**.

**Чего НЕ делаем:** регистры (накопления/сведения/бухгалтерии) — только
Измерения+Ресурсы, влезают без скролла; перечисление/константа/общий модуль —
одна секция. Серверных изменений (роуты, хендлеры, модель данных) нет.

## Затронутые файлы

- `internal/launcher/configurator_tmpl.go` — шаблоны (`cfgCSS`, `entity-detail`,
  панели отчёта и обработки внутри `tab-tree`) и JS (`cfgFoot`).
- `internal/launcher/*_test.go` — smoke-тест парсинга шаблона (см. «Проверка»).

## Решение

### Механизм (клиентские вкладки)

Переиспользуем существующий в кодовой базе паттерн вкладок (`modTab` —
Модуль объекта/проведения/менеджера; `formTab` — Форма списка/элемента):
чисто клиентское переключение `active`-класса, без серверного round-trip.

**CSS** (добавить в `cfgCSS`):

- `.obj-tabs` — панель вкладок объекта (стиль крупнее под-вкладок `.module-tab`,
  ближе к страничным `.tab`).
- `.obj-tab` / `.obj-tab.active` — вкладка.
- `.obj-pane{display:none}` / `.obj-pane.active{display:block}` — панель контента.

**JS** (добавить в `cfgFoot`), по образцу `modTab`:

```js
function cfgObjTab(el, paneId){
  var box = el.closest('.obj-editor');
  box.querySelectorAll('.obj-tab').forEach(function(t){t.classList.remove('active')});
  box.querySelectorAll('.obj-pane').forEach(function(p){p.classList.remove('active')});
  el.classList.add('active');
  document.getElementById(paneId).classList.add('active');
}
```

Скоуп через `.obj-editor`: вложенные `modTab`/`formTab` (другие классы, свой
скоуп через `.module-editor-wrap` и `el.parentNode`) продолжают работать —
класс `.obj-tab` отличается от `.module-tab`, коллизий нет.

**Структура каждого редактора:**

```
panel-title / panel-kind            ← остаётся над вкладками
<div class="obj-editor">
  <div class="obj-tabs">
    <div class="obj-tab active" onclick="cfgObjTab(this,'ot-data-<Name>')">Данные</div>
    <div class="obj-tab"        onclick="cfgObjTab(this,'ot-forms-<Name>')">Формы</div>
    …
  </div>
  <div class="obj-pane active" id="ot-data-<Name>"> …секции… </div>
  <div class="obj-pane"        id="ot-forms-<Name>"> …секции… </div>
  …
</div>
```

ID панелей суффиксуются именем объекта (`ot-data-Номенклатура`): все объекты
присутствуют в DOM одновременно (виден только один `.cfg-panel`), ID должны быть
уникальны на странице.

### Раскладка вкладок по типам объектов

**Справочник / Документ** (`entity-detail`) — 4 вкладки:

| Вкладка | Секции |
|---|---|
| **Данные** | Свойства (Проводится/Иерархический), Реквизиты, Табличные части, Ввод на основании, Предопределённые элементы |
| **Формы** | Авто-формы (список/элемент) + Управляемая форма |
| **Печатные формы** | Привязанные печатные формы (+ пустое состояние) |
| **Модули** | Модуль объекта / ОбработкаПроведения / Менеджер (существующий блок с под-вкладками `modTab`) |

**Отчёт** — 3 вкладки: **Параметры** (Заголовок + таблица параметров) / **Запрос** /
**Диаграмма** (Процедура диаграммы + Код диаграммы).

**Обработка** — 3 вкладки: **Параметры** (Заголовок + параметры) / **Код**
(Исходный код / Процедура Выполнить) / **Форма** (управляемая форма).

### Формы внутри вкладок

- У отчёта/обработки редактор — **одна** `<form>`. Панели `div.obj-pane` кладутся
  внутрь формы (внутри `.obj-editor`), кнопка «Сохранить» остаётся **под
  вкладками, всегда видимой** (вне `.obj-pane`). HTML допускает любые `div`
  внутри `form`.
- У справочника/документа форм несколько (поля, предопределённые, формы,
  модули). Каждая со своей кнопкой «Сохранить» внутри своей панели — формы НЕ
  вкладываются друг в друга, `.obj-pane` это просто `div`.

## Поведение

- **Активная по умолчанию** — первая вкладка (Данные / Параметры).
- **Сохранение** идёт через AJAX-тост (`cfgAjaxSubmit`) без перезагрузки →
  активная вкладка **не сбрасывается**. Маркеры `✓ Сохранено`
  (`FieldsSaved`/`ModuleSaved`) остаются как fallback для не-AJAX пути.
- **Ctrl+S / кнопка «Сохранить» в шапке** (`cfgSaveActive`): сейчас берёт первую
  AJAX-форму в `.cfg-panel`. Изменить — предпочитать первую AJAX-форму внутри
  активной `.obj-pane`; если в активной вкладке формы нет — поведение как раньше.
  Чтобы Ctrl+S на вкладке «Формы» сохранял формы, а не реквизиты.
- **Пустая вкладка «Печатные формы»**: вкладка присутствует всегда; при
  отсутствии привязанных печатных форм — текстовая подсказка + кнопка создания
  (существующий `cfgNewObj('printform')`).
- **i18n**: новые подписи вкладок (Данные, Формы, Печатные формы, Модули,
  Параметры, Запрос, Диаграмма, Код, Форма) оборачиваются в `{{t $.Lang "…"}}`.
  Pre-commit `i18ncheck` только предупреждает.

## Проверка

Изменения — чистый HTML-шаблон + CSS + JS; Go-логики нет.

1. **Сборка** лаунчера без ошибок.
2. **Парсинг шаблона**: `go test ./internal/launcher/`. Если уже есть тест,
   рендерящий конфигуратор — он поймает синтаксис; если нет — добавить
   минимальный тест, что `cfgTmpl` парсится и рендерит панель без ошибки.
3. **Ручная проверка** (лаунчер → база → Конфигуратор):
   - открыть справочник, документ, отчёт, обработку — вкладки переключаются,
     секции под правильными вкладками;
   - AJAX-сохранение работает (тост), активная вкладка сохраняется;
   - вложенные под-вкладки Модулей (`modTab`) и Форм (`formTab`) работают;
   - Ctrl+S сохраняет форму активной вкладки;
   - регистры/перечисления/константы — без изменений.

## Критерии готовности

- Редактор справочника/документа/отчёта/обработки показывает вкладки вместо
  длинного столбца; скролл устранён.
- Все существующие действия (правка полей, сохранение, проверка кода,
  переключение под-вкладок) работают как раньше.
- Сборка и `go test ./internal/launcher/` зелёные.

---

# План реализации (бите-сайз задачи)

> **Для агентов-исполнителей:** обязательный sub-skill —
> `superpowers:subagent-driven-development` (рекомендуется) или
> `superpowers:executing-plans`. Шаги помечены чекбоксами (`- [ ]`).

**Цель:** редактор объектов конфигуратора (справочник/документ/отчёт/обработка)
показывает секции вкладками вместо длинного вертикального столбца.

**Архитектура:** клиентские вкладки по образцу `modTab`/`formTab` — CSS-классы
`.obj-tabs`/`.obj-tab`/`.obj-pane` + JS `cfgObjTab(el, paneId)`, скоуп через
`.obj-editor`. Только правки шаблона/CSS/JS в
`internal/launcher/configurator_tmpl.go`; серверной логики нет.

**Стек:** Go `html/template`, ванильный JS, CSS (всё инлайн в `configurator_tmpl.go`).

---

### Задача 1: CSS + JS-фундамент (`.obj-tabs` / `cfgObjTab`)

Аддитивно: классы и функция, которые пока никто не использует.

**Файлы:**
- Modify: `internal/launcher/configurator_tmpl.go` — `cfgCSS` (около строки 155,
  рядом с `.module-tabs`); `cfgFoot` (около `modTab`, строка ~1561).

- [ ] **Шаг 1. Добавить CSS в `cfgCSS`** (после блока `.module-tab.active{…}`):

```css
/* ── Object editor tabs (issue #35) ─────────────────── */
.obj-editor{margin-top:4px}
.obj-tabs{display:flex;gap:2px;border-bottom:2px solid #d0d7e3;margin:10px 0 14px}
.obj-tab{padding:7px 16px;cursor:pointer;font-size:13px;color:#666;border:1px solid transparent;border-bottom:none;border-radius:6px 6px 0 0;margin-bottom:-2px;user-select:none}
.obj-tab:hover{color:#1a4a80;background:#f5f8ff}
.obj-tab.active{color:#1a4a80;border-color:#d0d7e3;border-bottom:2px solid #fff;background:#fff;font-weight:600}
.obj-pane{display:none}
.obj-pane.active{display:block}
```

- [ ] **Шаг 2. Добавить JS в `cfgFoot`** (рядом с функцией `modTab`):

```js
// Вкладки редактора объекта (issue #35). Скоуп — ближайший .obj-editor,
// чтобы не конфликтовать с вложенными modTab/formTab.
function cfgObjTab(el, paneId){
  var box = el.closest('.obj-editor');
  box.querySelectorAll('.obj-tab').forEach(function(t){t.classList.remove('active')});
  box.querySelectorAll('.obj-pane').forEach(function(p){p.classList.remove('active')});
  el.classList.add('active');
  document.getElementById(paneId).classList.add('active');
}
```

- [ ] **Шаг 3. Сборка и существующие тесты не сломаны:**

Run: `go build ./internal/launcher/ && go test ./internal/launcher/`
Expected: build OK, тесты PASS (новый код пока не задействован).

- [ ] **Шаг 4. Коммит:**

```bash
git add internal/launcher/configurator_tmpl.go
git commit -m "feat(configurator): CSS+JS фундамент вкладок редактора объектов (#35)"
```

---

### Задача 2: Справочник/документ → 4 вкладки (`entity-detail`)

**Файлы:**
- Modify: `internal/launcher/configurator_tmpl.go` — шаблон `entity-detail`
  (строки ~4458–4857).
- Test: `internal/launcher/configurator_tabs_render_test.go` (создать).

- [ ] **Шаг 1. Написать падающий рендер-тест.** Создать файл
  `internal/launcher/configurator_tabs_render_test.go`:

```go
package launcher

import (
	"bytes"
	"strings"
	"testing"
)

// Issue #35: редактор объектов конфигуратора рендерит секции вкладками,
// а не длинным столбцом. Фиксируем наличие панели вкладок и панелей контента.
func renderTabTree(t *testing.T) string {
	t.Helper()
	data := &configuratorData{
		Base: &Base{ID: "test-base", Name: "Тест", ConfigSource: "file"},
		Lang: "ru",
		Tab:  "tree",
		Catalogs: []cfgEntity{{
			Name: "Номенклатура", Kind: "Справочник",
			Fields: []cfgField{{Name: "Цена", Type: "number"}},
		}},
		Docs: []cfgEntity{{
			Name: "Реализация", Kind: "Документ", Posting: true,
			Fields: []cfgField{{Name: "Сумма", Type: "number"}},
		}},
		Reports:    []cfgReport{{Name: "Продажи"}},
		Processors: []cfgProcessor{{Name: "Загрузка"}},
	}
	var buf bytes.Buffer
	if err := cfgTmpl.ExecuteTemplate(&buf, "tab-tree", data); err != nil {
		t.Fatalf("ExecuteTemplate tab-tree: %v", err)
	}
	return buf.String()
}

func TestConfigurator_EntityTabs(t *testing.T) {
	html := renderTabTree(t)
	for _, sub := range []string{
		`class="obj-tabs"`,
		`id="ot-data-Номенклатура"`,
		`id="ot-forms-Номенклатура"`,
		`id="ot-print-Номенклатура"`,
		`id="ot-modules-Номенклатура"`,
		`id="ot-data-Реализация"`,
		`id="ot-modules-Реализация"`,
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML нет ожидаемого фрагмента: %q", sub)
		}
	}
}
```

- [ ] **Шаг 2. Запустить — убедиться, что падает:**

Run: `go test ./internal/launcher/ -run TestConfigurator_EntityTabs -v`
Expected: FAIL — фрагментов `obj-tabs`/`ot-data-…` нет (вкладки ещё не сделаны).
(Если вместо FAIL — паника на nil-поле data, дополнить `data` минимальным
значением проблемного поля и повторить; рендер должен проходить на zero-values.)

- [ ] **Шаг 3. Перестроить `entity-detail` в 4 вкладки.** В шаблоне
  `entity-detail` (между `{{$fSavedEnt := .FieldsSavedEntity}}` и финальным
  блоком управляемой формы) обернуть содержимое в `.obj-editor` с 4 панелями.
  Существующие блоки переносятся **без изменения их внутренней разметки**,
  меняется только порядок/обёртка. Целевая структура:

```html
<div class="obj-editor">
  <div class="obj-tabs">
    <div class="obj-tab active" onclick="cfgObjTab(this,'ot-data-{{$e.Name}}')">{{t $.Lang "Данные"}}</div>
    <div class="obj-tab" onclick="cfgObjTab(this,'ot-forms-{{$e.Name}}')">{{t $.Lang "Формы"}}</div>
    <div class="obj-tab" onclick="cfgObjTab(this,'ot-print-{{$e.Name}}')">{{t $.Lang "Печатные формы"}}</div>
    <div class="obj-tab" onclick="cfgObjTab(this,'ot-modules-{{$e.Name}}')">{{t $.Lang "Модули"}}</div>
  </div>

  <div class="obj-pane active" id="ot-data-{{$e.Name}}">
    {{/* Перенести БЕЗ изменений: форму полей (текущие ~4545–4697):
         Свойства (Проводится/Иерархический), «Ввод на основании»,
         «Реквизиты», табличные части, кнопку «Сохранить типы полей».
         Затем блок «Предопределённые элементы» (текущие ~4700–4722). */}}
  </div>

  <div class="obj-pane" id="ot-forms-{{$e.Name}}">
    {{/* Перенести блок «Формы» (текущие ~4738–4800) БЕЗ верхнего
         <div class="section-hd">Формы</div> — заголовком теперь служит вкладка.
         Под-вкладки formTab (Форма списка/элемента) оставить как есть.
         Затем блок «Управляемая форма» (текущие ~4803–4856) БЕЗ его
         верхнего section-hd «◇ Управляемая форма». */}}
  </div>

  <div class="obj-pane" id="ot-print-{{$e.Name}}">
    {{if $e.LinkedPrintForms}}
    <div style="display:flex;flex-wrap:wrap;gap:8px;margin-bottom:8px">
      {{range $e.LinkedPrintForms}}
      <a href="#" onclick="cfgSelectPanel('pf-{{.Name}}');return false"
         style="display:inline-flex;align-items:center;gap:5px;padding:5px 12px;background:#f0f4ff;border:1px solid #c8d4f0;border-radius:4px;font-size:12px;color:#1a4a80;text-decoration:none">
        🖨 {{.Name}}
      </a>
      {{end}}
    </div>
    {{else}}
    <div style="color:#94a3b8;font-size:12px;padding:8px 0">
      {{t $.Lang "Печатных форм нет."}}
      <a href="#" onclick="cfgNewObj('printform');return false" style="color:#1a4a80">{{t $.Lang "Создать печатную форму"}}</a>
    </div>
    {{end}}
  </div>

  <div class="obj-pane" id="ot-modules-{{$e.Name}}">
    {{/* Перенести блок «Модули» (текущие ~4467–4543), но УБРАТЬ внешний
         <details open><summary class="section-hd">Модули</summary> … </details>
         (заголовком служит вкладка); оставить внутренний
         <div class="module-editor-wrap"> с под-вкладками modTab. */}}
  </div>
</div>
```

  Удаляемые при переносе обёртки-заголовки: внешний `<details>` «Модули»,
  `<div class="section-hd">…"Печатные формы"…</div>`,
  `<div class="section-hd">…"Формы"…</div>`,
  `<div class="section-hd">◇ Управляемая форма</div>` — их роль берут вкладки.

- [ ] **Шаг 4. Запустить тест — проходит, сборка ОК:**

Run: `go test ./internal/launcher/ -run TestConfigurator_EntityTabs -v && go build ./internal/launcher/`
Expected: PASS, build OK.

- [ ] **Шаг 5. Коммит:**

```bash
git add internal/launcher/configurator_tmpl.go internal/launcher/configurator_tabs_render_test.go
git commit -m "feat(configurator): вкладки Данные/Формы/Печатные/Модули у справочника и документа (#35)"
```

---

### Задача 3: Отчёт → 3 вкладки (Параметры / Запрос / Диаграмма)

**Файлы:**
- Modify: `internal/launcher/configurator_tmpl.go` — панель отчёта в `tab-tree`
  (`{{range .Reports}}`, строки ~3920–3983).
- Test: `internal/launcher/configurator_tabs_render_test.go` (дополнить).

- [ ] **Шаг 1. Добавить падающий тест** в конец
  `configurator_tabs_render_test.go`:

```go
func TestConfigurator_ReportTabs(t *testing.T) {
	html := renderTabTree(t)
	for _, sub := range []string{
		`id="ot-params-Продажи"`,
		`id="ot-query-Продажи"`,
		`id="ot-chart-Продажи"`,
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML нет ожидаемого фрагмента отчёта: %q", sub)
		}
	}
}
```

- [ ] **Шаг 2. Запустить — падает:**

Run: `go test ./internal/launcher/ -run TestConfigurator_ReportTabs -v`
Expected: FAIL.

- [ ] **Шаг 3. Обернуть секции отчёта во вкладки.** Внутри `<form …/report>`
  (после скрытого `report_name` и поля «Заголовок») вставить `.obj-editor` с
  тремя панелями; кнопку «Сохранить» (`module-save-row`) оставить **после**
  `.obj-editor`, но внутри формы (видна всегда):

```html
<div class="obj-editor">
  <div class="obj-tabs">
    <div class="obj-tab active" onclick="cfgObjTab(this,'ot-params-{{$rn}}')">{{t $.Lang "Параметры"}}</div>
    <div class="obj-tab" onclick="cfgObjTab(this,'ot-query-{{$rn}}')">{{t $.Lang "Запрос"}}</div>
    <div class="obj-tab" onclick="cfgObjTab(this,'ot-chart-{{$rn}}')">{{t $.Lang "Диаграмма"}}</div>
  </div>
  <div class="obj-pane active" id="ot-params-{{$rn}}">
    {{/* Перенести БЕЗ изменений: section-hd «Параметры» + таблицу params-{{$rn}}
         (текущие ~3931–3954). */}}
  </div>
  <div class="obj-pane" id="ot-query-{{$rn}}">
    {{/* Перенести БЕЗ изменений: section-hd «Запрос» + code-wrap pre-rep-…
         (текущие ~3955–3962). */}}
  </div>
  <div class="obj-pane" id="ot-chart-{{$rn}}">
    {{/* Перенести БЕЗ изменений: поле «Процедура диаграммы» (chart_proc) +
         section-hd «Код диаграммы» + code-wrap pre-repchart-…
         (текущие ~3963–3974). */}}
  </div>
</div>
{{/* module-save-row остаётся здесь, вне .obj-pane (текущие ~3975–3980). */}}
```

- [ ] **Шаг 4. Тест проходит, сборка ОК:**

Run: `go test ./internal/launcher/ -run TestConfigurator_ReportTabs -v && go build ./internal/launcher/`
Expected: PASS, build OK.

- [ ] **Шаг 5. Коммит:**

```bash
git add internal/launcher/configurator_tmpl.go internal/launcher/configurator_tabs_render_test.go
git commit -m "feat(configurator): вкладки Параметры/Запрос/Диаграмма у отчёта (#35)"
```

---

### Задача 4: Обработка → 3 вкладки (Параметры / Код / Форма)

**Файлы:**
- Modify: `internal/launcher/configurator_tmpl.go` — панель обработки
  (`{{range .Processors}}`, строки ~4012–4106).
- Test: `internal/launcher/configurator_tabs_render_test.go` (дополнить).

- [ ] **Шаг 1. Добавить падающий тест:**

```go
func TestConfigurator_ProcessorTabs(t *testing.T) {
	html := renderTabTree(t)
	for _, sub := range []string{
		`id="ot-params-Загрузка"`,
		`id="ot-code-Загрузка"`,
		`id="ot-form-Загрузка"`,
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML нет ожидаемого фрагмента обработки: %q", sub)
		}
	}
}
```

- [ ] **Шаг 2. Запустить — падает:**

Run: `go test ./internal/launcher/ -run TestConfigurator_ProcessorTabs -v`
Expected: FAIL.

- [ ] **Шаг 3. Обернуть секции обработки во вкладки.** Аналогично отчёту:
  `.obj-editor` с тремя панелями внутри формы обработки; кнопка «Сохранить»
  после `.obj-editor`, внутри формы:

```html
<div class="obj-editor">
  <div class="obj-tabs">
    <div class="obj-tab active" onclick="cfgObjTab(this,'ot-params-{{$pn}}')">{{t $.Lang "Параметры"}}</div>
    <div class="obj-tab" onclick="cfgObjTab(this,'ot-code-{{$pn}}')">{{t $.Lang "Код"}}</div>
    <div class="obj-tab" onclick="cfgObjTab(this,'ot-form-{{$pn}}')">{{t $.Lang "Форма"}}</div>
  </div>
  <div class="obj-pane active" id="ot-params-{{$pn}}">
    {{/* Перенести БЕЗ изменений: section-hd «Параметры» + таблицу pparams-{{$pn}}
         (текущие ~4024–…). */}}
  </div>
  <div class="obj-pane" id="ot-code-{{$pn}}">
    {{/* Перенести БЕЗ изменений: блок «Исходный код» (Процедура Выполнить),
         текущий <details>/code-wrap процедуры обработки. */}}
  </div>
  <div class="obj-pane" id="ot-form-{{$pn}}">
    {{/* Перенести БЕЗ изменений: блок «Управляемая форма» обработки
         (текущие ~4062–4106), без его верхнего section-hd. */}}
  </div>
</div>
{{/* module-save-row обработки остаётся вне .obj-pane, внутри формы. */}}
```

  Внимание: блок «Управляемая форма» может находиться **вне** `<form …/processor>`.
  Если так — поместить панель `ot-form-{{$pn}}` всё равно внутрь `.obj-editor`,
  а `.obj-editor` тогда вынести из формы и обернуть им и форму, и блок формы
  (скоуп `cfgObjTab` по `.obj-editor` это допускает). Проверить фактическое
  расположение перед правкой; сохранить исходную вложенность форм.

- [ ] **Шаг 4. Тест проходит, сборка ОК:**

Run: `go test ./internal/launcher/ -run TestConfigurator_ProcessorTabs -v && go build ./internal/launcher/`
Expected: PASS, build OK.

- [ ] **Шаг 5. Коммит:**

```bash
git add internal/launcher/configurator_tmpl.go internal/launcher/configurator_tabs_render_test.go
git commit -m "feat(configurator): вкладки Параметры/Код/Форма у обработки (#35)"
```

---

### Задача 5: Ctrl+S сохраняет форму активной вкладки

Поведение DOM не покрывается юнит-тестом — проверка ручная.

**Файлы:**
- Modify: `internal/launcher/configurator_tmpl.go` — функция `cfgSaveActive`
  в `cfgFoot` (строки ~1345–1355).

- [ ] **Шаг 1. Изменить `cfgSaveActive`** — искать форму сперва в активной
  `.obj-pane`, затем (fallback) во всей панели:

```js
function cfgSaveActive() {
  var panel = document.querySelector('.cfg-panel.active');
  if (!panel) { cfgToast('Нет открытого объекта для сохранения', true); return; }
  var pick = function(scope){
    var f = null;
    scope.querySelectorAll('form').forEach(function(x){ if (!f && cfgIsAjaxForm(x)) f = x; });
    return f;
  };
  var activePane = panel.querySelector('.obj-pane.active');
  var target = (activePane && pick(activePane)) || pick(panel);
  if (!target) { cfgToast('В этом разделе нечего сохранять', true); return; }
  if (typeof target.requestSubmit === 'function') target.requestSubmit();
  else target.submit();
}
```

- [ ] **Шаг 2. Сборка и все тесты:**

Run: `go build ./internal/launcher/ && go test ./internal/launcher/`
Expected: build OK, PASS.

- [ ] **Шаг 3. Коммит:**

```bash
git add internal/launcher/configurator_tmpl.go
git commit -m "feat(configurator): Ctrl+S сохраняет форму активной вкладки объекта (#35)"
```

---

### Задача 6: Финальная проверка и закрытие плана

- [ ] **Шаг 1. Полная сборка и тесты:**

Run: `go build ./... && go test ./internal/launcher/`
Expected: всё зелёное.

- [ ] **Шаг 2. Ручная проверка** (`go build -o onebase.exe ./cmd/onebase`,
  запустить лаунчер, открыть базу → Конфигуратор):
  - справочник, документ, отчёт, обработка — вкладки переключаются, секции под
    правильными вкладками, скролла нет;
  - правка реквизита и «Сохранить» → тост «Сохранено», активная вкладка не
    сбрасывается;
  - под-вкладки Модулей (Модуль объекта/проведения/менеджера) и Форм
    (список/элемент) работают внутри своих вкладок;
  - Ctrl+S на вкладке «Формы» сохраняет формы; на «Данные» — реквизиты;
  - регистр, перечисление, константа — без изменений (без вкладок);
  - вкладка «Печатные формы» у объекта без печ. форм показывает подсказку
    со ссылкой «Создать печатную форму».

- [ ] **Шаг 3. Обновить статус плана** в шапке этого файла на
  `✅ Реализовано` и закоммитить:

```bash
git add Plans/49-configurator-tabs.md
git commit -m "docs(configurator): план 49 — вкладки реализованы (#35)"
```

- [ ] **Шаг 4. Завершение ветки** — использовать
  `superpowers:finishing-a-development-branch` (PR / merge по выбору).
