package cli

import (
	"os"
	"time"

	"github.com/ivantit66/onebase/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Запустить MCP stdio server для AI-инструментов разработчика",
	Long: `Запускает Model Context Protocol server поверх существующих CLI-команд
OneBase. По умолчанию доступны только read-only tools/resources. Мутирующие
tools нужно включать явно: точечными --allow-*-write флагами или общим
--allow-write для обратной совместимости.`,
	RunE:          runMCP,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	addBaseFlags(mcpCmd)
	mcpCmd.Flags().Bool("allow-write", false, "разрешить все mutating MCP tools")
	mcpCmd.Flags().Bool("allow-fmt-write", false, "разрешить MCP tool fmt_write")
	mcpCmd.Flags().Bool("allow-refactor-write", false, "разрешить MCP tool refactor_write")
	mcpCmd.Flags().Bool("allow-config-rollback", false, "разрешить MCP tool config_rollback")
	mcpCmd.Flags().Bool("allow-procrun", false, "разрешить MCP tool procrun")
	mcpCmd.Flags().Duration("timeout", 30*time.Second, "таймаут одного tool/resource вызова")
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, _ []string) error {
	id, _ := cmd.Flags().GetString("id")
	projectDir, _ := cmd.Flags().GetString("project")
	sqlitePath, _ := cmd.Flags().GetString("sqlite")
	dsn, _ := cmd.Flags().GetString("db")
	allowWrite, _ := cmd.Flags().GetBool("allow-write")
	allowFmtWrite, _ := cmd.Flags().GetBool("allow-fmt-write")
	allowRefactorWrite, _ := cmd.Flags().GetBool("allow-refactor-write")
	allowConfigRollback, _ := cmd.Flags().GetBool("allow-config-rollback")
	allowProcrun, _ := cmd.Flags().GetBool("allow-procrun")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	srv, err := mcp.New(mcp.Config{
		ID:                  id,
		Project:             projectDir,
		SQLitePath:          sqlitePath,
		DSN:                 dsn,
		AllowWrite:          allowWrite,
		AllowFmtWrite:       allowFmtWrite,
		AllowRefactorWrite:  allowRefactorWrite,
		AllowConfigRollback: allowConfigRollback,
		AllowProcrun:        allowProcrun,
		Timeout:             timeout,
	})
	if err != nil {
		return err
	}
	return srv.Serve(os.Stdin, os.Stdout)
}
