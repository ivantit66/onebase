# План 41: Инструменты для разработки с ИИ (CLI-интроспекция)

## Контекст

Цель — чтобы ИИ (и любой разработчик с ИИ) мог легко писать конфигурации для
OneBase и дорабатывать платформу. Узкое место не в «умении кодить», а в **петле
обратной связи**: без headless-способа увидеть структуру/синтаксис и быстро
поймать ошибки ИИ работает «вслепую» (показательно: для отладки обработки в этом
сеансе пришлось строить `procrun`).

Принцип: **CLI-first** — команда + текст/JSON + точная ошибка. Максимально
переиспользовать существующий код, не строить «фреймворк». MCP — отдельная
следующая фаза (см. конец).

Аудитория — **любой разработчик + его ИИ**, поэтому оптимизируем под онбординг
чужих людей: генерируемый справочник и `init`, кладущий AI-гайд в репозиторий.

### Итерация 1 (этот план)
`onebase check` + `onebase describe` + `onebase ai-guide` + дополнение `onebase init`.

---

## Часть 1. `onebase check` — гейт валидации (в основном рефактор)

Вся логика уже есть в `internal/launcher/check_handlers.go`: синтаксис `.os`,
неизвестные функции (через `interpreter.KnownBuiltinNames()`), YAML-схема всех
видов объектов, компиляция запросов виджетов/отчётов — с `file:line:col`. Но она
package-private в `launcher`.

- **Вынести ядро в новый пакет `internal/configcheck`** (экспортировать):
  `Issue`/`Result`, `CheckDir(dir) []Issue` (бывш. `checkProjectDir`),
  `CheckQueries(proj) []Issue`, `ParseDSL`, `CheckDSLCalls` + AST-walkers.
- `launcher/check_handlers.go` → тонкие обёртки над `configcheck`; HTTP-эндпоинты
  конфигуратора и per-fragment проверки редактора сохраняются без изменений.
- **Команда** `onebase check [--project dir | --id base | --sqlite … ] [--json]`:
  печатает `file:line:col: message` (текст) или `Result` (JSON); **exit code ≠ 0**
  при наличии ошибок — готовый pre-commit/CI-гейт. Для db-config — экспорт в
  temp-dir (как `materializeProject`).

---

## Часть 2. `onebase describe --json` — «рентген» конфигурации

`internal/cli/describe.go`. Загружает проект (`project.Load`) и сериализует
`project.Project` (там уже всё: Entities/Registers/InfoRegisters/Enums/Constants/
Reports/Processors/Subsystems/Journals/AccountRegisters/Widgets/HomePage) в чистый
JSON. Плюс:
- по каждому модулю/обработке — имена процедур и их параметры (`prog.Procedures`:
  `Name.Literal` + `Params`);
- `builtins`: отсортированный `KnownBuiltinNames()`.

Это стабильный контракт, который позже отдаст MCP как ресурс. Read-only.

---

## Часть 3. `onebase ai-guide` — справочник, генерируемый из платформы

`internal/cli/aiguide.go` + генератор markdown (его же зовёт `init`):
- layout/конвенции репозитория конфигурации (catalogs/, documents/, src/*.os, …);
- как валидировать (`onebase check`) и запускать (`onebase procrun`, `describe`);
- builtins — список из `KnownBuiltinNames()`, **сгруппированный по источнику**
  (core / HTTP / Email / Tx / Chart / Spreadsheet) — авто, не устаревает;
- краткая схема метаданных — из enum'ов `metadata` (виды объектов, типы полей);
- модель безопасности (dev-инструменты ≠ опубликованный рантайм).

**Ограничение:** реестр builtins хранит только имена, без сигнатур/описаний.
Сигнатуры в v1 не генерируем (follow-up: курируемая таблица или doc-комментарии).

---

## Часть 4. `onebase init` кладёт AI-гайд

`internal/cli/init.go` уже скаффолдит конфигурацию. Дополнить: писать `AGENTS.md`
(содержимое из генератора ai-guide) в созданный каталог — у нового разработчика
ИИ сразу ориентируется. Общий генератор, без дублирования.

---

## Общий хелпер резолвинга базы

`procrun.go` уже резолвит `--id/--project/--sqlite/--db`. Вынести в
`internal/cli/baseresolve.go` (dir [+ db для procrun]) и переиспользовать в
check/describe. Рефактор procrun на хелпер — низкий риск.

---

## Критические файлы

- NEW `internal/configcheck/check.go` — вынос из `launcher/check_handlers.go`.
- MOD `internal/launcher/check_handlers.go` — обёртки над `configcheck`.
- NEW `internal/cli/check.go`, `describe.go`, `aiguide.go`.
- MOD `internal/cli/init.go` — запись `AGENTS.md`.
- NEW `internal/cli/baseresolve.go` + MOD `internal/cli/procrun.go`.
- Reuse: `interpreter.KnownBuiltinNames()`, `project.Load`, `query.Compile`,
  `metadata.*`, `runtime.Registry`, `configdb.ExportToDir`, паттерн cobra (procrun.go).

## Проверка

1. `onebase check --project <PuT>` → «OK» или `file:line:col: message`; заведомая
   ошибка в `.os` ловится; exit code ≠ 0.
2. `onebase describe --project <PuT> --json | jq` → валидный JSON со всеми видами
   объектов + `builtins`.
3. `onebase ai-guide` → markdown со списком builtins, layout, командами.
4. `onebase init <tmp>` → в каталоге появился `AGENTS.md`.
5. Go-тесты: `configcheck` (чистый → 0 issues; битый `.os` → issue со строкой),
   `describe`-ключи; существующие тесты зелёные; `go build ./...` + полный прогон.

---

## Фаза 2 (не в этой итерации, ориентир)

`onebase mcp` — тонкая обёртка над CLI: resources (describe-JSON, исходники
модулей, builtins) + tools (check, describe, procrun). Контракт = JSON из `describe`.
Любой MCP-клиент подключается одной строкой конфига.
