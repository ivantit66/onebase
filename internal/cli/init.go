package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/ivantit66/onebase/internal/project"
)

var (
	initTemplate     string
	initListTemplate bool
)

var initCmd = &cobra.Command{
	Use:   "init [directory]",
	Short: "Scaffold a new onebase project",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runInit,
}

func init() {
	initCmd.Flags().StringVar(&initTemplate, "template", "", "use a built-in template (tasks, crm, warehouse, finance)")
	initCmd.Flags().BoolVar(&initListTemplate, "list-templates", false, "list available templates and exit")
}

func runInit(cmd *cobra.Command, args []string) error {
	if initListTemplate {
		fmt.Fprintln(os.Stdout, "Available templates:")
		for _, t := range project.ListTemplates() {
			fmt.Fprintf(os.Stdout, "  %-12s %s\n", t.Name, t.Description)
		}
		return nil
	}

	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	name := filepath.Base(dir)
	if dir == "." {
		if wd, err := os.Getwd(); err == nil {
			name = filepath.Base(wd)
		} else {
			name = "myapp"
		}
	}

	if initTemplate != "" {
		if err := project.ApplyTemplate(initTemplate, dir, name); err != nil {
			return err
		}
		writeAIGuide(dir)
		fmt.Fprintf(os.Stdout, "project initialized from template %q in %s\n", initTemplate, dir)
		return nil
	}

	if err := project.Scaffold(dir, name); err != nil {
		return err
	}
	writeAIGuide(dir)
	fmt.Fprintf(os.Stdout, "project initialized in %s\n", dir)
	return nil
}

// writeAIGuide кладёт AGENTS.md в новую конфигурацию, чтобы ИИ разработчика
// сразу знал структуру, рабочий цикл и встроенные функции (best-effort).
func writeAIGuide(dir string) {
	_ = os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(generateAIGuide()), 0o644)
}
