package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ivantit66/onebase/internal/configdb"
	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/loader"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/printform"
	"github.com/ivantit66/onebase/internal/processor"
	"github.com/ivantit66/onebase/internal/report"
	"gopkg.in/yaml.v3"
)

type Project struct {
	Dir              string
	Entities         []*metadata.Entity
	Registers        []*metadata.Register
	InfoRegisters    []*metadata.InfoRegister
	Enums            []*metadata.Enum
	Constants        []*metadata.Constant
	Reports          []*report.Report
	PrintForms       []*printform.PrintForm
	DSLPrintForms    []*printform.DSLPrintForm
	Programs         map[string]*ast.Program  // entity name → parsed DSL
	Processors       []*processor.Processor
	Modules          map[string]*ast.Program  // module name → parsed procs
	Subsystems       []*metadata.Subsystem
	Journals         []*metadata.Journal
	ScheduledJobs    []*metadata.ScheduledJob
	ChartsOfAccounts []*metadata.ChartOfAccounts
	AccountRegisters []*metadata.AccountRegister
	Widgets          []*metadata.Widget
	HomePage         *metadata.HomePage
	cleanup          func()
}

// Close releases resources (e.g., temp dirs) associated with this Project.
func (p *Project) Close() {
	if p.cleanup != nil {
		p.cleanup()
	}
}

// EmailConfig holds SMTP configuration from app.yaml section "email".
type EmailConfig struct {
	SMTPHost    string `yaml:"smtp_host"`
	SMTPPort    int    `yaml:"smtp_port"`
	SMTPUser    string `yaml:"smtp_user"`
	SMTPPass    string `yaml:"smtp_password"`
	FromName    string `yaml:"from_name"`
	FromAddress string `yaml:"from_address"`
}

// AttachmentsConfig holds file attachment settings from app.yaml.
type AttachmentsConfig struct {
	MaxFileSizeMB int      `yaml:"max_file_size_mb"`
	AllowedTypes  []string `yaml:"allowed_types"`
}

// DemoConfig holds demo-mode settings from app.yaml section "demo".
type DemoConfig struct {
	Enabled       bool   `yaml:"enabled"`
	ResetBackup   string `yaml:"reset_backup"`   // путь к .obz относительно директории проекта
	ResetSchedule string `yaml:"reset_schedule"`  // cron, по умолчанию "0 2 * * *"
	Message       string `yaml:"message"`         // текст баннера
}

// AppConfig holds the optional config/app.yaml metadata.
type AppConfig struct {
	Name        string             `yaml:"name"`
	Version     string             `yaml:"version"`
	Logo        string             `yaml:"logo,omitempty"`
	Email       *EmailConfig       `yaml:"email,omitempty"`
	Attachments *AttachmentsConfig `yaml:"attachments,omitempty"`
	Demo        *DemoConfig        `yaml:"demo,omitempty"`
}

// LoadConfig reads config/app.yaml from the project directory.
func LoadConfig(dir string) (*AppConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, "config", "app.yaml"))
	if err != nil {
		return &AppConfig{Name: filepath.Base(dir)}, nil
	}
	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadFromDB loads project metadata from the _onebase_config table, writing
// to a temp directory, then calling Load on it.
func LoadFromDB(ctx context.Context, repo *configdb.Repo) (*Project, error) {
	tmpDir, err := os.MkdirTemp("", "onebase-cfg-")
	if err != nil {
		return nil, fmt.Errorf("project: mktempdir: %w", err)
	}

	if err := repo.ExportToDir(ctx, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("project: export from db: %w", err)
	}

	proj, err := Load(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}

	proj.cleanup = func() { os.RemoveAll(tmpDir) }
	return proj, nil
}

func Load(dir string) (*Project, error) {
	p := &Project{
		Dir:      dir,
		Programs: make(map[string]*ast.Program),
		Modules:  make(map[string]*ast.Program),
	}
	if err := p.loadMetadata(); err != nil {
		return nil, err
	}
	if err := metadata.Validate(p.Entities, p.Enums); err != nil {
		return nil, err
	}
	if err := p.loadDSL(); err != nil {
		return nil, err
	}
	if err := p.loadFormModules(); err != nil {
		return nil, err
	}
	if err := p.loadPrintForms(); err != nil {
		return nil, err
	}
	if err := p.loadProcessors(); err != nil {
		return nil, err
	}
	if err := p.loadSubsystems(); err != nil {
		return nil, err
	}
	if err := p.loadJournals(); err != nil {
		return nil, err
	}
	if err := p.loadScheduled(); err != nil {
		return nil, err
	}
	if err := p.loadAccounts(); err != nil {
		return nil, err
	}
	if err := p.loadAccountRegs(); err != nil {
		return nil, err
	}
	if err := p.loadWidgets(); err != nil {
		return nil, err
	}
	if err := p.loadHomePage(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Project) loadWidgets() error {
	widgets, err := metadata.LoadWidgetDir(filepath.Join(p.Dir, "widgets"))
	if err != nil {
		return fmt.Errorf("project: load widgets: %w", err)
	}
	p.Widgets = widgets
	return nil
}

func (p *Project) loadHomePage() error {
	hp, err := metadata.LoadHomePage(filepath.Join(p.Dir, "config", "home_page.yaml"))
	if err != nil {
		return fmt.Errorf("project: load home_page: %w", err)
	}
	p.HomePage = hp
	return nil
}

func (p *Project) loadProcessors() error {
	procs, err := processor.LoadDir(filepath.Join(p.Dir, "processors"))
	if err != nil {
		return fmt.Errorf("project: load processors: %w", err)
	}
	p.Processors = procs
	return nil
}

func (p *Project) loadSubsystems() error {
	subs, err := metadata.LoadSubsystemDir(filepath.Join(p.Dir, "subsystems"))
	if err != nil {
		return fmt.Errorf("project: load subsystems: %w", err)
	}
	p.Subsystems = subs
	return nil
}

func (p *Project) loadJournals() error {
	journals, err := metadata.LoadJournalDir(filepath.Join(p.Dir, "journals"))
	if err != nil {
		return fmt.Errorf("project: load journals: %w", err)
	}
	p.Journals = journals
	return nil
}

func (p *Project) loadScheduled() error {
	jobs, err := metadata.LoadScheduledDir(filepath.Join(p.Dir, "scheduled"))
	if err != nil {
		return fmt.Errorf("project: load scheduled: %w", err)
	}
	p.ScheduledJobs = jobs
	return nil
}

func (p *Project) loadAccounts() error {
	charts, err := metadata.LoadChartOfAccountsDir(filepath.Join(p.Dir, "accounts"))
	if err != nil {
		return fmt.Errorf("project: load accounts: %w", err)
	}
	p.ChartsOfAccounts = charts
	return nil
}

func (p *Project) loadAccountRegs() error {
	regs, err := metadata.LoadAccountRegisterDir(filepath.Join(p.Dir, "accountregs"))
	if err != nil {
		return fmt.Errorf("project: load account registers: %w", err)
	}
	p.AccountRegisters = regs
	return nil
}

func (p *Project) loadPrintForms() error {
	forms, dslForms, err := printform.LoadDir(filepath.Join(p.Dir, "printforms"))
	if err != nil {
		return fmt.Errorf("project: load printforms: %w", err)
	}
	p.PrintForms = forms
	p.DSLPrintForms = dslForms
	return nil
}

func (p *Project) loadMetadata() error {
	type entry struct {
		subdir string
		kind   metadata.Kind
	}
	for _, e := range []entry{
		{"catalogs", metadata.KindCatalog},
		{"documents", metadata.KindDocument},
	} {
		dir := filepath.Join(p.Dir, e.subdir)
		items, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("readdir %s: %w", dir, err)
		}
		for _, item := range items {
			if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
				continue
			}
			ent, err := metadata.LoadFile(filepath.Join(dir, item.Name()), e.kind)
			if err != nil {
				return err
			}
			p.Entities = append(p.Entities, ent)
		}
	}
	// load registers
	regDir := filepath.Join(p.Dir, "registers")
	items, err := os.ReadDir(regDir)
	if err == nil {
		for _, item := range items {
			if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
				continue
			}
			reg, err := metadata.LoadRegisterFile(filepath.Join(regDir, item.Name()))
			if err != nil {
				return err
			}
			p.Registers = append(p.Registers, reg)
		}
	}
	// load info registers
	irDir := filepath.Join(p.Dir, "inforegs")
	irItems, err := os.ReadDir(irDir)
	if err == nil {
		for _, item := range irItems {
			if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
				continue
			}
			ir, err := metadata.LoadInfoRegisterFile(filepath.Join(irDir, item.Name()))
			if err != nil {
				return err
			}
			p.InfoRegisters = append(p.InfoRegisters, ir)
		}
	}
	// load enums
	enumDir := filepath.Join(p.Dir, "enums")
	enumItems, err := os.ReadDir(enumDir)
	if err == nil {
		for _, item := range enumItems {
			if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
				continue
			}
			e, err := metadata.LoadEnumFile(filepath.Join(enumDir, item.Name()))
			if err != nil {
				return err
			}
			p.Enums = append(p.Enums, e)
		}
	}
	// load constants (all .yaml files from constants/)
	constDir := filepath.Join(p.Dir, "constants")
	constItems, err := os.ReadDir(constDir)
	if err == nil {
		for _, item := range constItems {
			if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
				continue
			}
			consts, err := metadata.LoadConstantsFile(filepath.Join(constDir, item.Name()))
			if err != nil {
				return err
			}
			p.Constants = append(p.Constants, consts...)
		}
	}
	// load reports
	repDir := filepath.Join(p.Dir, "reports")
	repItems, err := os.ReadDir(repDir)
	if err == nil {
		for _, item := range repItems {
			if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
				continue
			}
			rep, err := report.LoadFile(filepath.Join(repDir, item.Name()))
			if err != nil {
				return err
			}
			p.Reports = append(p.Reports, rep)
		}
	}
	return nil
}

func (p *Project) loadDSL() error {
	srcDir := filepath.Join(p.Dir, "src")
	items, err := os.ReadDir(srcDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("readdir %s: %w", srcDir, err)
	}
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".os") {
			continue
		}
		name := item.Name()
		isModule := strings.HasSuffix(name, ".module.os")
		isProc := strings.HasSuffix(name, ".proc.os")
		isPosting := strings.HasSuffix(name, ".posting.os")
		isReport := strings.HasSuffix(name, ".rep.os")

		fullPath := filepath.Join(srcDir, name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return err
		}
		l := lexer.New(string(data), fullPath)
		pr := parser.New(l)
		prog, err := pr.ParseProgram()
		if err != nil {
			return err
		}

		if isModule {
			base := strings.TrimSuffix(name, ".module.os")
			moduleName := fileNameToEntityBase(base)
			p.Modules[moduleName] = prog
			continue
		}

		if isProc {
			base := strings.TrimSuffix(name, ".proc.os")
			entityName := fileNameToEntityBase(base)
			p.Programs[entityName] = prog
			continue
		}
		if isReport {
			base := strings.TrimSuffix(name, ".rep.os")
			entityName := fileNameToEntityBase(base)
			if actual := p.findReportName(entityName); actual != "" {
				entityName = actual
			}
			p.Programs[entityName] = prog
			continue
		}

		var entityName string
		if isPosting {
			base := strings.TrimSuffix(name, ".posting.os")
			entityName = fileNameToEntityBase(base)
		} else {
			entityName = fileNameToEntity(name)
		}
		if actual := p.findEntityName(entityName); actual != "" {
			entityName = actual
		}
		if isPosting {
			if existing, ok := p.Programs[entityName]; ok {
				existing.Procedures = append(existing.Procedures, prog.Procedures...)
			} else {
				p.Programs[entityName] = prog
			}
		} else {
			p.Programs[entityName] = prog
		}
	}
	return nil
}

func (p *Project) loadFormModules() error {
	srcDir := filepath.Join(p.Dir, "src")
	formLoader := loader.NewFormLoader()

	for _, ent := range p.Entities {
		forms, err := formLoader.LoadEntityForms(srcDir, ent.Name)
		if err != nil {
			return fmt.Errorf("load form modules for %s: %w", ent.Name, err)
		}
		ent.Forms = forms
	}
	return nil
}

// findEntityName returns the canonical entity name matching s case-insensitively.
func (p *Project) findEntityName(s string) string {
	sl := strings.ToLower(s)
	for _, e := range p.Entities {
		if strings.ToLower(e.Name) == sl {
			return e.Name
		}
	}
	return ""
}

func (p *Project) findReportName(s string) string {
	sl := strings.ToLower(s)
	for _, r := range p.Reports {
		if strings.ToLower(r.Name) == sl {
			return r.Name
		}
	}
	return ""
}

// fileNameToEntity converts "invoice.os" → "Invoice", "счёт.os" → "Счёт".
func fileNameToEntity(name string) string {
	return fileNameToEntityBase(strings.TrimSuffix(name, ".os"))
}

// fileNameToEntityBase capitalises the first rune of a bare name (no extension).
func fileNameToEntityBase(base string) string {
	if base == "" {
		return base
	}
	r, size := utf8.DecodeRuneInString(base)
	return string(unicode.ToUpper(r)) + base[size:]
}
