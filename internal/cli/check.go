package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ivantit66/onebase/internal/configcheck"
	"github.com/ivantit66/onebase/internal/project"
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

	issues := configcheck.CheckDir(bc.Dir)
	// project.Load даёт кросс-ссылочные ошибки и Project для компиляции запросов.
	if proj, lerr := project.Load(bc.Dir); lerr == nil {
		issues = append(issues, configcheck.CheckQueries(proj)...)
		proj.Close()
	} else if !configcheck.AlreadyReported(issues, lerr.Error()) {
		issues = append(issues, configcheck.Issue{Message: "Project.Load: " + lerr.Error()})
	}
	res := configcheck.NewResult(issues)

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

func printIssuesText(res configcheck.Result) {
	if res.OK {
		fmt.Fprintln(os.Stdout, "OK: ошибок не найдено")
		return
	}
	for _, is := range res.Issues {
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
	fmt.Fprintf(os.Stderr, "\nНайдено ошибок: %d\n", res.Total)
}
