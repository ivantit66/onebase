# Тип поля richtext (HTML с картинками) — план 65

**Статус:** ✅ Реализовано (сверка 2026-06-25). В коде есть
`metadata.FieldTypeRichText`, явная TEXT-ветка DDL, запрет richtext в табличных
частях, пакет `internal/richtext` с bluemonday-санитайзером и `MaxBytes`,
санитизация при записи и defense-in-depth при выводе, офлайн Quill в
`internal/webassets/quill`, рендер richtext в autogen/managed формах, а также
печать через `sheet.Cell.RichHTML`: HTML выводит санитизированный блок, PDF
строит текстово-картиночную проекцию. Тесты закрывают санитайзинг, лимит
размера, Quill-ассеты, richtext в печатных формах и PDF.

## Context

Issue #43 (тестировщик `1cboris`): нужны поля для хранения HTML с картинками (сценарий — система учёта задач, поля «результат выполнения»/«результат тестирования», исполнитель пишет текст и вставляет скриншоты; печать сейчас делается внешним pandoc→Word). Согласовано с тестировщиком: сначала печатные формы v2 (план 64 — **завершён**), затем richtext, причём печать richtext ляжет на готовый механизм печатных форм.

Исходное состояние до реализации: тип поля добавлялся тривиально (`FieldType` — строка, голый `richtext` уже проходил парсер), TEXT-хранение и миграция были готовы (дефолтная ветка `ddl.go fieldType`), CRUD строк без спецобработки. **Не было**: WYSIWYG-редактора, HTML-санитайзера, inline-вложений, вывода HTML в ячейку печатной формы (`sheet.Cell` экранировал `Text`).

**Решения пользователя** (зафиксированы): (1) редактор — **Quill** (vendored, офлайн, как monaco/echarts); (2) картинки — **base64 data-URI прямо в поле** (самодостаточно, работает в HTML- и PDF-печати — data-URI поддержан с этапа 7 плана 64; лимит размера защищает от раздувания); (3) печать — **HTML полная + PDF текст+картинки** (fpdf не рендерит произвольный HTML); санитайзер — **bluemonday** (стандарт для UGC, обоснованно против самописного).

## Целевая архитектура

- **Тип поля** `richtext` (`metadata.FieldTypeRichText`), хранится в TEXT-колонке (как string). `IsRichText(ft)` helper. Только в реквизитах шапки сущности — НЕ в табличных частях (валидация запрещает; SlickGrid-редактор для richtext вне MVP).
- **Санитайзер** `internal/richtext`: bluemonday-политика — allowlist форматирующих тегов (`p, br, b/strong, i/em, u, s, ul/ol/li, h1-h3, blockquote, a[href safe-scheme], img[src=data:image/* only], span/div`), вырезание `<script>`, on*-атрибутов, `javascript:`/внешних `src` у img (только data-URI картинки, согласовано). Применяется **на записи** (вход формы → перед сохранением) И **на выводе** (defense-in-depth перед `template.HTML`). Плюс `Plaintext(html)` (для проекций/поиска).
- **Лимит размера**: richtext-значение > N (по умолчанию ~4 МБ, т.к. base64-картинки) → ошибка сохранения (понятное сообщение). Защита TEXT-колонки и формы.
- **Редактирование**: этап 1 — `<textarea>` (сырой HTML, функционально и тестируемо); этап 2 — Quill поверх (textarea остаётся скрытым полем формы, Quill синхронизирует). Прогрессивное улучшение.
- **Показ read-only** (просмотр элемента, список): санитизированный HTML через `template.HTML` (списки — усечённая plaintext-проекция, чтобы строки не разъезжались).
- **Печать** (этап 3): `sheet.Cell` получает richtext-контент (новое поле + признак, не через экранируемый `Text`); `html.go` выводит санитизированный HTML-блок; `pdf.go` — текстовая проекция (bold/italic/абзацы) + встроенные data-URI картинки; `printform` binding помечает ячейку как richtext, если параметр привязан к richtext-полю.

## Этапы (каждый = рабочий PR от main; ветки feature/65-richtext-stageN)

### Этап 1 — фундамент (тип, санитайзер, хранение, лимит, textarea-редактор, показ, валидация) ✅
- `metadata/types.go`: `FieldTypeRichText = "richtext"` + `IsRichText`. `validate.go`: запрет richtext в табличных частях (внятная ошибка). `ddl.go`: явная ветка TEXT (для ясности; поведение не меняется).
- Новый пакет `internal/richtext`: `Sanitize(html string) string` (bluemonday policy), `Plaintext(html string) string`, `const MaxBytes`. Тесты на XSS-векторы (script/onerror/javascript:/external img → вырезаны; data:image — сохранён; форматирование — сохранено).
- go.mod: + `github.com/microcosm-cc/bluemonday`.
- Сохранение: в пути формы (`handlers.go formToFields` / `handlers_entity.go parseSubmitForm`) — для richtext-поля `Sanitize` + проверка `MaxBytes` (ошибка формы при превышении).
- Форма (autogen `templates.go page-form`): ветка richtext → `<textarea name=…>` с сырым (санитизированным) HTML; инициализация значением без двойного экранирования.
- Показ read-only: на странице просмотра — `template.HTML(richtext.Sanitize(value))`; в списках — `richtext.Plaintext` усечённый.
- Тесты: сохранение richtext (санитайз применён), oversize → ошибка, показ безопасен, валидация TP.

### Этап 2 — Quill (WYSIWYG) ✅
- Вендоринг Quill в `internal/webassets/quill` (go:embed) + handler + `static.go mountStatic` `/vendor/quill/`. Лаунчер-конфигуратор при необходимости тоже.
- `page-form` + `templates_managed.go`: richtext-ветка инициализирует Quill над скрытым textarea (Quill → textarea sync перед submit); панель форматирования + вставка картинки (base64). Управляемые формы (header-поле) — та же интеграция.
- Прогрессивно: без JS остаётся рабочий textarea.
- Тесты рендера (контейнер Quill, vendor-роут).

### Этап 3 — печать richtext ✅
- `sheet.Cell`: поле под richtext (например `RichHTML string`) + рендер: `html.go` — санитизированный HTML-блок (не экранированный `Text`); `pdf.go` — проекция (парс ограниченного HTML: абзацы/переносы/bold/italic + data-URI `<img>` через существующий `decodeDataURIImage`).
- `printform/declarative.go` + `binding.go`: параметр, привязанный к richtext-полю сущности, помечает ячейку как rich (по `FieldType` поля в метаданных).
- Тесты: richtext в HTML-печати (форматирование/картинка), PDF (текст+картинка, не падает на сложном HTML).

## Критические файлы

- `internal/metadata/types.go`, `validate.go`, `internal/storage/ddl.go` — тип/валидация/DDL (этап 1)
- `internal/richtext/*.go` — санитайзер (новый, этап 1)
- `internal/ui/handlers.go` (formToFields), `handlers_entity.go` (parseSubmitForm), `templates.go` (page-form), `templates_managed.go` — сохранение/форма/показ (этапы 1-2)
- `internal/webassets/`, `internal/ui/static.go` — вендоринг Quill (этап 2)
- `internal/sheet/{sheet,html,pdf}.go`, `internal/printform/{binding,declarative}.go` — печать (этап 3)

## Верификация

- Этап 1: `go test ./internal/richtext/ ./internal/ui/ ./internal/metadata/ -count=1` (санитайзер режет XSS, сохраняет форматирование+data-URI; oversize→ошибка; richtext-в-TP→ошибка валидации); `onebase check` на examples; ручная: создать сущность с richtext-полем, сохранить HTML, увидеть санитизированным.
- Этап 2: редактор Quill грузится офлайн (vendor-роут), форматирование и вставка картинки работают, значение сохраняется; без JS — textarea.
- Этап 3: richtext-поле в печатной форме — HTML с форматированием/картинкой; серверный PDF — текст+картинка, не падает.
- Везде: `go test ./...`, golden 5/5, `go vet`, `go build`, i18ncheck (новые t-ключи en+de), сборка.

## Риски

- **XSS** — главный риск: санитайзинг и на входе, и на выходе; тесты на известные векторы; только data-URI картинки (внешние/js — вырезаются).
- Раздувание БД base64-картинками — лимит размера поля; в будущем (отдельный план) — опционально вынос в attachments.
- PDF не рендерит произвольный HTML — осознанная проекция (текст+картинки+базовое форматирование), задокументировать ограничение.
- Размер бинаря +Quill (~200КБ) — приемлемо (monaco 4.2МБ уже встроен).

## Follow-up (вне плана)

- richtext в табличных частях (SlickGrid custom editor) — если будет спрос.
- Вынос картинок в attachments с дедупликацией — оптимизация хранения.
- Богатая проекция richtext→PDF (таблицы/списки) — если плоской текст+картинки окажется мало.
