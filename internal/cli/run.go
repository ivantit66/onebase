package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/ivantit66/onebase/internal/api"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/backup"
	"github.com/ivantit66/onebase/internal/configdb"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/launcher"
	"github.com/ivantit66/onebase/internal/mailer"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/scheduler"
	"github.com/ivantit66/onebase/internal/storage"
	"github.com/ivantit66/onebase/internal/ui"
	"github.com/ivantit66/onebase/internal/version"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the server in production mode",
	RunE:  runServer,
}

func init() {
	runCmd.Flags().String("id", "", "run a base from the ibases registry by ID")
	runCmd.Flags().String("project", ".", "path to project directory")
	runCmd.Flags().String("db", "", "PostgreSQL DSN (overrides DATABASE_URL env)")
	runCmd.Flags().String("sqlite", "", "path to SQLite database file (alternative to --db)")
	runCmd.Flags().Int("port", 8080, "HTTP server port")
	runCmd.Flags().String("config-source", "file", "configuration source: file or database")
}

func runServer(cmd *cobra.Command, _ []string) error {
	baseID, _ := cmd.Flags().GetString("id")

	var dir, dsn, configSource, sqlitePath, dbType string
	var port int

	// If --id given, load settings from the ibases registry
	if baseID != "" {
		store, err := launcher.NewStore()
		if err != nil {
			return fmt.Errorf("ibases store: %w", err)
		}
		base, err := store.Get(baseID)
		if err != nil {
			return fmt.Errorf("база не найдена: %w\nИспользуйте 'onebase ibases list' для просмотра зарегистрированных баз", err)
		}
		dir = base.Path
		dsn = base.DB
		port = base.Port
		configSource = base.ConfigSource
		dbType = base.DBType
		sqlitePath = base.DBPath
		fmt.Fprintf(os.Stdout, "Запуск базы: %s\n", base.Name)
	} else {
		dir, _ = cmd.Flags().GetString("project")
		dsn = dsnFromFlags(cmd)
		sqlitePath, _ = cmd.Flags().GetString("sqlite")
		port, _ = cmd.Flags().GetInt("port")
		configSource, _ = cmd.Flags().GetString("config-source")
		if sqlitePath != "" {
			dbType = "sqlite"
		}
	}

	ctx := context.Background()
	var (
		db  *storage.DB
		err error
	)
	if dbType == "sqlite" {
		if sqlitePath == "" {
			return fmt.Errorf("--sqlite path is required for sqlite databases")
		}
		db, err = storage.ConnectSQLite(ctx, sqlitePath)
	} else {
		db, err = storage.Connect(ctx, dsn)
	}
	if err != nil {
		return err
	}
	defer db.Close()

	authRepo := auth.NewRepo(db)
	if err := authRepo.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("auth schema: %w", err)
	}

	var proj *project.Project
	if configSource == "database" {
		cfgRepo := configdb.New(db)
		if err := cfgRepo.EnsureSchema(ctx); err != nil {
			return fmt.Errorf("configdb schema: %w", err)
		}
		if err := cfgRepo.MigrateContent(ctx); err != nil {
			return fmt.Errorf("configdb migrate content: %w", err)
		}
		proj, err = project.LoadFromDB(ctx, cfgRepo)
	} else {
		proj, err = project.Load(dir)
	}
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	defer proj.Close()

	if err := db.Migrate(ctx, proj.Entities); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := db.MigrateRegisters(ctx, proj.Registers); err != nil {
		return fmt.Errorf("migrate registers: %w", err)
	}
	if err := db.MigrateInfoRegisters(ctx, proj.InfoRegisters); err != nil {
		return fmt.Errorf("migrate info registers: %w", err)
	}
	if err := db.MigrateConstants(ctx, proj.Constants); err != nil {
		return fmt.Errorf("migrate constants: %w", err)
	}
	if err := db.EnsureAuditSchema(ctx); err != nil {
		return fmt.Errorf("audit schema: %w", err)
	}

	// Sync roles from YAML
	if roles, err2 := auth.LoadRolesYAML(proj.Dir + "/roles"); err2 == nil && len(roles) > 0 {
		_ = authRepo.SyncRoles(ctx, roles)
	}

	if err := db.EnsureAccountsTable(ctx); err != nil {
		return fmt.Errorf("accounts table: %w", err)
	}
	if err := db.SyncAccounts(ctx, proj.ChartsOfAccounts); err != nil {
		return fmt.Errorf("sync accounts: %w", err)
	}
	if err := db.MigrateAccountRegisters(ctx, proj.AccountRegisters); err != nil {
		return fmt.Errorf("migrate account registers: %w", err)
	}

	reg := runtime.NewRegistry()
	reg.Load(proj.Entities, proj.Programs, proj.Registers, proj.InfoRegisters, proj.Enums, proj.Constants, proj.Reports, proj.PrintForms)
	reg.LoadDSLPrintForms(proj.DSLPrintForms)
	reg.LoadModules(proj.Modules)
	reg.LoadProcessors(proj.Processors)
	reg.LoadSubsystems(proj.Subsystems)
	reg.LoadJournals(proj.Journals)
	reg.LoadAccountRegisters(proj.AccountRegisters, proj.ChartsOfAccounts)
	reg.LoadWidgets(proj.Widgets)
	reg.LoadHomePage(proj.HomePage)

	appCfg, _ := project.LoadConfig(proj.Dir)
	uiCfg := ui.Config{
		DSN:         dsn,
		PlatVersion: version.String(),
	}
	if appCfg != nil {
		uiCfg.AppName = appCfg.Name
		uiCfg.AppVersion = appCfg.Version
		if appCfg.Logo != "" {
			uiCfg.Logo = filepath.Join(proj.Dir, appCfg.Logo)
		}
		if appCfg.Attachments != nil && appCfg.Attachments.MaxFileSizeMB > 0 {
			uiCfg.MaxFileSizeMB = appCfg.Attachments.MaxFileSizeMB
		}
	}

	interp := interpreter.New()
	interp.LookupProc = reg.GetModuleProc

	if err := db.EnsureScheduledRunsTable(ctx); err != nil {
		return fmt.Errorf("scheduled runs schema: %w", err)
	}
	if err := db.EnsureAttachmentTable(ctx); err != nil {
		return fmt.Errorf("attachments table: %w", err)
	}
	sched := scheduler.New(db, reg, interp)
	if err := sched.LoadJobs(proj.ScheduledJobs); err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}

	if appCfg != nil && appCfg.Demo != nil && appCfg.Demo.Enabled {
		uiCfg.DemoMode = true
		msg := appCfg.Demo.Message
		if msg == "" {
			msg = "Данные сбрасываются каждую ночь в 02:00"
		}
		uiCfg.DemoMessage = msg

		schedule := appCfg.Demo.ResetSchedule
		if schedule == "" {
			schedule = "0 2 * * *"
		}
		backupPath := ""
		if appCfg.Demo.ResetBackup != "" {
			backupPath = filepath.Join(dir, appCfg.Demo.ResetBackup)
		}
		dbRef := db // capture
		if err := sched.RegisterGoJob("DemoReset", "Сброс демо-данных", schedule, func(ctx context.Context) error {
			_, err := backup.DemoReset(ctx, dbRef, backupPath)
			return err
		}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: demo reset job: %v\n", err)
		}
	}

	if appCfg != nil && appCfg.Email != nil {
		m := mailer.New(mailer.Config{
			SMTPHost:    appCfg.Email.SMTPHost,
			SMTPPort:    appCfg.Email.SMTPPort,
			SMTPUser:    appCfg.Email.SMTPUser,
			SMTPPass:    appCfg.Email.SMTPPass,
			FromName:    appCfg.Email.FromName,
			FromAddress: appCfg.Email.FromAddress,
		})
		uiCfg.Mailer = m
		sched.SetMailer(m)
	}

	srv := api.New(reg, db, interp, authRepo, port, uiCfg, sched)

	schedCtx, schedCancel := context.WithCancel(ctx)
	defer schedCancel()
	go sched.Start(schedCtx)

	fmt.Fprintf(os.Stdout, "onebase running on :%d\n", port)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "server error:", err)
		}
	}()
	<-quit
	schedCancel()
	return srv.Shutdown(ctx)
}
