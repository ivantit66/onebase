package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/spf13/cobra"
)

var aiguideCmd = &cobra.Command{
	Use:   "ai-guide",
	Short: "Сгенерировать AGENTS.md — справочник по разработке на OneBase для ИИ",
	Long: `Печатает markdown-справочник, сгенерированный из самой платформы:
структура конфигурации, рабочий цикл (check/describe/procrun), встроенные
функции DSL (сгруппированы по источнику), схема метаданных, модель безопасности.
Тот же текст onebase init кладёт в AGENTS.md новой конфигурации.

Примеры:
  onebase ai-guide
  onebase ai-guide --output AGENTS.md`,
	RunE:          runAIGuide,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	aiguideCmd.Flags().String("output", "", "записать руководство в файл вместо stdout (обычно AGENTS.md)")
	aiguideCmd.Flags().Bool("claude", false, "также положить рядом CLAUDE.md-указатель (авто-загрузка Claude Code); не перезаписывает существующий")
	rootCmd.AddCommand(aiguideCmd)
}

func runAIGuide(cmd *cobra.Command, _ []string) error {
	guide := generateAIGuide()
	out, _ := cmd.Flags().GetString("output")
	if out == "" {
		fmt.Fprint(os.Stdout, guide)
		return nil
	}
	if err := os.WriteFile(out, []byte(guide), 0o644); err != nil {
		return err
	}
	// --claude: рядом с руководством кладём CLAUDE.md-указатель (Claude Code
	// авто-загружает его) — чтобы ИИ сразу видел инструменты без подсказок.
	if withClaude, _ := cmd.Flags().GetBool("claude"); withClaude {
		claudePath := filepath.Join(filepath.Dir(out), "CLAUDE.md")
		if _, err := os.Stat(claudePath); os.IsNotExist(err) {
			if werr := os.WriteFile(claudePath, []byte(claudePointer), 0o644); werr != nil {
				return werr
			}
		}
	}
	return nil
}

// generateAIGuide строит справочник из платформы. Списки builtins берутся из
// реестра функций (не устаревают), остальное — стабильные конвенции платформы.
func generateAIGuide() string {
	var b strings.Builder
	b.WriteString(`# OneBase — руководство для ИИ-разработчика

Этот файл сгенерирован командой ` + "`onebase ai-guide`" + ` из самой платформы.
OneBase — 1С-подобная платформа: конфигурация описывает прикладные объекты
(метаданные YAML) и логику (DSL ` + "`.os`" + `), платформа их исполняет.

## Структура репозитория конфигурации

` + "```" + `
config/app.yaml         настройки приложения (имя, язык, БД)
config/home_page.yaml   главная страница
catalogs/*.yaml         справочники
documents/*.yaml        документы
registers/*.yaml        регистры накопления
inforegs/*.yaml         регистры сведений
enums/*.yaml            перечисления
constants/*.yaml        константы
reports/*.yaml          отчёты (+ запросы)
processors/*.yaml       обработки (мастера с параметрами)
subsystems/*.yaml       подсистемы (структура интерфейса/прав)
journals/*.yaml         журналы документов
widgets/*.yaml          виджеты главной страницы
roles/*.yaml            роли (RBAC)
forms/                  управляемые формы (опционально)
src/*.os                модули DSL (логика объектов, обработок, отчётов)
` + "```" + `

Имена файлов ` + "`.os`" + ` (в нижнем регистре) соответствуют объектам:
` + "`<объект>.posting.os`" + ` — проведение документа, ` + "`<обработка>.proc.os`" + ` —
логика обработки (процедура ` + "`Выполнить()`" + `), ` + "`<отчёт>.rep.os`" + ` — отчёт,
` + "`<объект>.manager.os`" + ` — модуль менеджера.

## Рабочий цикл (всё headless, текст/JSON)

Все команды — через бинарь ` + "`onebase`" + `. Если оболочка отвечает ` + "`onebase: command not found`" + `,
он не установлен в PATH. Установка (нужен Go): ` + "`go install github.com/ivantit66/onebase/cmd/onebase@latest`" + `
и убедись, что каталог ` + "`go env GOPATH`/bin" + ` есть в PATH. Из исходников:
` + "`go build -o <dir-в-PATH>/onebase ./cmd/onebase`" + `. Проверка: ` + "`onebase --help`" + `.
Внутри проекта флаг ` + "`--project`" + ` можно опускать — берётся текущий каталог.

| Команда | Назначение |
|---------|-----------|
| ` + "`onebase check --project <dir>`" + ` | Валидация: синтаксис .os, неизвестные функции, YAML-схема, компиляция и исполнение запросов. Выводит ` + "`file:line:col: message`" + `, exit code ≠ 0 при ошибках. Запускай после каждой правки. |
| ` + "`onebase describe --project <dir>`" + ` | Вся структура конфигурации + список builtins в JSON. «Рентген» для понимания базы. |
| ` + "`onebase procrun --project <dir> --proc <Имя> --set К=З --file П=путь`" + ` | Запуск обработки офлайн, печать ` + "`Сообщить()`" + `. Отладка прикладной логики. |
| ` + "`onebase run --project <dir> --sqlite <файл> --port N`" + ` | Поднять сервер (UI + REST). |

## Язык DSL

1С-подобный, ключевые слова и идентификаторы на русском, регистронезависимый.
Строки — UTF-8. Встроенные функции (по источнику):

`)
	for _, g := range builtinGroups() {
		if len(g.names) == 0 {
			continue
		}
		fmt.Fprintf(&b, "**%s:** %s\n\n", g.title, strings.Join(g.names, ", "))
	}
	b.WriteString(`> Справочник перечисляет имена функций (из реестра платформы — не устаревает).
> Сигнатуры смотрите в примерах существующих модулей конфигурации.

## Запросы — частые ошибки (проверь перед коммитом)

` + "`onebase check`" + ` ловит синтаксис и компиляцию запроса, но **не исполнение** и **не
семантику сравнений** — проверяй результат виджета/отчёта на реальных данных. Типовые ловушки:

- **Виртуальные таблицы — только со скобками:** ` + "`РегистрНакопления.X.Остатки(...)`, `.Обороты(...)`, `.СрезПоследних(...)`" + `.
  Без ` + "`()`" + ` суффикс уходит в имя физической таблицы → ` + "`no such table`" + `.
- **HAVING — деепричастие ` + "`ИМЕЯ`" + `** (не ` + "`ИМЕЮ`" + `). Неизвестное слово молча уходит в SQL как
  идентификатор → синтаксическая ошибка в рантайме.
- **Квалифицируй колонку ` + "`Объект.Поле`" + `,** если ссылаешься на reference-поле, чей каталог имеет
  одноимённую колонку (` + "`Статус`, `Дата`, `Наименование`" + `) — иначе ` + "`ambiguous column`" + ` от авто-JOIN.
- **Служебное поле периода регистра в запросах — ` + "`period`" + `** (snake_case), движение — ` + "`вид_движения`" + `.
- **Числовые поля на SQLite сравниваются/сортируются как строки.** ` + "`number`" + ` хранится как TEXT
  (точный decimal), поэтому ` + "`ГДЕ Цена > 100`, `... < Н.МинимальныйОстаток`, `УПОРЯДОЧИТЬ ПО Сумма`" + `
  коэрсятся к строковому сравнению («10» < «9»). Оборачивай сырую колонку:
  ` + "`CAST(Н.МинимальныйОстаток AS NUMERIC)`" + ` — **не** ` + "`AS INTEGER`" + ` (усекает дробь). Агрегаты
  ` + "`СУММА(...)`" + ` уже числовые, их кастить не нужно.
- **Скалярные 1С-функции работают и в запросах** (имена совпадают со скриптовым DSL):
  математика ` + "`ОКР(x[, n])`, `АБС(x)`, `ЦЕЛ(x)`" + ` (усечение к нулю); даты
  ` + "`Год/Месяц/День(x)`, `НачалоДня/НачалоМесяца/НачалоГода(x)`" + `. Транслируются в
  нативный SQL под диалект — сырое русское имя в БД не уходит.

## Схема метаданных (основное)

- **Виды объектов:** справочник, документ, регистр накопления, регистр сведений,
  перечисление, константа, отчёт, обработка, подсистема, журнал, виджет.
- **Типы полей:** ` + "`string`, `number`, `date`, `bool`, `text`, `reference:<Объект>`, `enum:<Перечисление>`" + `.
- У документов есть проведение (` + "`posting`" + `) и движения по регистрам; у справочников —
  иерархия (` + "`hierarchical`" + `), предопределённые элементы, ввод на основании (` + "`based_on`" + `).

Точную структуру конкретной базы смотри через ` + "`onebase describe`" + `.

## Безопасность

Инструменты разработчика (консоль кода/запросов, отладчик, ` + "`procrun`" + `) — для
**разработки**. На опубликованной (задеплоенной) базе debug-API недоступен без
внутреннего токена, а консоли требуют сессию администратора. Не полагайся на их
наличие в проде; данные мутирующие операции (запись/проведение) — только явно.
`)
	return b.String()
}

// claudePointer — крошечная заглушка CLAUDE.md, которую авто-загружают
// ИИ-ассистенты (в частности Claude Code). Не дублирует AGENTS.md, а указывает
// на него — так руководство остаётся в одном месте и не устаревает.
const claudePointer = `# OneBase — конфигурация

Это проект конфигурации платформы OneBase (1С-подобная: метаданные YAML + DSL ` + "`.os`" + `).

**Инструменты и правила разработки — в ` + "`AGENTS.md`" + `** (сгенерирован ` + "`onebase ai-guide`" + `,
всегда синхронизирован с версией движка). Перед правкой запросов прочитай раздел
«Запросы — частые ошибки» в ` + "`AGENTS.md`" + `.

Быстрый цикл:
- ` + "`onebase check`" + ` — валидация перед коммитом (exit ≠ 0 при ошибках).
- ` + "`onebase describe`" + ` — структура этой базы в JSON.
- ` + "`onebase ai-guide --output AGENTS.md`" + ` — обновить руководство.
`

type builtinGroup struct {
	title string
	names []string
}

// builtinGroups группирует имена встроенных функций по источнику, используя
// экспортированные фабрики интерпретатора. core = всё известное минус группы.
func builtinGroups() []builtinGroup {
	collect := func(m map[string]any) []string {
		var out []string
		for k := range m {
			if strings.HasPrefix(k, "__factory_") {
				continue
			}
			out = append(out, strings.ToLower(k))
		}
		sort.Strings(out)
		return out
	}
	groups := []builtinGroup{
		{"HTTP", collect(interpreter.NewHTTPFunctions())},
		{"Email", collect(interpreter.NewEmailFunctions(nil))},
		{"Транзакции", collect(interpreter.NewTxFunctions(nil, nil))},
		{"Диаграммы", collect(interpreter.NewChartFunctions())},
		{"ТабличныйДокумент", collect(interpreter.NewSpreadsheetFunctions())},
		{"Файлы", collect(interpreter.NewFileFunctions())},
	}
	grouped := map[string]bool{}
	for _, g := range groups {
		for _, n := range g.names {
			grouped[n] = true
		}
	}
	var core []string
	for n := range interpreter.KnownBuiltinNames() {
		if !grouped[n] {
			core = append(core, n)
		}
	}
	sort.Strings(core)
	return append([]builtinGroup{{"Базовые", core}}, groups...)
}
