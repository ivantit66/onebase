package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/configcheck"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/storage"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Проверить конфигурацию (синтаксис .os, YAML-схема, запросы)",
	Long: `Валидирует конфигурацию без запуска: синтаксис модулей .os, вызовы
неизвестных функций, схему YAML всех объектов и компиляцию запросов
виджетов/отчётов. Выводит проблемы в формате file:line:col: message.
Завершается с ненулевым кодом, если найдены ошибки — пригодно для pre-commit/CI.

Примеры:
  onebase check --project C:\Projects\OneBaseConfs\PuT
  onebase check --id <baseID> --json`,
	RunE:          runCheck,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	addBaseFlags(checkCmd)
	checkCmd.Flags().Bool("json", false, "вывод в JSON")
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, _ []string) error {
	bc, err := resolveBase(cmd)
	if err != nil {
		return err
	}
	defer bc.Cleanup()

	dirIssues, dirWarnings := configcheck.CheckDir(bc.Dir)
	issues := dirIssues
	warnings := dirWarnings
	// project.Load даёт кросс-ссылочные ошибки и Project для компиляции запросов.
	if proj, lerr := project.Load(bc.Dir); lerr == nil {
		issues = append(issues, configcheck.CheckQueries(proj)...)
		issues = append(issues, configcheck.CheckReportComposition(proj)...)
		// Кросс-ссылки между объектами (документы в журналах/подсистемах/ролях,
		// виджеты главной страницы, источник печатной формы). Роли грузятся
		// отдельно — они не часть project.Project.
		roles, _ := auth.LoadRolesYAML(filepath.Join(bc.Dir, "roles"))
		issues = append(issues, configcheck.CheckCrossRefs(proj, roles)...)
		// Неблокирующие предупреждения по макетам v2 (например rowspan в repeat-
		// области может некорректно разрываться по страницам PDF).
		warnings = append(warnings, configcheck.CheckLayoutWarnings(proj)...)
		// HTTP-сервисы (план 61): дубли root_url, наличие обработчиков, auth.
		issues = append(issues, configcheck.CheckHTTPServices(proj)...)
		// Страницы (план 66): наличие обработчика ПриФормировании.
		issues = append(issues, configcheck.CheckPages(proj)...)
		// Коллизии имён таблиц: справочник и документ с одинаковым именем
		// делят одну физическую таблицу lower(имя) (issue #20).
		issues = append(issues, configcheck.CheckNameCollisions(proj)...)
		// п.45: исполняемая валидация запросов против in-memory схемы из
		// метаданных (best-effort — при сбое настройки схемы просто пропускаем,
		// чтобы не ломать обычную проверку компиляции).
		if db, closeDB, derr := buildSchemaDB(proj); derr == nil {
			validate := func(sql string) error { return db.ValidateQuery(context.Background(), sql) }
			issues = append(issues, configcheck.CheckQueriesExecutable(proj, validate)...)
			// Запросы внутри .os-модулей (Запрос.Текст = "...") — компиляция + PREPARE.
			issues = append(issues, configcheck.CheckModuleQueries(proj, validate)...)
			closeDB()
		} else {
			// Схему поднять не удалось — хотя бы проверим компиляцию модульных запросов.
			issues = append(issues, configcheck.CheckModuleQueries(proj, nil)...)
		}
		proj.Close()
	} else if !configcheck.AlreadyReported(issues, lerr.Error()) {
		issues = append(issues, configcheck.Issue{Message: "Project.Load: " + lerr.Error()})
	}
	res := configcheck.NewResult(issues, warnings)

	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res)
	} else {
		printIssuesText(res)
	}

	if !res.OK {
		bc.Cleanup() // defer не выполнится при os.Exit
		os.Exit(1)
	}
	return nil
}

// buildSchemaDB поднимает временную SQLite-базу со схемой из метаданных проекта
// (entity/register/inforeg/constant/accountreg) — для исполняемой валидации
// запросов (п.45). Возвращает базу и closer (закрыть + удалить файл). Файл, а не
// ":memory:", т.к. ConnectSQLite ориентирован на путь; один коннект в пуле
// (SetMaxOpenConns(1)) гарантирует, что миграции и PREPARE видят одну схему.
func buildSchemaDB(proj *project.Project) (*storage.DB, func(), error) {
	ctx := context.Background()
	f, err := os.CreateTemp("", "onebase_check_*.db")
	if err != nil {
		return nil, nil, err
	}
	path := f.Name()
	f.Close()
	db, err := storage.ConnectSQLite(ctx, path)
	if err != nil {
		os.Remove(path)
		return nil, nil, err
	}
	closer := func() { db.Close(); os.Remove(path) }
	steps := []func() error{
		func() error { return db.Migrate(ctx, proj.Entities) },
		func() error { return db.MigrateRegisters(ctx, proj.Registers) },
		func() error { return db.MigrateInfoRegisters(ctx, proj.InfoRegisters) },
		func() error { return db.MigrateConstants(ctx, proj.Constants) },
		func() error { return db.MigrateAccountRegisters(ctx, proj.AccountRegisters) },
		// План счетов (_accounts) — отдельная системная таблица, на которую
		// JOIN-ятся бух-отчёты (оборотно-сальдовая). run/dev/deploy создают её
		// через EnsureAccountsTable; повторяем здесь, иначе исполняемая
		// валидация запросов падает с «no such table: _accounts».
		func() error { return db.EnsureAccountsTable(ctx) },
		func() error { return db.SyncAccounts(ctx, proj.ChartsOfAccounts) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			closer()
			return nil, nil, err
		}
	}
	return db, closer, nil
}

func printIssuesText(res configcheck.Result) {
	printIssueList := func(list []configcheck.Issue) {
		for _, is := range list {
			loc := is.File
			if loc == "" {
				loc = "(конфигурация)"
			}
			if is.Line > 0 {
				loc = fmt.Sprintf("%s:%d:%d", loc, is.Line, is.Column)
			}
			prefix := ""
			if is.Kind != "" {
				prefix = "[" + is.Kind + "] "
			}
			fmt.Fprintf(os.Stdout, "%s: %s%s\n", loc, prefix, is.Message)
		}
	}

	if len(res.Warnings) > 0 {
		fmt.Fprintf(os.Stdout, "Предупреждения:\n")
		printIssueList(res.Warnings)
	}

	if res.OK {
		if len(res.Warnings) > 0 {
			fmt.Fprintf(os.Stdout, "OK: ошибок не найдено (%d предупреждений)\n", len(res.Warnings))
		} else {
			fmt.Fprintln(os.Stdout, "OK: ошибок не найдено")
		}
		return
	}
	printIssueList(res.Issues)
	fmt.Fprintf(os.Stderr, "\nНайдено ошибок: %d\n", res.Total)
}
