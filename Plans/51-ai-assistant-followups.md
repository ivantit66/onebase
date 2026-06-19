# План 51 (follow-ups): UI провайдеров/моделей · tool-use OpenAI/Gemini · два зазора RBAC

> Дизайн-спека (brainstorming). Продолжение `51-llm-business-assistant.md`.
> Детальный план реализации — в `51-followups-impl.md` (создаётся отдельно).
> Дата: 2026-06-19.

## Контекст

План 51 закрыт по платформенной части (F1–F6). В его «итоге» перечислены 4 follow-up'а.
Разведка по коду показала: **2 из 4 уже сделаны** другими планами уже после того,
как список follow-up'ов был записан:

- **Объектный RBAC на произвольные запросы** — реализован планом 54 (влит из `origin/main`):
  у скомпилированного запроса есть `query.Result.Sources []SourceRef{Kind,Name}`
  (`internal/query/query.go:46`), перед исполнением `aiDeniedSource` проверяет права на
  каждый источник через `canCtx(...,"read")` (`internal/ui/ai_tools.go:112`), есть три
  режима доступа `admin_only`/`rbac`/`all` (`storage.GetAIDataScope`) и per-user флаг
  `AIDataAccess`.
- **Срез `describe` в контексте конфигуратора** — реализован планом 57 (этап 1, коммит
  `dd56890`): пакет `internal/aicontext` строит компактный текстовый срез конфигурации и
  вшивает его в системный промпт конфигуратора (`internal/launcher/ai_assist.go:137-140`).

**Реально осталось два follow-up'а** + один явный запрос пользователя. Этот документ
покрывает **три пункта**:

1. **UI провайдеров/моделей** (запрос пользователя) — заменить сырой JSON-редактор на
   нормальные формы.
2. **Tool-use для OpenAI/Gemini** (незакрытый follow-up) — агентный цикл сейчас только
   Anthropic/GLM.
3. **Два мелких зазора RBAC** (полировка плана 54) — источники-регистры бухгалтерии и
   фильтрация инструмента `описание_данных`.

**Не входит в этот заход:** демо умной загрузки документов для `examples/trade` (отложено
пользователем; там отдельный платформенный зазор — параметр обработки `type: file` отдаёт в
DSL текст, а не base64/бинарь).

Решения по развилкам (согласовано в brainstorming):
- Раскладка UI — **гибрид**: «что делает ИИ» сверху + таблицы провайдеров/моделей в
  раскрытии.
- Редактирование строк — **inline**; цепочка моделей задачи — **нумерованный список с ↑↓**.
- «Показать JSON» — **тоггл в режим сырья** (правка + сохранение там же).
- Регистры бухгалтерии в RBAC — **маппить на право `register`**.
- `описание_данных` — **фильтровать по правам** для не-админа в режиме `rbac`.

---

## Часть 1. UI настроек провайдеров/моделей

### Что есть сейчас
Страница «ИИ-помощник» (`internal/launcher/ai_handlers.go:45` `cfgAdminAI`) рендерит HTML
через `fmt.Sprintf` и отдаёт как фрагмент; редактор конфига — **единственная
`<textarea id="ai-cfg">`** с indent-JSON от `cfg.Redacted()` (ключи маскированы). Сохранение
— `cfgAdminAISave` (`ai_handlers.go:173`), проверка — `cfgAdminAITest` (`ai_handlers.go:207`).
Отдельная секция «Доступ ИИ-чата к данным» (`admin_only`/`rbac`/`all`) — `scopeSection`
(`ai_handlers.go:107`). Стек конфигуратора — Go `html/template` каркас + ванильный JS;
админ-фрагменты грузятся `cfgAdmin(name)` (`static/configurator.js:3013`) в модальный оверлей,
скрипты фрагмента пересоздаются вручную.

### Целевая раскладка (гибрид, утверждена визуально)
В модалке (≈800px, скролл):
- Шапка: флаг **«ИИ-помощник включён»** (`Config.Enabled`) + тоггл **«⚙ Показать JSON»**.
- Секция **«Что делает ИИ»** — строки задач (`документы`/`анализ`/`чат`/`конфигуратор` и
  любые из `Config.Profiles`): иконка, имя задачи, **цепочка моделей чипами** (`модель →
  модель`, порядок = приоритет фолбэка), кнопка **«Проверить»**, место под результат, ✎.
- Раскрывающийся блок **«Провайдеры и ключи»** — таблица Endpoints (Имя, Тип, Base URL,
  Ключ-маска, ✎/🗑) + «+ добавить провайдера».
- Раскрывающийся блок **«Модели»** — таблица Models (Имя, Провайдер, Vision, MaxTokens,
  ✎/🗑) + «+ добавить модель».
- Подвал: `#msg` + **«Сохранить»**.
- Существующая секция **«Доступ ИИ-чата к данным»** — сохраняется, перерисовывается в новом
  стиле.

### Взаимодействие
- **Inline-редактирование** строк провайдера/модели: ✎ превращает строку в input/select,
  ✓ применяет (в память), ✕ отменяет.
- **Редактор цепочки задачи**: ✎ раскрывает inline нумерованный список моделей с ↑↓ (порядок),
  ✕ (убрать) и выпадающим «+ добавить модель» — только существующие `Config.Models`; для
  задачи `документы` vision-модели подсвечены.
- **«Показать JSON»**: тоггл скрывает формы и показывает `<textarea>` с текущим (собранным из
  формы) JSON; правка + «Сохранить» идут тем же путём. Обратный тоггл перечитывает JSON в
  формы (с предупреждением при невалидном JSON).
- **Стартовая заготовка** для пустой базы (`starterLLMConfig()`, `ai_handlers.go:19`) —
  сохраняется, но рендерится как предзаполненные строки форм.

### Архитектура (backend почти не меняется)
Весь конфиг — один JSON (`Config`, `internal/llm/config.go:51`; хранение — `llm.config` в
`_settings`). Клиентская модель:
- Новый JS держит объект `Config` в памяти, рендерит все секции из него, правки мутируют
  объект.
- **«Сохранить»** POST'ит **весь** конфиг на существующий `cfgAdminAISave` — там уже
  `cfg.UnmaskKeys(prev)` подставляет реальные ключи вместо неизменённых масок `****`
  (`config.go:124`). Логика ключей переиспользуется без изменений.
- **«Проверить»** по задаче POST'ит `{config, task}` на существующий `cfgAdminAITest`
  (`ai_handlers.go:207`) — гоняет резолв-цепочку задачи (заодно проверяя фолбэк), результат
  пишется в место под строкой задачи.
- Меняется **только**: рендер `cfgAdminAI` (textarea → каркас формы) + новый JS. Эндпоинты
  `save`/`test`/`datascope`, `Redacted`/`UnmaskKeys`, формат `Config` — **без изменений**.

**Где живёт JS:** новый файл **`internal/launcher/static/ai-settings.js`**, отдаётся через
существующий static-embed (`internal/launcher/static.go`). Внимание: в `main` директива
embed'ит **по конкретным файлам** (`//go:embed static/js-yaml.min.js`), а не всю папку —
значит `ai-settings.js` нужно **явно добавить в `//go:embed`** и убедиться, что роут
`/static/*` его отдаёт. Фрагмент подключает его `<script src>` и вызывает идемпотентный
`aiSettingsInit(baseId)` (cfgAdmin пересоздаёт скрипты при каждом открытии — init не должен
дублировать обработчики/глобали; обернуть в IIFE). Альтернатива (если станет проще) — inline
`<script>` в фрагменте по образцу остальных админ-страниц; выбор зафиксировать в `-impl`.

**Клиентская валидация-предупреждения** (не блокирует сохранение, показывается inline):
- модель ссылается на несуществующий провайдер (`Model.Endpoint` ∉ `Endpoints`);
- профиль ссылается на несуществующую модель (`Profile.Models[i]` ∉ `Models`);
- задача `документы` содержит модель без `Vision`.

---

## Часть 2. Tool-use для OpenAI/Gemini

### Что есть сейчас
Tool-use — только Anthropic-протокол (`internal/llm/anthropic_tools.go`,
`completeAnthropicTools`). В `Runner.RunWithTools` (`internal/llm/run.go:76`) жёсткое
`if rm.Endpoint.Kind != KindAnthropic { continue }` (`run.go:87`) отбирает только
anthropic-модели; при их отсутствии деградирует до `Run`. Типы tool-use
(`Tool`/`ToolCall`/`ToolResult`/`ToolExecutor`, `MaxToolIterations=12`) — провайдеро-нейтральны
(`internal/llm/tools.go`). `Tool.Schema map[string]any` — сырая JSON Schema.

### Изменения
- Ввести **диспетчер `completeTools(ctx, hc, rm, req, tools, exec)`** по образцу `complete`
  (`run.go:129`): switch по `rm.Endpoint.Kind` — `KindAnthropic`→`completeAnthropicTools`,
  `KindOpenAI`/`KindCompatible`→**`completeOpenAITools`**, `KindGemini`→**`completeGeminiTools`**.
- В `RunWithTools` заменить `if Kind != KindAnthropic { continue }` на проверку «есть ли
  tools-реализация для Kind» + вызов диспетчера. Цепочка фолбэка теперь включает все
  протоколы. Деградация сохраняется: пустой `tools` → `Run`; пустая/несовместимая цепочка →
  `Run`.
- **`completeOpenAITools`** (новый файл `openai_tools.go`): тело запроса +
  `tools:[{"type":"function","function":{name,description,parameters: schema}}]`; ответ —
  `choices[0].message.tool_calls[]{id, function:{name, arguments}}` (где `arguments` — **JSON-
  строка**, `Unmarshal` в `ToolCall.Input`), конец цикла по `finish_reason=="tool_calls"`;
  ассистентский ход реконструируется с полем `tool_calls`, результаты —
  `{"role":"tool","tool_call_id":id,"content":res.Content}`.
- **`completeGeminiTools`** (новый файл `gemini_tools.go`): тело +
  `tools:[{"functionDeclarations":[{name,description,parameters: schema}]}]`; ответ —
  `candidates[0].content.parts[].functionCall{name,args}`, конец цикла — отсутствие
  `functionCall` в parts; ассистентский ход — `{"role":"model","parts":[…functionCall…]}`,
  результат — part `{"functionResponse":{name, response:{…}}}`.
- Цикл `MaxToolIterations`, аккумуляция токенов, обработка ошибок (`postJSON`/`APIError`/
  `shouldFallback`) — как в `anthropic_tools.go`.
- **Ограничение** (осознанно, как у Anthropic сейчас): tool-путь **текстовый** — изображения
  в tools не передаются.

### Тесты
По образцу `tools_test.go` — `httptest`-моки openai и gemini протоколов: полный цикл вызова
инструмента (модель просит инструмент → исполнение → финальный ответ), деградация при пустых
tools, фолбэк по цепочке смешанных протоколов. Без реальной сети.

---

## Часть 3. Два зазора RBAC (полировка плана 54)

### 3a. Источники-регистры бухгалтерии
Сейчас `sourcePermKind` (`internal/query/query.go:54`) для регистра бухгалтерии возвращает
`""` → `canCtx(ctx,"",имя,"read")` проваливается в `User.Has` к `false` для не-админа, т.е.
любой запрос, касающийся регбуха, в режиме `rbac` блокируется для не-админа.
**Фикс:** `sourcePermKind` маппит тип регбуха на **`"register"`**. Тогда
`aiDeniedSource → canCtx(ctx,"register",имя,"read")` проверяет право штатно (`User.Has`,
`internal/auth/roles.go:38`). Имена регистров бухгалтерии не конфликтуют с регистрами
накопления, ключ `Permission.Registers` — по имени.
**Чтобы право можно было выдать:** регистры бухгалтерии добавляются в список «регистров» в
редакторе ролей (UI ролей / синхронизация ролей), иначе админ не сможет выдать read.

### 3b. Фильтрация `описание_данных`
Инструмент `описание_данных` (`aiSchemaText`, `internal/ui/ai_tools.go:84`) отдаёт **полную**
схему конфигурации без фильтра по правам.
**Фикс:** для не-админа в режиме `rbac` срез строится **только из объектов с правом read**
(`canCtx(ctx, kind, name, "read")`) — согласованно с фильтрацией `выполнить_запрос`. `aiSchemaText`
собирает `aicontext.Input` из разрешённых справочников/документов/регистров/инфо-регистров/
регбухов (регбух — kind `"register"` по 3a). Объекты без права в срез не попадают.

### Тесты
- Запрос к регистру бухгалтерии без права → отказ `нет доступа к объекту: …`; с правом →
  проходит.
- `описание_данных` у не-админа в режиме `rbac` не содержит запрещённых объектов; у админа —
  полный.

---

## Критические файлы

**Часть 1 (UI):**
- **MOD** `internal/launcher/ai_handlers.go` — `cfgAdminAI`: textarea → каркас формы
  (подключение `ai-settings.js`, контейнеры секций, начальные данные конфига в JSON для JS).
  `cfgAdminAISave`/`cfgAdminAITest`/`cfgAdminAIDataScope` — **без изменений**.
- **NEW** `internal/launcher/static/ai-settings.js` — рендер форм, inline-правка, редактор
  цепочки, тоггл JSON, валидация, save/test через существующие эндпоинты.
- **MOD** `internal/launcher/static.go` — добавить `ai-settings.js` в `//go:embed` (в `main`
  embed по конкретным файлам).
- **Reuse:** `llm.Config`/`Redacted`/`UnmaskKeys` (`internal/llm/config.go`),
  `storage.Get/SaveLLMConfig` (`internal/storage/settings.go:224`), паттерны таблиц/форм
  админки (`internal/launcher/admin_handlers.go`).

**Часть 2 (tool-use):**
- **NEW** `internal/llm/openai_tools.go`, `internal/llm/gemini_tools.go`.
- **MOD** `internal/llm/run.go` — диспетчер `completeTools` + правка отбора в `RunWithTools`
  (`run.go:76-116`).
- **NEW** тесты в `internal/llm/` (моки openai/gemini).
- **Reuse:** `anthropic_tools.go` (образец), `tools.go`, `openai.go`/`gemini.go` (формирование
  сообщений/usage), `types.go`/`httpjson.go`.

**Часть 3 (RBAC):**
- **MOD** `internal/query/query.go` — `sourcePermKind` (регбух → `"register"`).
- **MOD** `internal/ui/ai_tools.go` — фильтрация `aiSchemaText` по правам.
- **MOD** редактор/синхронизация ролей — регбухи в списке «регистров» (`internal/auth/roles.go`
  + UI ролей в `internal/launcher`).
- **Reuse:** `User.Has`/`canCtx` (`internal/ui/handlers.go:253`), `aiDeniedSource`,
  `query.Result.Sources`.

## Verification

1. **UI:** на пустой базе видна заготовка формами; добавление провайдера/модели/задачи,
   inline-правка, редактор цепочки с ↑↓ работают; «Проверить» по задаче даёт ответ модели и имя
   ответившей; ключи маскируются (`****`), при сохранении без изменения маски — ключ
   сохраняется прежним; тоггл «Показать JSON» туда-обратно не теряет данные; предупреждения о
   битых ссылках видны. `go build -tags webview ./...` зелёный.
2. **Tool-use:** Go-тесты на `httptest`-моках openai и gemini проходят полный цикл вызова
   инструмента и деградацию; смешанная цепочка фолбэка работает; существующие `tools_test.go`
   зелёные.
3. **RBAC:** тест — запрос к регбуху без права отклонён, с правом проходит; `описание_данных`
   у не-админа в `rbac` не раскрывает запрещённые объекты.
4. **Сборка/тесты:** `go test ./...` зелёный, без реальных сетевых вызовов в тестах.

## Что НЕ трогаем
Демо умной загрузки (отложено), хранилище конфига (`_settings`/`llm.config`),
`Redacted`/`UnmaskKeys`, формат `Config`, vision-в-tools (осознанно за рамками).
