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
	"time"

	"github.com/ivantit66/onebase/internal/api"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/backup"
	"github.com/ivantit66/onebase/internal/configdb"
	"github.com/ivantit66/onebase/internal/devserver"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/extform"
	"github.com/ivantit66/onebase/internal/i18n"
	"github.com/ivantit66/onebase/internal/launcher"
	oblog "github.com/ivantit66/onebase/internal/logging"
	"github.com/ivantit66/onebase/internal/mailer"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/scheduler"
	"github.com/ivantit66/onebase/internal/storage"
	"github.com/ivantit66/onebase/internal/ui"
	"github.com/ivantit66/onebase/internal/version"
	"github.com/ivantit66/onebase/internal/webhook"
	"github.com/spf13/cobra"
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
	// Secure-by-default (план 53): наружу сервер выставляется только явно.
	runCmd.Flags().String("host", "127.0.0.1", "интерфейс прослушивания (0.0.0.0 — все интерфейсы)")
	runCmd.Flags().String("config-source", "file", "configuration source: file or database")
	// hot reload .os/.yaml без перезапуска. По умолчанию off,
	// для прода обычно не нужен. Включается флагом --watch.
	runCmd.Flags().Bool("watch", false, "reload project metadata when files change (.os/.yaml)")
	// Демо-режим через флаги — работает независимо от источника конфигурации.
	// Удобно для --config-source database, где app.yaml не лежит файлом и
	// блок demo: некуда вписать. Флаги имеют приоритет над app.yaml.
	runCmd.Flags().String("demo-backup", "", "путь к .obz; включает демо-режим (сброс данных по расписанию)")
	runCmd.Flags().String("demo-schedule", "", "cron-расписание сброса демо-данных (по умолчанию '0 2 * * *')")
	runCmd.Flags().String("demo-message", "", "текст баннера демо-режима")
}

func runServer(cmd *cobra.Command, _ []string) error {
	runLog := oblog.Component("cli.run")
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
	reg.Load(runtime.LoadOptions{
		Entities:        proj.Entities,
		Programs:        proj.Programs,
		ManagerPrograms: proj.ManagerPrograms,
		ServicePrograms: proj.ServicePrograms,
		PagePrograms:    proj.PagePrograms,
		Registers:       proj.Registers,
		InfoRegs:        proj.InfoRegisters,
		Enums:           proj.Enums,
		Constants:       proj.Constants,
		Reports:         proj.Reports,
		PrintForms:      proj.PrintForms,
	})
	reg.LoadDSLPrintForms(proj.DSLPrintForms)
	reg.LoadLayoutForms(proj.LayoutForms)
	reg.LoadModules(proj.Modules)
	reg.LoadProcessors(proj.Processors)
	reg.LoadHTTPServices(proj.HTTPServices)
	reg.LoadPages(proj.Pages)
	reg.LoadSubsystems(proj.Subsystems)
	reg.LoadJournals(proj.Journals)
	reg.LoadAccountRegisters(proj.AccountRegisters, proj.ChartsOfAccounts)
	reg.LoadWidgets(proj.Widgets)
	reg.LoadHomePage(proj.HomePage)

	// Внешний контур: печатные формы и отчёты из БД (вне конфигурации проекта).
	extRepo := extform.New(db)
	if err := extRepo.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("extform schema: %w", err)
	}
	if extForms, extLayouts, err := extRepo.LoadEnabledPrintForms(ctx); err != nil {
		runLog.Warn("external print forms load failed", "err", err)
	} else {
		reg.SetExternalPrintForms(extForms)
		reg.SetExternalLayoutForms(extLayouts)
	}
	extRepRepo := extform.NewReports(db)
	if err := extRepRepo.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("extform reports schema: %w", err)
	}
	if extReps, err := extRepRepo.LoadEnabledReports(ctx); err != nil {
		runLog.Warn("external reports load failed", "err", err)
	} else {
		reg.SetExternalReports(extReps)
	}
	extProcRepo := extform.NewProcessors(db)
	if err := extProcRepo.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("extform processors schema: %w", err)
	}
	if extProcs, extPrograms, err := extProcRepo.LoadEnabled(ctx); err != nil {
		runLog.Warn("external processors load failed", "err", err)
	} else {
		reg.SetExternalProcessors(extProcs, extPrograms)
	}

	appCfg, _ := project.LoadConfig(proj.Dir)
	// app.yaml может задавать конфиг ИИ-помощника (секция llm, ключи через
	// ${env:...}) — применяем его к базе при старте. Это деплоит ИИ вместе с
	// конфигурацией: таблица _settings не входит в .obz, поэтому для демо/прод
	// это способ донести конфиг без утечки ключа в выгрузку.
	if appCfg != nil && appCfg.LLM != nil {
		if err := db.SaveLLMConfig(ctx, *appCfg.LLM); err != nil {
			runLog.Warn("apply app llm config failed", "err", err)
		}
	}
	uiCfg := ui.Config{
		DSN:         dsn,
		PlatVersion: version.String(),
		PlatAuthor:  version.Author,
		PlatLicense: version.License,
	}
	if appCfg != nil {
		uiCfg.AppName = appCfg.Name
		uiCfg.AppVersion = appCfg.Version
		uiCfg.AppAuthor = appCfg.Author
		uiCfg.AppCopyright = appCfg.Copyright
		uiCfg.AppLicense = appCfg.License
		uiCfg.Lang = appCfg.Lang
		if appCfg.Logo != "" {
			uiCfg.Logo = filepath.Join(proj.Dir, appCfg.Logo)
		}
		if appCfg.Attachments != nil && appCfg.Attachments.MaxFileSizeMB > 0 {
			uiCfg.MaxFileSizeMB = appCfg.Attachments.MaxFileSizeMB
		}
	}

	bundle, err := i18n.Load(i18n.EmbeddedLocales, filepath.Join(proj.Dir, "locales"))
	if err != nil {
		runLog.Warn("i18n load failed", "err", err)
	}
	uiCfg.Bundle = bundle

	interp := interpreter.New()
	interp.LookupProc = reg.GetModuleProc
	interp.LookupSiblingProc = reg.GetSiblingProc
	interp.LookupModuleProc = reg.GetModuleNamespacedProc

	if err := db.EnsureScheduledRunsTable(ctx); err != nil {
		return fmt.Errorf("scheduled runs schema: %w", err)
	}
	if err := db.EnsureAttachmentTable(ctx); err != nil {
		return fmt.Errorf("attachments table: %w", err)
	}
	if err := db.EnsureBlobTable(ctx); err != nil {
		return fmt.Errorf("blobs table: %w", err)
	}
	sched := scheduler.New(db, reg, interp)
	if err := sched.LoadJobs(proj.ScheduledJobs); err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}

	// Флаги --demo-* включают демо-режим независимо от источника конфига и
	// имеют приоритет над блоком demo: в app.yaml.
	demoBackupFlag, _ := cmd.Flags().GetString("demo-backup")
	demoScheduleFlag, _ := cmd.Flags().GetString("demo-schedule")
	demoMessageFlag, _ := cmd.Flags().GetString("demo-message")
	if demoBackupFlag != "" || demoScheduleFlag != "" || demoMessageFlag != "" {
		if appCfg == nil {
			appCfg = &project.AppConfig{}
		}
		if appCfg.Demo == nil {
			appCfg.Demo = &project.DemoConfig{}
		}
		appCfg.Demo.Enabled = true
		if demoBackupFlag != "" {
			appCfg.Demo.ResetBackup = demoBackupFlag
		}
		if demoScheduleFlag != "" {
			appCfg.Demo.ResetSchedule = demoScheduleFlag
		}
		if demoMessageFlag != "" {
			appCfg.Demo.Message = demoMessageFlag
		}
	}

	if appCfg != nil && appCfg.Demo != nil && appCfg.Demo.Enabled {
		// защита от случайной активации демо-режима на проде.
		if err := checkDemoEnv(os.Getenv("ONEBASE_ENV")); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "⚠️  ONEBASE: ДЕМО-РЕЖИМ. Данные сбрасываются по расписанию.")

		uiCfg.DemoMode = true
		// Безопасность: в демо-режиме обработки исполняет недоверенный
		// пользователь — ограничиваем файловые builtins каталогом базы,
		// чтобы DSL не мог читать/писать произвольные файлы на сервере.
		interpreter.SetFileSandbox(proj.Dir)
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
			// Абсолютный путь берём как есть; относительный — от каталога проекта.
			// Важно для --config-source database (dir = "."), где иначе абсолютный
			// путь превратился бы в относительный.
			if filepath.IsAbs(appCfg.Demo.ResetBackup) {
				backupPath = appCfg.Demo.ResetBackup
			} else {
				backupPath = filepath.Join(dir, appCfg.Demo.ResetBackup)
			}
		}
		dbRef := db // capture
		if err := sched.RegisterGoJob("DemoReset", "Сброс демо-данных", schedule, func(ctx context.Context) error {
			_, err := backup.DemoReset(ctx, dbRef, backupPath)
			return err
		}); err != nil {
			runLog.Warn("demo reset job registration failed", "err", err)
		}
	}

	if appCfg != nil && appCfg.Backup != nil {
		target := backup.AutoTarget{
			DBType:     dbType,
			DSN:        dsn,
			SQLitePath: sqlitePath,
			ProjectDir: dir,
		}
		if err := backup.RegisterAutoBackup(appCfg.Backup, target, sched); err != nil {
			runLog.Warn("auto backup job registration failed", "err", err)
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

	// Исходящие веб-хуки из app.yaml (план 29): асинхронная отправка с retry,
	// журнал — в _webhook_log.
	if appCfg != nil && len(appCfg.Webhooks) > 0 {
		dbRef := db
		d := webhook.New(appCfg.Webhooks, func(e webhook.LogEntry) {
			dbRef.LogWebhook(context.Background(), storage.WebhookLogEntry{
				Webhook: e.Webhook, Event: e.Event, Entity: e.Entity, RecordID: e.RecordID,
				URL: e.URL, StatusCode: e.StatusCode, Error: e.Error,
				Duration: e.Duration, Attempts: e.Attempts,
			})
		})
		// Предохранитель сети (план 62): хуки уходят только при разрешённой сети.
		d.SetGuard(func() bool { return dbRef.GetNetworkEnabled(context.Background()) })
		uiCfg.Webhooks = d
		fmt.Fprintf(os.Stdout, "веб-хуки: настроено %d\n", len(appCfg.Webhooks))
		if !db.GetNetworkEnabled(ctx) {
			fmt.Fprintln(os.Stdout, "  ⚠ сеть заблокирована предохранителем — хуки не будут отправляться,\n"+
				"    пока не включить «Разрешить сетевые операции» в конфигураторе")
		}
	}

	host, _ := cmd.Flags().GetString("host")
	// Footgun-страж (план 53, анализ §2.7): без пользователей auth выключен
	// целиком (включая консоль кода); слушать в таком виде не-loopback адрес —
	// почти наверняка ошибка оператора.
	if !api.IsLoopbackHost(host) {
		if hasUsers, _ := authRepo.HasUsers(ctx); !hasUsers {
			fmt.Fprintf(os.Stderr, "ПРЕДУПРЕЖДЕНИЕ: сервер слушает %s без настроенных пользователей —\n"+
				"база и консоль кода доступны без аутентификации. Создайте пользователя\n"+
				"или уберите --host (по умолчанию 127.0.0.1).\n", host)
		}
	}

	srv := api.New(reg, db, interp, authRepo, host, port, uiCfg, sched)

	// опциональный hot reload (см. --watch).
	// Перечитываем только метаданные (reg.Load*), миграции не повторяем —
	// они единоразовы и потенциально опасны. Срабатывает только для
	// file-based конфигов (configdb не имеет смысла отслеживать).
	if watchEnabled, _ := cmd.Flags().GetBool("watch"); watchEnabled && configSource == "file" {
		reload := func() {
			newProj, err := project.Load(dir)
			if err != nil {
				runLog.Warn("watch reload failed", "err", err)
				return
			}
			reg.Load(runtime.LoadOptions{
				Entities:        newProj.Entities,
				Programs:        newProj.Programs,
				ManagerPrograms: newProj.ManagerPrograms,
				ServicePrograms: newProj.ServicePrograms,
				PagePrograms:    newProj.PagePrograms,
				Registers:       newProj.Registers,
				InfoRegs:        newProj.InfoRegisters,
				Enums:           newProj.Enums,
				Constants:       newProj.Constants,
				Reports:         newProj.Reports,
				PrintForms:      newProj.PrintForms,
			})
			reg.LoadDSLPrintForms(newProj.DSLPrintForms)
			reg.LoadLayoutForms(newProj.LayoutForms)
			reg.LoadModules(newProj.Modules)
			reg.LoadProcessors(newProj.Processors)
			reg.LoadHTTPServices(newProj.HTTPServices)
			reg.LoadPages(newProj.Pages)
			reg.LoadSubsystems(newProj.Subsystems)
			reg.LoadJournals(newProj.Journals)
			reg.LoadAccountRegisters(newProj.AccountRegisters, newProj.ChartsOfAccounts)
			reg.LoadWidgets(newProj.Widgets)
			reg.LoadHomePage(newProj.HomePage)
			fmt.Fprintln(os.Stdout, "[watch] метаданные перезагружены")
		}
		if err := devserver.Watch(dir, reload); err != nil {
			runLog.Warn("watch init failed", "err", err)
		} else {
			fmt.Fprintf(os.Stdout, "[watch] отслеживаем %s — изменения .yaml/.os подхватятся без рестарта\n", dir)
		}
	}

	schedCtx, schedCancel := context.WithCancel(ctx)
	defer schedCancel()
	schedDone := make(chan struct{})
	go func() {
		defer close(schedDone)
		sched.Start(schedCtx)
	}()

	fmt.Fprintf(os.Stdout, "onebase running on %s:%d\n", host, port)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			runLog.Error("server failed", "err", err)
		}
	}()
	<-quit
	schedCancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	err = srv.Shutdown(shutdownCtx)
	<-schedDone
	return err
}
