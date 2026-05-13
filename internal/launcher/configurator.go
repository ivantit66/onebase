package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/configdb"
	"github.com/ivantit66/onebase/internal/converter"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/storage"
	"github.com/ivantit66/onebase/internal/version"
	"gopkg.in/yaml.v3"
)

// ── YAML save structs ────────────────────────────────────────────────────────

type saveField struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type saveTP struct {
	Name   string      `yaml:"name"`
	Fields []saveField `yaml:"fields"`
}

type saveEntity struct {
	Name       string      `yaml:"name"`
	Posting    bool        `yaml:"posting,omitempty"`
	Fields     []saveField `yaml:"fields"`
	TableParts []saveTP    `yaml:"tableparts,omitempty"`
}

type saveRegister struct {
	Name       string      `yaml:"name"`
	Dimensions []saveField `yaml:"dimensions,omitempty"`
	Resources  []saveField `yaml:"resources,omitempty"`
	Attributes []saveField `yaml:"attributes,omitempty"`
}

// ── view types ────────────────────────────────────────────────────────────────

type cfgField struct {
	Name           string
	Type           string
	RefEntity      string
	EnumName       string
	FormListHidden bool
	FormItemHidden bool
}

type cfgEnum struct {
	Name   string
	Values []string
}

type cfgConstant struct {
	Name      string
	Type      string
	RefEntity string
	Default   string
	Label     string
}

type cfgTablePart struct {
	Name   string
	Fields []cfgField
}

type cfgEntity struct {
	Name             string
	Kind             string // "Справочник" / "Документ"
	Posting          bool
	Fields           []cfgField
	TableParts       []cfgTablePart
	Source           string // raw .os content (object module)
	PostingSource    string // raw .posting.os content (ОбработкаПроведения)
	LinkedPrintForms []cfgPrintForm
}

type cfgRegister struct {
	Name       string
	Dimensions []cfgField
	Resources  []cfgField
	Attributes []cfgField
}

type cfgParam struct {
	Name  string
	Type  string
	Label string
}

type cfgReport struct {
	Name   string
	Title  string
	Query  string
	Params []cfgParam
}

type cfgModule struct {
	Name   string
	Source string
}

type cfgProcessor struct {
	Name   string
	Title  string
	Source string
	Params []cfgParam
}

type cfgPrintForm struct {
	Name     string
	Document string
	Source   string
	FileName string
}

type cfgDSLPrintForm struct {
	Name          string
	Document      string
	Source        string
	FileName      string
	HasLayout     bool
	LayoutYAML    string
	LayoutPreview template.HTML
}

type cfgInfoRegister struct {
	Name       string
	Periodic   bool
	Dimensions []cfgField
	Resources  []cfgField
}

type cfgSubsystem struct {
	Name     string
	Title    string
	Icon     string
	Order    int
	Contents metadata.SubsystemContents
}

type configuratorData struct {
	Base       *Base
	AppName    string
	AppVersion string
	DSNMasked  string
	Tab        string // "tree" | "convert" | "files"
	Entities  []cfgEntity
	Catalogs  []cfgEntity
	Docs      []cfgEntity
	Registers []cfgRegister
	InfoRegisters []cfgInfoRegister
	Enums     []cfgEnum
	Constants []cfgConstant
	Reports   []cfgReport
	Modules    []cfgModule
	Processors []cfgProcessor
	PrintForms    []cfgPrintForm
	DSLPrintForms []cfgDSLPrintForm
	Subsystems []cfgSubsystem
	Error     string
	// all entity names for reference picker
	AllEntityNames []string
	// query builder schema (JSON for inline query builder)
	QBSchema template.JS
	// converter
	ConvertSrcDir  string
	ConvertResult  string
	ConvertApplied bool
	// module save
	ModuleSaved       bool
	ModuleSavedEntity string
	// fields save
	FieldsSaved       bool
	FieldsSavedEntity string
	// platform version
	PlatformVer    string
	UIServerURL    string
	BackupMessage  string
	BackupDir      string
	BackupFiles    []backupFile
	BackupSettings backupSettings
	// session token for passing to UI server (bootstrap auth)
	SessionToken string
	// IsRunning: процесс базы запущен сейчас
	IsRunning bool
	// ConfigDirty: на диске есть изменения конфигурации новее, чем запуск базы
	// — пользователю нужно перезапустить базу, чтобы применить.
	ConfigDirty bool
}

type backupFile struct {
	Name string
	Size string
	Date string
}

type backupSettings struct {
	Enabled   bool
	Schedule  string
	KeepLast  int
	Directory string
}

// ── handlers ──────────────────────────────────────────────────────────────────

func (h *handler) configuratorPage(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "tree"
	}
	data := h.loadCfgData(r.Context(), b, tab)
	if cookie, cerr := r.Cookie("onebase_session"); cerr == nil {
		data.SessionToken = cookie.Value
	}
	renderCfg(w, data)
}

func (h *handler) configuratorConvert(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	srcDir := strings.TrimSpace(r.FormValue("src_dir"))
	apply := r.FormValue("apply") == "1"

	data := h.loadCfgData(r.Context(), b, "convert")
	data.ConvertSrcDir = srcDir

	if srcDir == "" {
		data.Error = "Укажите путь к папке конфигурации 1С"
		renderCfg(w, data)
		return
	}

	outDir, err := workspacePath(b.ID + "-convert")
	if err != nil {
		data.Error = err.Error()
		renderCfg(w, data)
		return
	}
	// clean previous conversion
	os.RemoveAll(outDir)

	rep, err := converter.Convert(converter.Options{SourceDir: srcDir, OutDir: outDir})
	if err != nil {
		data.Error = "Ошибка конвертации: " + err.Error()
		renderCfg(w, data)
		return
	}
	data.ConvertResult = rep.String()

	if apply {
		if b.ConfigSource == "database" {
			db, cerr := storage.Connect(r.Context(), b.DB)
			if cerr != nil {
				data.Error = "Ошибка подключения к БД: " + cerr.Error()
				renderCfg(w, data)
				return
			}
			defer db.Close()
			repo := configdb.New(db.Pool())
			repo.EnsureSchema(r.Context())
			if cerr := repo.ImportFromDir(r.Context(), outDir); cerr != nil {
				data.Error = "Ошибка импорта: " + cerr.Error()
				renderCfg(w, data)
				return
			}
		} else {
			// file mode — copy files into base path
			if cerr := copyDir(outDir, b.Path); cerr != nil {
				data.Error = "Ошибка копирования: " + cerr.Error()
				renderCfg(w, data)
				return
			}
		}
		data.ConvertApplied = true
		// reload tree with new data
		fresh := h.loadCfgData(r.Context(), b, "convert")
		fresh.ConvertSrcDir = srcDir
		fresh.ConvertResult = data.ConvertResult
		fresh.ConvertApplied = true
		data = fresh
	}

	renderCfg(w, data)
}

// ── data loading ──────────────────────────────────────────────────────────────

// configDirtyAfter возвращает true, если в rootDir есть .os/.yaml/.yml файл
// с mtime новее threshold. Используется для отображения «звёздочки» в дереве
// метаданных — конфигурация на диске изменилась с момента запуска базы.
func configDirtyAfter(rootDir string, threshold time.Time) bool {
	dirty := false
	filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if base == "backups" || base == ".git" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".os" && ext != ".yaml" && ext != ".yml" {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.ModTime().After(threshold) {
			dirty = true
			return filepath.SkipAll
		}
		return nil
	})
	return dirty
}

func (h *handler) loadCfgData(ctx context.Context, b *Base, tab string) *configuratorData {
	data := &configuratorData{Base: b, Tab: tab, PlatformVer: version.String(), UIServerURL: fmt.Sprintf("http://localhost:%d", b.Port), DSNMasked: maskDSN(b.DB)}

	if startedAt, ok := h.runner.StartedAt(b.ID); ok {
		data.IsRunning = true
		if b.ConfigSource == "file" {
			data.ConfigDirty = configDirtyAfter(b.Path, startedAt)
		}
	}

	var proj *project.Project
	var err error

	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(ctx, b.DB)
		if cerr != nil {
			data.Error = "Нет подключения к БД: " + cerr.Error()
			return data
		}
		defer db.Close()
		repo := configdb.New(db.Pool())
		if cerr := repo.EnsureSchema(ctx); cerr != nil {
			data.Error = cerr.Error()
			return data
		}
		empty, _ := repo.IsEmpty(ctx)
		if empty {
			data.Error = "Конфигурация не загружена в базу данных. Воспользуйтесь вкладкой «Файлы»."
			return data
		}
		proj, err = project.LoadFromDB(ctx, repo)
	} else {
		proj, err = project.Load(b.Path)
	}

	if err != nil {
		data.Error = "Ошибка загрузки конфигурации: " + err.Error()
		return data
	}
	defer proj.Close()

	if appCfg, _ := project.LoadConfig(proj.Dir); appCfg != nil {
		data.AppName = appCfg.Name
		data.AppVersion = appCfg.Version
	}

	sources, postingSources := readOSSources(proj.Dir)

	for _, e := range proj.Entities {
		ev := cfgEntity{
			Name:          e.Name,
			Posting:       e.Posting,
			Source:        sources[strings.ToLower(e.Name)],
			PostingSource: postingSources[strings.ToLower(e.Name)],
		}
		if e.Kind == metadata.KindCatalog {
			ev.Kind = "Справочник"
		} else {
			ev.Kind = "Документ"
		}
		for _, f := range e.Fields {
			ev.Fields = append(ev.Fields, toCfgField(f))
		}
		for _, tp := range e.TableParts {
			tpv := cfgTablePart{Name: tp.Name}
			for _, f := range tp.Fields {
				tpv.Fields = append(tpv.Fields, toCfgField(f))
			}
			ev.TableParts = append(ev.TableParts, tpv)
		}
		// Mark fields hidden based on form config
		if len(e.ListForm) > 0 {
			lfSet := make(map[string]bool, len(e.ListForm))
			for _, n := range e.ListForm {
				lfSet[n] = true
			}
			for i := range ev.Fields {
				if !lfSet[ev.Fields[i].Name] {
					ev.Fields[i].FormListHidden = true
				}
			}
		}
		if len(e.ItemForm) > 0 {
			ifSet := make(map[string]bool, len(e.ItemForm))
			for _, n := range e.ItemForm {
				ifSet[n] = true
			}
			for i := range ev.Fields {
				if !ifSet[ev.Fields[i].Name] {
					ev.Fields[i].FormItemHidden = true
				}
			}
		}
		data.Entities = append(data.Entities, ev)
	}

	sort.Slice(data.Entities, func(i, j int) bool {
		return data.Entities[i].Name < data.Entities[j].Name
	})

	// load print forms and link to entities
	pfSources := readPrintFormSources(proj.Dir)
	pfByDoc := make(map[string][]cfgPrintForm)
	for _, pf := range proj.PrintForms {
		entry := pfSources[pf.Name]
		cpf := cfgPrintForm{Name: pf.Name, Document: pf.Document, Source: entry.source, FileName: entry.filename}
		data.PrintForms = append(data.PrintForms, cpf)
		pfByDoc[strings.ToLower(pf.Document)] = append(pfByDoc[strings.ToLower(pf.Document)], cpf)
	}

		// load DSL print forms (.os)
		for _, df := range proj.DSLPrintForms {
			cpf := cfgDSLPrintForm{
				Name:     df.Name,
				Document: df.Document,
				Source:   df.Source,
				FileName: df.Name + ".os",
			}
			if df.Layout != nil {
				cpf.HasLayout = true
				cpf.LayoutPreview = template.HTML(df.Layout.PreviewHTML())
				if df.LayoutPath != "" {
					if raw, err := os.ReadFile(df.LayoutPath); err == nil {
						cpf.LayoutYAML = string(raw)
					}
				}
			}
			data.DSLPrintForms = append(data.DSLPrintForms, cpf)
		}
	for _, e := range data.Entities {
		data.AllEntityNames = append(data.AllEntityNames, e.Name)
		e.LinkedPrintForms = pfByDoc[strings.ToLower(e.Name)]
		if e.Kind == "Справочник" {
			data.Catalogs = append(data.Catalogs, e)
		} else {
			data.Docs = append(data.Docs, e)
		}
	}

	for _, reg := range proj.Registers {
		rv := cfgRegister{Name: reg.Name}
		for _, f := range reg.Dimensions {
			rv.Dimensions = append(rv.Dimensions, toCfgField(f))
		}
		for _, f := range reg.Resources {
			rv.Resources = append(rv.Resources, toCfgField(f))
		}
		for _, f := range reg.Attributes {
			rv.Attributes = append(rv.Attributes, toCfgField(f))
		}
		data.Registers = append(data.Registers, rv)
	}

	for _, ir := range proj.InfoRegisters {
		rv := cfgInfoRegister{Name: ir.Name, Periodic: ir.Periodic}
		for _, f := range ir.Dimensions {
			rv.Dimensions = append(rv.Dimensions, toCfgField(f))
		}
		for _, f := range ir.Resources {
			rv.Resources = append(rv.Resources, toCfgField(f))
		}
		data.InfoRegisters = append(data.InfoRegisters, rv)
	}

	for _, en := range proj.Enums {
		data.Enums = append(data.Enums, cfgEnum{Name: en.Name, Values: en.Values})
	}

	for _, c := range proj.Constants {
		typ := string(c.Type)
		if c.RefEntity != "" {
			typ = "reference"
		}
		data.Constants = append(data.Constants, cfgConstant{
			Name:      c.Name,
			Type:      typ,
			RefEntity: c.RefEntity,
			Default:   c.Default,
			Label:     c.Label,
		})
	}

	for _, rep := range proj.Reports {
		rv := cfgReport{Name: rep.Name, Title: rep.Title, Query: rep.Query}
		for _, p := range rep.Params {
			rv.Params = append(rv.Params, cfgParam{Name: p.Name, Type: p.Type, Label: p.Label})
		}
		data.Reports = append(data.Reports, rv)
	}

	moduleSources, procSources := readModuleAndProcSources(proj.Dir)

	for name := range proj.Modules {
		data.Modules = append(data.Modules, cfgModule{
			Name:   name,
			Source: moduleSources[strings.ToLower(name)],
		})
	}
	sort.Slice(data.Modules, func(i, j int) bool { return data.Modules[i].Name < data.Modules[j].Name })

	for _, proc := range proj.Processors {
		rv := cfgProcessor{
			Name:   proc.Name,
			Title:  proc.Title,
			Source: procSources[strings.ToLower(proc.Name)],
		}
		for _, p := range proc.Params {
			rv.Params = append(rv.Params, cfgParam{Name: p.Name, Type: p.Type, Label: p.Label})
		}
		data.Processors = append(data.Processors, rv)
	}
	sort.Slice(data.Processors, func(i, j int) bool { return data.Processors[i].Name < data.Processors[j].Name })

	// Subsystems
	for _, sub := range proj.Subsystems {
		data.Subsystems = append(data.Subsystems, cfgSubsystem{
			Name:     sub.Name,
			Title:    sub.Title,
			Icon:     sub.Icon,
			Order:    sub.Order,
			Contents: sub.Contents,
		})
	}

	// Generate query builder schema
	data.QBSchema = buildQBSchema(data)

	// Backup dir & files
	data.BackupDir = h.backupDir(b)
	backupDir := data.BackupDir
	if files, err := filepath.Glob(filepath.Join(backupDir, "backup_*.sql.gz")); err == nil {
		for _, f := range files {
			info, _ := os.Stat(f)
			if info == nil {
				continue
			}
			data.BackupFiles = append(data.BackupFiles, backupFile{
				Name: filepath.Base(f),
				Size: fmt.Sprintf("%.1f KB", float64(info.Size())/1024),
				Date: info.ModTime().Format("2006-01-02 15:04"),
			})
		}
		for i, j := 0, len(data.BackupFiles)-1; i < j; i, j = i+1, j-1 {
			data.BackupFiles[i], data.BackupFiles[j] = data.BackupFiles[j], data.BackupFiles[i]
		}
	}

	// Backup settings from app.yaml
	{
		cfgPath := ""
		if b.ConfigSource == "database" {
			// read from DB config
		} else {
			cfgPath = filepath.Join(b.Path, "config", "app.yaml")
		}
		if cfgPath != "" {
			if raw, err := os.ReadFile(cfgPath); err == nil {
				var tmp struct {
					Backup struct {
						Enabled   bool   `yaml:"enabled"`
						Schedule  string `yaml:"schedule"`
						KeepLast  int    `yaml:"keep_last"`
						Directory string `yaml:"directory"`
					} `yaml:"backup"`
				}
				yaml.Unmarshal(raw, &tmp)
				data.BackupSettings = backupSettings{
					Enabled:   tmp.Backup.Enabled,
					Schedule:  tmp.Backup.Schedule,
					KeepLast:  tmp.Backup.KeepLast,
					Directory: tmp.Backup.Directory,
				}
			}
		}
	}

	return data
}

// ── query builder schema ────────────────────────────────────────────────────

type cfgQBField struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

type cfgQBSource struct {
	ID      string        `json:"id"`
	Label   string        `json:"label"`
	Group   string        `json:"group"`
	VTParam string        `json:"vtParam,omitempty"`
	Fields  []cfgQBField  `json:"fields"`
}

func cfgQBFieldType(t string) string {
	switch t {
	case "number":
		return "number"
	case "date":
		return "date"
	case "bool", "boolean":
		return "bool"
	case "reference":
		return "ref"
	default:
		return "string"
	}
}

func buildQBSchema(d *configuratorData) template.JS {
	var sources []cfgQBSource

	for _, e := range d.Catalogs {
		src := cfgQBSource{ID: "catalog:" + e.Name, Label: "Справочник." + e.Name, Group: "Справочники"}
		for _, f := range e.Fields {
			src.Fields = append(src.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
		}
		sources = append(sources, src)
	}

	for _, e := range d.Docs {
		src := cfgQBSource{ID: "document:" + e.Name, Label: "Документ." + e.Name, Group: "Документы"}
		for _, f := range e.Fields {
			src.Fields = append(src.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
		}
		sources = append(sources, src)
	}

	for _, reg := range d.Registers {
		raw := cfgQBSource{ID: "register:" + reg.Name, Label: "РегистрНакопления." + reg.Name, Group: "Регистры накопления"}
		raw.Fields = append(raw.Fields, cfgQBField{Name: "период", Label: "Период", Type: "date"})
		raw.Fields = append(raw.Fields, cfgQBField{Name: "вид_движения", Label: "ВидДвижения", Type: "string"})
		for _, f := range reg.Dimensions {
			raw.Fields = append(raw.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
		}
		for _, f := range reg.Resources {
			raw.Fields = append(raw.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
		}
		sources = append(sources, raw)

		bal := cfgQBSource{ID: "vt_balances:" + reg.Name, Label: "РегистрНакопления." + reg.Name + ".Остатки(&НаДату)", Group: "Виртуальные таблицы", VTParam: "&НаДату"}
		for _, f := range reg.Dimensions {
			bal.Fields = append(bal.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
		}
		for _, f := range reg.Resources {
			bal.Fields = append(bal.Fields, cfgQBField{Name: f.Name + "Остаток", Label: f.Name + "Остаток", Type: "res"})
		}
		sources = append(sources, bal)

		trn := cfgQBSource{ID: "vt_turnovers:" + reg.Name, Label: "РегистрНакопления." + reg.Name + ".Обороты(&Начало, &Конец)", Group: "Виртуальные таблицы", VTParam: "&Начало, &Конец"}
		for _, f := range reg.Dimensions {
			trn.Fields = append(trn.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
		}
		for _, f := range reg.Resources {
			trn.Fields = append(trn.Fields, cfgQBField{Name: f.Name + "Приход", Label: f.Name + "Приход", Type: "res"})
			trn.Fields = append(trn.Fields, cfgQBField{Name: f.Name + "Расход", Label: f.Name + "Расход", Type: "res"})
			trn.Fields = append(trn.Fields, cfgQBField{Name: f.Name + "Оборот", Label: f.Name + "Оборот", Type: "res"})
		}
		sources = append(sources, trn)
	}

	for _, ir := range d.InfoRegisters {
		raw := cfgQBSource{ID: "inforeg:" + ir.Name, Label: "РегистрСведений." + ir.Name, Group: "Регистры сведений"}
		if ir.Periodic {
			raw.Fields = append(raw.Fields, cfgQBField{Name: "period", Label: "Период", Type: "date"})
		}
		for _, f := range ir.Dimensions {
			raw.Fields = append(raw.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
		}
		for _, f := range ir.Resources {
			raw.Fields = append(raw.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
		}
		sources = append(sources, raw)

		if ir.Periodic {
			sl := cfgQBSource{ID: "vt_slice:" + ir.Name, Label: "РегистрСведений." + ir.Name + ".СрезПоследних(&НаДату)", Group: "Виртуальные таблицы", VTParam: "&НаДату"}
			for _, f := range ir.Dimensions {
				sl.Fields = append(sl.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
			}
			for _, f := range ir.Resources {
				sl.Fields = append(sl.Fields, cfgQBField{Name: f.Name, Label: f.Name, Type: cfgQBFieldType(f.Type)})
			}
			sources = append(sources, sl)
		}
	}

	b, _ := json.Marshal(sources)
	return template.JS(b)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toCfgField(f metadata.Field) cfgField {
	typ := string(f.Type)
	if f.RefEntity != "" {
		typ = "reference"
	} else if f.EnumName != "" {
		typ = "enum"
	}
	return cfgField{Name: f.Name, Type: typ, RefEntity: f.RefEntity, EnumName: f.EnumName}
}

func readOSSources(dir string) (sources, postingSources map[string]string) {
	sources = make(map[string]string)
	postingSources = make(map[string]string)
	entries, err := os.ReadDir(filepath.Join(dir, "src"))
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".os") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "src", e.Name()))
		if err != nil {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".posting.os") {
			base := strings.ToLower(strings.TrimSuffix(name, ".posting.os"))
			postingSources[base] = string(raw)
		} else if strings.HasSuffix(name, ".module.os") || strings.HasSuffix(name, ".proc.os") {
			// skip — handled by readModuleAndProcSources
		} else {
			base := strings.ToLower(strings.TrimSuffix(name, ".os"))
			sources[base] = string(raw)
		}
	}
	return
}

func readModuleAndProcSources(dir string) (moduleSources, procSources map[string]string) {
	moduleSources = make(map[string]string)
	procSources = make(map[string]string)
	entries, err := os.ReadDir(filepath.Join(dir, "src"))
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".os") {
			continue
		}
		name := e.Name()
		raw, err := os.ReadFile(filepath.Join(dir, "src", name))
		if err != nil {
			continue
		}
		if strings.HasSuffix(name, ".module.os") {
			base := strings.ToLower(strings.TrimSuffix(name, ".module.os"))
			moduleSources[base] = string(raw)
		} else if strings.HasSuffix(name, ".proc.os") {
			base := strings.ToLower(strings.TrimSuffix(name, ".proc.os"))
			procSources[base] = string(raw)
		}
	}
	return
}

type pfSourceEntry struct {
	source   string
	filename string
}

func readPrintFormSources(dir string) map[string]pfSourceEntry {
	result := make(map[string]pfSourceEntry)
	entries, err := os.ReadDir(filepath.Join(dir, "printforms"))
	if err != nil {
		return result
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "printforms", e.Name()))
		if err != nil {
			continue
		}
		var hdr struct {
			Name string `yaml:"name"`
		}
		if yaml.Unmarshal(raw, &hdr) == nil && hdr.Name != "" {
			result[hdr.Name] = pfSourceEntry{source: string(raw), filename: e.Name()}
		}
	}
	return result
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func (h *handler) configuratorSaveModule(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	entityName := r.FormValue("entity")
	moduleType := r.FormValue("module_type")
	source := r.FormValue("source")

	var filename string
	if moduleType == "posting" {
		filename = entityToPostingFilename(entityName)
	} else {
		filename = entityToFilename(entityName)
	}

	var saveErr error
	if b.ConfigSource == "database" {
		db, err := storage.Connect(r.Context(), b.DB)
		if err != nil {
			saveErr = err
		} else {
			defer db.Close()
			_, saveErr = db.Pool().Exec(r.Context(), `
				INSERT INTO _onebase_config (path, content, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
			`, "src/"+filename, []byte(source))
		}
	} else {
		srcDir := filepath.Join(b.Path, "src")
		os.MkdirAll(srcDir, 0o755)
		saveErr = os.WriteFile(filepath.Join(srcDir, filename), []byte(source), 0o644)
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.ModuleSaved = true
		data.ModuleSavedEntity = entityName
	}
	renderCfg(w, data)
}

// entityToFilename converts "ПоступлениеТоваров" → "поступлениеТоваров.os"
func entityToFilename(name string) string {
	if name == "" {
		return ".os"
	}
	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes) + ".os"
}

// entityToPostingFilename converts "ПоступлениеТоваров" → "поступлениеТоваров.posting.os"
func entityToPostingFilename(name string) string {
	if name == "" {
		return ".posting.os"
	}
	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes) + ".posting.os"
}

// ── field-type save ───────────────────────────────────────────────────────────

func findEntityFilePath(dir, entityName string) (string, error) {
	for _, sub := range []string{"catalogs", "documents"} {
		items, _ := os.ReadDir(filepath.Join(dir, sub))
		for _, item := range items {
			if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
				continue
			}
			p := filepath.Join(dir, sub, item.Name())
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			var hdr struct {
				Name string `yaml:"name"`
			}
			if yaml.Unmarshal(data, &hdr) == nil && hdr.Name == entityName {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("entity %q not found", entityName)
}

func applyFieldEdits(ent *saveEntity, fields []saveField, tpFields map[string][]saveField, posting *bool) {
	ent.Fields = fields
	for i, tp := range ent.TableParts {
		if f, ok := tpFields[tp.Name]; ok {
			ent.TableParts[i].Fields = f
		}
	}
	if posting != nil {
		ent.Posting = *posting
	}
}

func saveEntityFieldsToFile(dir, entityName string, fields []saveField, tpFields map[string][]saveField, posting *bool) error {
	filePath, err := findEntityFilePath(dir, entityName)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	var ent saveEntity
	if err := yaml.Unmarshal(raw, &ent); err != nil {
		return err
	}
	applyFieldEdits(&ent, fields, tpFields, posting)
	out, err := yaml.Marshal(&ent)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, out, 0o644)
}

func (h *handler) saveEntityFieldsToDB(ctx context.Context, b *Base, entityName string, fields []saveField, tpFields map[string][]saveField, posting *bool) error {
	db, err := storage.Connect(ctx, b.DB)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer db.Close()

	rows, err := db.Pool().Query(ctx,
		`SELECT path, content FROM _onebase_config WHERE path LIKE 'catalogs/%.yaml' OR path LIKE 'documents/%.yaml'`)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var targetPath string
	var ent saveEntity
	for rows.Next() {
		var p string
		var content []byte
		if err := rows.Scan(&p, &content); err != nil {
			continue
		}
		var e saveEntity
		if yaml.Unmarshal(content, &e) == nil && e.Name == entityName {
			targetPath = p
			ent = e
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if targetPath == "" {
		return fmt.Errorf("entity %q not found in DB config", entityName)
	}

	applyFieldEdits(&ent, fields, tpFields, posting)
	out, err := yaml.Marshal(&ent)
	if err != nil {
		return err
	}
	_, err = db.Pool().Exec(ctx, `
		INSERT INTO _onebase_config (path, content, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
	`, targetPath, out)
	return err
}

func (h *handler) configuratorSaveForm(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	entityName := r.FormValue("entity")
	dir := b.Path
	if b.ConfigSource == "database" {
		dir, err = workspacePath(b.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}

	// Find entity YAML file
	entityDir := ""
	for _, sub := range []string{"catalogs", "documents"} {
		p := filepath.Join(dir, sub, nameToFilename(entityName)+".yaml")
		if _, e := os.Stat(p); e == nil {
			entityDir = sub
			break
		}
	}
	if entityDir == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Файл сущности не найден: " + entityName
		renderCfg(w, data)
		return
	}

	filePath := filepath.Join(dir, entityDir, nameToFilename(entityName)+".yaml")
	raw, err := os.ReadFile(filePath)
	if err != nil {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Ошибка чтения: " + err.Error()
		renderCfg(w, data)
		return
	}

	// Parse as generic map, update list_form and item_form
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Ошибка YAML: " + err.Error()
		renderCfg(w, data)
		return
	}

	// Build list_form from lf.N.name + lf.N.vis
	var listForm []string
	for i := 0; ; i++ {
		name := r.FormValue(fmt.Sprintf("lf.%d.name", i))
		if name == "" {
			break
		}
		vis := r.FormValue(fmt.Sprintf("lf.%d.vis", i))
		if vis == "1" {
			listForm = append(listForm, name)
		}
	}
	if len(listForm) > 0 {
		doc["list_form"] = listForm
	} else {
		delete(doc, "list_form")
	}

	// Build item_form from ef.N.name + ef.N.vis
	var itemForm []string
	for i := 0; ; i++ {
		name := r.FormValue(fmt.Sprintf("ef.%d.name", i))
		if name == "" {
			// Try table part fields
			name = r.FormValue(fmt.Sprintf("ef.tp0.%d.name", i))
		}
		if name == "" {
			break
		}
		vis := r.FormValue(fmt.Sprintf("ef.%d.vis", i))
		if vis == "" {
			vis = r.FormValue(fmt.Sprintf("ef.tp0.%d.vis", i))
		}
		if vis == "1" {
			itemForm = append(itemForm, name)
		}
	}
	// Also check table part fields with any index
	for tpJ := 0; ; tpJ++ {
		foundAny := false
		for fi := 0; ; fi++ {
			name := r.FormValue(fmt.Sprintf("ef.tp%d.%d.name", tpJ, fi))
			if name == "" {
				break
			}
			foundAny = true
			vis := r.FormValue(fmt.Sprintf("ef.tp%d.%d.vis", tpJ, fi))
			if vis == "1" {
				itemForm = append(itemForm, name)
			}
		}
		if !foundAny {
			break
		}
	}
	if len(itemForm) > 0 {
		doc["item_form"] = itemForm
	} else {
		delete(doc, "item_form")
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Ошибка сериализации: " + err.Error()
		renderCfg(w, data)
		return
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if err := os.WriteFile(filePath, out, 0o644); err != nil {
		data.Error = "Ошибка сохранения: " + err.Error()
		renderCfg(w, data)
		return
	}

	fresh := h.loadCfgData(r.Context(), b, "tree")
	fresh.FieldsSaved = true
	fresh.FieldsSavedEntity = entityName
	renderCfg(w, fresh)
}

func (h *handler) configuratorSaveFields(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	entityName := r.FormValue("entity")
	tpNames := r.Form["tp_names"]
	entityKind := r.FormValue("entity_kind")

	// read posting checkbox (only meaningful for documents)
	var posting *bool
	if entityKind == "Документ" {
		v := r.FormValue("posting") == "true"
		posting = &v
	}

	var fields []saveField
	for i := 0; i < 500; i++ {
		name := r.FormValue(fmt.Sprintf("field.%d.name", i))
		if name == "" {
			break
		}
		typ := r.FormValue(fmt.Sprintf("field.%d.type", i))
		ref := r.FormValue(fmt.Sprintf("field.%d.ref", i))
		if typ == "reference" {
			if ref == "" {
				data := h.loadCfgData(r.Context(), b, "tree")
				data.Error = fmt.Sprintf("Поле «%s»: выберите объект для ссылки", name)
				renderCfg(w, data)
				return
			}
			typ = "reference:" + ref
		}
		fields = append(fields, saveField{Name: name, Type: typ})
	}
	// new fields added via "+ Добавить поле"
	for i := 1; i <= 500; i++ {
		name := strings.TrimSpace(r.FormValue(fmt.Sprintf("new_field.%d.name", i)))
		if name == "" {
			continue
		}
		typ := r.FormValue(fmt.Sprintf("new_field.%d.type", i))
		ref := r.FormValue(fmt.Sprintf("new_field.%d.ref", i))
		if typ == "reference" {
			if ref == "" {
				data := h.loadCfgData(r.Context(), b, "tree")
				data.Error = fmt.Sprintf("Поле «%s»: выберите объект для ссылки", name)
				renderCfg(w, data)
				return
			}
			typ = "reference:" + ref
		}
		fields = append(fields, saveField{Name: name, Type: typ})
	}

	tpFields := make(map[string][]saveField)
	for _, tpName := range tpNames {
		var f []saveField
		for i := 0; i < 500; i++ {
			name := r.FormValue(fmt.Sprintf("tp.%s.field.%d.name", tpName, i))
			if name == "" {
				break
			}
			typ := r.FormValue(fmt.Sprintf("tp.%s.field.%d.type", tpName, i))
			ref := r.FormValue(fmt.Sprintf("tp.%s.field.%d.ref", tpName, i))
			if typ == "reference" {
				if ref == "" {
					data := h.loadCfgData(r.Context(), b, "tree")
					data.Error = fmt.Sprintf("Поле «%s.%s»: выберите объект для ссылки", tpName, name)
					renderCfg(w, data)
					return
				}
				typ = "reference:" + ref
			}
			f = append(f, saveField{Name: name, Type: typ})
		}
		tpFields[tpName] = f
	}
	// new table parts added via "+ Добавить табличную часть"
	for _, tpIdx := range r.Form["new_tp_name"] {
		tpName := strings.TrimSpace(tpIdx)
		if tpName == "" {
			continue
		}
		tpFields[tpName] = nil
	}
	for ntp := 1; ntp <= 100; ntp++ {
		tpKey := strings.TrimSpace(r.FormValue(fmt.Sprintf("new_tp.%d.idx", ntp)))
		if tpKey == "" {
			continue
		}
		var f []saveField
		for i := 1; i <= 500; i++ {
			name := strings.TrimSpace(r.FormValue(fmt.Sprintf("new_tp.%d.field.%d.name", ntp, i)))
			if name == "" {
				continue
			}
			typ := r.FormValue(fmt.Sprintf("new_tp.%d.field.%d.type", ntp, i))
			ref := r.FormValue(fmt.Sprintf("new_tp.%d.field.%d.ref", ntp, i))
			if typ == "reference" {
				if ref == "" {
					data := h.loadCfgData(r.Context(), b, "tree")
					data.Error = fmt.Sprintf("Поле «%s.%s»: выберите объект для ссылки", tpKey, name)
					renderCfg(w, data)
					return
				}
				typ = "reference:" + ref
			}
			f = append(f, saveField{Name: name, Type: typ})
		}
		tpFields[tpKey] = f
	}

	var saveErr error
	if b.ConfigSource == "database" {
		saveErr = h.saveEntityFieldsToDB(r.Context(), b, entityName, fields, tpFields, posting)
	} else {
		saveErr = saveEntityFieldsToFile(b.Path, entityName, fields, tpFields, posting)
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = entityName
	}
	renderCfg(w, data)
}

func renderCfg(w http.ResponseWriter, data *configuratorData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := cfgTmpl.ExecuteTemplate(w, "cfg-main", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

// ── Register field save ───────────────────────────────────────────────────────

func findRegisterFilePath(dir, regName string) (string, error) {
	items, _ := os.ReadDir(filepath.Join(dir, "registers"))
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			continue
		}
		p := filepath.Join(dir, "registers", item.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var hdr struct {
			Name string `yaml:"name"`
		}
		if yaml.Unmarshal(data, &hdr) == nil && hdr.Name == regName {
			return p, nil
		}
	}
	return "", fmt.Errorf("register %q not found", regName)
}

func saveRegisterFieldsToFile(dir, regName string, dims, res, attrs []saveField) error {
	filePath, err := findRegisterFilePath(dir, regName)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	var reg saveRegister
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		return err
	}
	reg.Dimensions = dims
	reg.Resources = res
	reg.Attributes = attrs
	out, err := yaml.Marshal(&reg)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, out, 0o644)
}

func (h *handler) saveRegisterFieldsToDB(ctx context.Context, b *Base, regName string, dims, res, attrs []saveField) error {
	db, err := storage.Connect(ctx, b.DB)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer db.Close()

	rows, err := db.Pool().Query(ctx,
		`SELECT path, content FROM _onebase_config WHERE path LIKE 'registers/%.yaml'`)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var targetPath string
	var reg saveRegister
	for rows.Next() {
		var p string
		var content []byte
		if err := rows.Scan(&p, &content); err != nil {
			continue
		}
		var r saveRegister
		if yaml.Unmarshal(content, &r) == nil && r.Name == regName {
			targetPath = p
			reg = r
			break
		}
	}
	rows.Close()
	if targetPath == "" {
		return fmt.Errorf("register %q not found in DB config", regName)
	}

	reg.Dimensions = dims
	reg.Resources = res
	reg.Attributes = attrs
	out, err := yaml.Marshal(&reg)
	if err != nil {
		return err
	}
	_, err = db.Pool().Exec(ctx, `
		INSERT INTO _onebase_config (path, content, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
	`, targetPath, out)
	return err
}

func (h *handler) configuratorSaveRegisterFields(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	regName := r.FormValue("register")

	parseSection := func(prefix string) []saveField {
		var fields []saveField
		for i := 0; i < 500; i++ {
			name := r.FormValue(fmt.Sprintf("%s.%d.name", prefix, i))
			if name == "" {
				break
			}
			typ := r.FormValue(fmt.Sprintf("%s.%d.type", prefix, i))
			ref := r.FormValue(fmt.Sprintf("%s.%d.ref", prefix, i))
			if typ == "reference" && ref != "" {
				typ = "reference:" + ref
			}
			fields = append(fields, saveField{Name: name, Type: typ})
		}
		return fields
	}

	dims := parseSection("dim")
	res := parseSection("res")
	attrs := parseSection("attr")

	var saveErr error
	if b.ConfigSource == "database" {
		saveErr = h.saveRegisterFieldsToDB(r.Context(), b, regName, dims, res, attrs)
	} else {
		saveErr = saveRegisterFieldsToFile(b.Path, regName, dims, res, attrs)
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = regName
	}
	renderCfg(w, data)
}

// ── New object creation ───────────────────────────────────────────────────────

func (h *handler) configuratorNewObject(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	kind := r.FormValue("kind")
	name := strings.TrimSpace(r.FormValue("name"))

	if name == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Укажите имя объекта"
		renderCfg(w, data)
		return
	}

	subdir, content := newObjectContent(kind, name)
	if subdir == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Неизвестный тип объекта: " + kind
		renderCfg(w, data)
		return
	}

	filename := nameToFilename(name) + ".yaml"

	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(r.Context(), b.DB)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			repo := configdb.New(db.Pool())
			repo.EnsureSchema(r.Context())
			path := subdir + "/" + filename
			_, saveErr = db.Pool().Exec(r.Context(), `
				INSERT INTO _onebase_config (path, content, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
			`, path, []byte(content))
		}
	} else {
		dir := filepath.Join(b.Path, subdir)
		os.MkdirAll(dir, 0o755)
		saveErr = os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644)
	}

	if saveErr != nil {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Ошибка создания: " + saveErr.Error()
		renderCfg(w, data)
		return
	}
	http.Redirect(w, r, "/bases/"+b.ID+"/configurator?tab=tree", http.StatusFound)
}

func newObjectContent(kind, name string) (subdir, content string) {
	switch kind {
	case "catalog":
		return "catalogs", "name: " + name + "\nfields:\n  - name: Наименование\n    type: string\n"
	case "document":
		return "documents", "name: " + name + "\nfields:\n  - name: Дата\n    type: date\n"
	case "register":
		return "registers", "name: " + name + "\ndimensions:\n  - name: Измерение1\n    type: string\nresources:\n  - name: Ресурс1\n    type: number\n"
	case "inforeg":
		return "inforegs", "name: " + name + "\nperiodic: false\ndimensions:\n  - name: Ключ\n    type: string\nresources:\n  - name: Значение\n    type: string\n"
	case "enum":
		return "enums", "name: " + name + "\nvalues:\n  - Значение1\n  - Значение2\n"
	case "subsystem":
		return "subsystems", "name: " + name + "\ntitle: " + name + "\norder: 10\ncontents:\n  catalogs: []\n  documents: []\n  registers: []\n"
	}
	return "", ""
}

func nameToFilename(name string) string {
	return strings.ToLower(name)
}

// ── Enum save ─────────────────────────────────────────────────────────────────

func (h *handler) configuratorSaveEnum(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	enumName := r.FormValue("enum_name")
	rawValues := r.FormValue("values")

	var values []string
	for _, line := range strings.Split(rawValues, "\n") {
		v := strings.TrimSpace(line)
		if v != "" {
			values = append(values, v)
		}
	}

	type saveEnum struct {
		Name   string   `yaml:"name"`
		Values []string `yaml:"values"`
	}
	out, _ := yaml.Marshal(saveEnum{Name: enumName, Values: values})

	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(r.Context(), b.DB)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			path := "enums/" + nameToFilename(enumName) + ".yaml"
			_, saveErr = db.Pool().Exec(r.Context(), `
				INSERT INTO _onebase_config (path, content, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
			`, path, out)
		}
	} else {
		dir := filepath.Join(b.Path, "enums")
		os.MkdirAll(dir, 0o755)
		// find existing file by name field, fallback to name-based filename
		files, _ := os.ReadDir(dir)
		targetFile := filepath.Join(dir, nameToFilename(enumName)+".yaml")
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			p := filepath.Join(dir, f.Name())
			raw, _ := os.ReadFile(p)
			var hdr struct{ Name string `yaml:"name"` }
			if yaml.Unmarshal(raw, &hdr) == nil && hdr.Name == enumName {
				targetFile = p
				break
			}
		}
		saveErr = os.WriteFile(targetFile, out, 0o644)
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = enumName
	}
	renderCfg(w, data)
}

// ── Constant save ─────────────────────────────────────────────────────────────

func (h *handler) configuratorSaveConstant(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	constName := r.FormValue("const_name")
	label := strings.TrimSpace(r.FormValue("label"))
	typ := strings.TrimSpace(r.FormValue("type"))
	ref := strings.TrimSpace(r.FormValue("ref"))
	def := strings.TrimSpace(r.FormValue("default"))
	if typ == "reference" && ref != "" {
		typ = "reference:" + ref
	}

	type rawConst struct {
		Name    string `yaml:"name"`
		Type    string `yaml:"type"`
		Default string `yaml:"default,omitempty"`
		Label   string `yaml:"label,omitempty"`
	}
	type rawConstsFile struct {
		Constants []rawConst `yaml:"constants"`
	}

	updateConstantsFile := func(raw []byte) ([]byte, error) {
		var cf rawConstsFile
		if err := yaml.Unmarshal(raw, &cf); err != nil {
			return nil, err
		}
		for i, c := range cf.Constants {
			if c.Name == constName {
				cf.Constants[i].Label = label
				cf.Constants[i].Type = typ
				cf.Constants[i].Default = def
				break
			}
		}
		return yaml.Marshal(&cf)
	}

	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(r.Context(), b.DB)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			rows, _ := db.Pool().Query(r.Context(),
				`SELECT path, content FROM _onebase_config WHERE path LIKE 'constants/%.yaml'`)
			var targetPath string
			var targetContent []byte
			for rows.Next() {
				var p string
				var content []byte
				rows.Scan(&p, &content)
				var cf rawConstsFile
				if yaml.Unmarshal(content, &cf) == nil {
					for _, c := range cf.Constants {
						if c.Name == constName {
							targetPath = p
							targetContent = content
							break
						}
					}
				}
				if targetPath != "" {
					break
				}
			}
			rows.Close()
			if targetPath != "" {
				if out, err := updateConstantsFile(targetContent); err == nil {
					_, saveErr = db.Pool().Exec(r.Context(), `
						INSERT INTO _onebase_config (path, content, updated_at)
						VALUES ($1, $2, now())
						ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
					`, targetPath, out)
				}
			}
		}
	} else {
		dir := filepath.Join(b.Path, "constants")
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			p := filepath.Join(dir, f.Name())
			raw, _ := os.ReadFile(p)
			var cf rawConstsFile
			if yaml.Unmarshal(raw, &cf) != nil {
				continue
			}
			found := false
			for _, c := range cf.Constants {
				if c.Name == constName {
					found = true
					break
				}
			}
			if !found {
				continue
			}
			out, err := updateConstantsFile(raw)
			if err == nil {
				saveErr = os.WriteFile(p, out, 0o644)
			} else {
				saveErr = err
			}
			break
		}
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = constName
	}
	renderCfg(w, data)
}

// ── Report save ───────────────────────────────────────────────────────────────

func (h *handler) configuratorSaveReport(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	repName := r.FormValue("report_name")
	query := r.FormValue("query")
	title := strings.TrimSpace(r.FormValue("title"))

	type saveParam struct {
		Name  string `yaml:"name"`
		Type  string `yaml:"type"`
		Label string `yaml:"label,omitempty"`
	}
	type saveReport struct {
		Name   string      `yaml:"name"`
		Title  string      `yaml:"title,omitempty"`
		Params []saveParam `yaml:"params,omitempty"`
		Query  string      `yaml:"query"`
	}

	// Parse params from form: param.0.name, param.0.type, param.0.label, ...
	var newParams []saveParam
	for i := 0; i < 50; i++ {
		pname := strings.TrimSpace(r.FormValue(fmt.Sprintf("param.%d.name", i)))
		if pname == "" {
			break
		}
		ptype := r.FormValue(fmt.Sprintf("param.%d.type", i))
		plabel := strings.TrimSpace(r.FormValue(fmt.Sprintf("param.%d.label", i)))
		newParams = append(newParams, saveParam{Name: pname, Type: ptype, Label: plabel})
	}

	updateReportFile := func(raw []byte) ([]byte, error) {
		var rep saveReport
		if err := yaml.Unmarshal(raw, &rep); err != nil {
			return nil, err
		}
		rep.Query = query
		if title != "" {
			rep.Title = title
		}
		rep.Params = newParams
		return yaml.Marshal(&rep)
	}

	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(r.Context(), b.DB)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			rows, _ := db.Pool().Query(r.Context(),
				`SELECT path, content FROM _onebase_config WHERE path LIKE 'reports/%.yaml'`)
			var targetPath string
			var targetContent []byte
			for rows.Next() {
				var p string
				var content []byte
				rows.Scan(&p, &content)
				var rep saveReport
				if yaml.Unmarshal(content, &rep) == nil && rep.Name == repName {
					targetPath = p
					targetContent = content
					break
				}
			}
			rows.Close()
			if targetPath != "" {
				if out, err := updateReportFile(targetContent); err == nil {
					_, saveErr = db.Pool().Exec(r.Context(), `
						INSERT INTO _onebase_config (path, content, updated_at)
						VALUES ($1, $2, now())
						ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
					`, targetPath, out)
				}
			}
		}
	} else {
		dir := filepath.Join(b.Path, "reports")
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			p := filepath.Join(dir, f.Name())
			raw, _ := os.ReadFile(p)
			var rep saveReport
			if yaml.Unmarshal(raw, &rep) != nil || rep.Name != repName {
				continue
			}
			out, err := updateReportFile(raw)
			if err == nil {
				saveErr = os.WriteFile(p, out, 0o644)
			} else {
				saveErr = err
			}
			break
		}
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = repName
	}
	renderCfg(w, data)
}

// ── Common module save ────────────────────────────────────────────────────────

func (h *handler) configuratorSaveCommonModule(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	moduleName := r.FormValue("module_name")
	source := r.FormValue("source")

	filename := moduleNameToFilename(moduleName)

	var saveErr error
	if b.ConfigSource == "database" {
		db, err := storage.Connect(r.Context(), b.DB)
		if err != nil {
			saveErr = err
		} else {
			defer db.Close()
			_, saveErr = db.Pool().Exec(r.Context(), `
				INSERT INTO _onebase_config (path, content, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
			`, "src/"+filename, []byte(source))
		}
	} else {
		srcDir := filepath.Join(b.Path, "src")
		os.MkdirAll(srcDir, 0o755)
		saveErr = os.WriteFile(filepath.Join(srcDir, filename), []byte(source), 0o644)
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.ModuleSaved = true
		data.ModuleSavedEntity = moduleName
	}
	renderCfg(w, data)
}

func moduleNameToFilename(name string) string {
	if name == "" {
		return ".module.os"
	}
	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes) + ".module.os"
}

// ── Processor save ────────────────────────────────────────────────────────────

func (h *handler) configuratorSaveProcessor(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	procName := r.FormValue("processor_name")
	title := strings.TrimSpace(r.FormValue("title"))
	source := r.FormValue("source")

	type saveParam struct {
		Name  string `yaml:"name"`
		Type  string `yaml:"type"`
		Label string `yaml:"label,omitempty"`
	}
	type saveProcessor struct {
		Name   string      `yaml:"name"`
		Title  string      `yaml:"title,omitempty"`
		Params []saveParam `yaml:"params,omitempty"`
	}

	var newParams []saveParam
	for i := 0; i < 50; i++ {
		pname := strings.TrimSpace(r.FormValue(fmt.Sprintf("param.%d.name", i)))
		if pname == "" {
			break
		}
		ptype := r.FormValue(fmt.Sprintf("param.%d.type", i))
		plabel := strings.TrimSpace(r.FormValue(fmt.Sprintf("param.%d.label", i)))
		newParams = append(newParams, saveParam{Name: pname, Type: ptype, Label: plabel})
	}

	yamlData, _ := yaml.Marshal(saveProcessor{Name: procName, Title: title, Params: newParams})
	yamlFilename := "processors/" + nameToFilename(procName) + ".yaml"
	srcFilename := "src/" + processorSrcFilename(procName)

	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(r.Context(), b.DB)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			if _, err := db.Pool().Exec(r.Context(), `
				INSERT INTO _onebase_config (path, content, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
			`, yamlFilename, yamlData); err != nil {
				saveErr = err
			}
			if saveErr == nil {
				_, saveErr = db.Pool().Exec(r.Context(), `
					INSERT INTO _onebase_config (path, content, updated_at)
					VALUES ($1, $2, now())
					ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
				`, srcFilename, []byte(source))
			}
		}
	} else {
		procDir := filepath.Join(b.Path, "processors")
		os.MkdirAll(procDir, 0o755)
		if err := os.WriteFile(filepath.Join(b.Path, yamlFilename), yamlData, 0o644); err != nil {
			saveErr = err
		}
		if saveErr == nil {
			srcDir := filepath.Join(b.Path, "src")
			os.MkdirAll(srcDir, 0o755)
			saveErr = os.WriteFile(filepath.Join(b.Path, srcFilename), []byte(source), 0o644)
		}
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = procName
	}
	renderCfg(w, data)
}

func processorSrcFilename(name string) string {
	if name == "" {
		return ".proc.os"
	}
	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes) + ".proc.os"
}

// ── Print form save ───────────────────────────────────────────────────────────

func (h *handler) configuratorSavePrintForm(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	filename := strings.TrimSpace(r.FormValue("printform_filename"))
	source := r.FormValue("source")

	if filename == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Имя файла печатной формы не указано"
		renderCfg(w, data)
		return
	}

	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(r.Context(), b.DB)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			_, saveErr = db.Pool().Exec(r.Context(), `
				INSERT INTO _onebase_config (path, content, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
			`, "printforms/"+filename, []byte(source))
		}
	} else {
		pfDir := filepath.Join(b.Path, "printforms")
		os.MkdirAll(pfDir, 0o755)
		saveErr = os.WriteFile(filepath.Join(pfDir, filename), []byte(source), 0o644)
	}

	var hdr struct {
		Name string `yaml:"name"`
	}
	yaml.Unmarshal([]byte(source), &hdr) //nolint
	pfName := hdr.Name
	isDSL := r.FormValue("printform_dsl") == "1"
	if isDSL {
		pfName = strings.TrimSuffix(filename, ".os")
	} else if pfName == "" {
		pfName = strings.TrimSuffix(filename, ".yaml")
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = pfName
	}
	renderCfg(w, data)
}

func (h *handler) configuratorNewPrintForm(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	document := strings.TrimSpace(r.FormValue("document"))

	if name == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Имя печатной формы обязательно"
		renderCfg(w, data)
		return
	}

	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])
	filename := string(runes) + ".yaml"

	source := fmt.Sprintf("name: %s\ndocument: %s\ntitle: \"{{Номер}} от {{Дата | date}}\"\n\nheader: |\n  ## %s\n\ntable:\n  source: Товары\n  columns:\n    - field: \"@row\"\n      label: \"№\"\n      width: 36px\n      align: center\n", name, document, name)

	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(r.Context(), b.DB)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			_, saveErr = db.Pool().Exec(r.Context(), `
				INSERT INTO _onebase_config (path, content, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
			`, "printforms/"+filename, []byte(source))
		}
	} else {
		pfDir := filepath.Join(b.Path, "printforms")
		os.MkdirAll(pfDir, 0o755)
		fullPath := filepath.Join(pfDir, filename)
		if _, statErr := os.Stat(fullPath); os.IsNotExist(statErr) {
			saveErr = os.WriteFile(fullPath, []byte(source), 0o644)
		}
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка создания: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = name
	}
	renderCfg(w, data)
}

func (h *handler) configuratorSaveLayout(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	layoutName := strings.TrimSpace(r.FormValue("layout_name"))
	source := r.FormValue("source")

	if layoutName == "" {
		http.Error(w, "layout name required", 400)
		return
	}

	filename := layoutName + ".layout.yaml"

	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(r.Context(), b.DB)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			_, saveErr = db.Pool().Exec(r.Context(), `
				INSERT INTO _onebase_config (path, content, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
			`, "printforms/"+filename, []byte(source))
		}
	} else {
		pfDir := filepath.Join(b.Path, "printforms")
		layoutPath := ""
		filepath.Walk(pfDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Base(path), filename) {
				layoutPath = path
			}
			return nil
		})
		if layoutPath == "" {
			filepath.Walk(pfDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if strings.EqualFold(filepath.Base(path), layoutName+".os") {
					layoutPath = filepath.Join(filepath.Dir(path), filename)
				}
				return nil
			})
		}
		if layoutPath == "" {
			layoutPath = filepath.Join(pfDir, filename)
		}
		saveErr = os.WriteFile(layoutPath, []byte(source), 0o644)
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения макета: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = layoutName
	}
	renderCfg(w, data)
}


// ── Subsystem save ──────────────────────────────────────────────────────────

func (h *handler) configuratorSaveSubsystem(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	subName := r.FormValue("subsystem_name")
	title := r.FormValue("title")
	icon := r.FormValue("icon")
	orderStr := r.FormValue("order")
	var order int
	if orderStr != "" {
		fmt.Sscanf(orderStr, "%d", &order)
	}

	if title == "" {
		title = subName
	}

	dir := b.Path
	if b.ConfigSource == "database" {
		dir, err = workspacePath(b.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}

	subDir := filepath.Join(dir, "subsystems")
	os.MkdirAll(subDir, 0o755)

	type yamlSubsystem struct {
		Name     string `yaml:"name"`
		Title    string `yaml:"title"`
		Icon     string `yaml:"icon"`
		Order    int    `yaml:"order"`
		Contents struct {
			Catalogs   []string `yaml:"catalogs"`
			Documents  []string `yaml:"documents"`
			Registers  []string `yaml:"registers"`
			InfoRegs   []string `yaml:"inforegs"`
			Reports    []string `yaml:"reports"`
			Processors []string `yaml:"processors"`
			Journals   []string `yaml:"journals"`
		} `yaml:"contents"`
	}

	ys := yamlSubsystem{
		Name:  subName,
		Title: title,
		Icon:  icon,
		Order: order,
	}
	ys.Contents.Catalogs = r.Form["catalogs"]
	ys.Contents.Documents = r.Form["documents"]
	ys.Contents.Registers = r.Form["registers"]
	ys.Contents.InfoRegs = r.Form["inforegs"]
	ys.Contents.Reports = r.Form["reports"]
	ys.Contents.Processors = r.Form["processors"]

	out, _ := yaml.Marshal(&ys)
	targetFile := filepath.Join(subDir, nameToFilename(subName)+".yaml")

	data := h.loadCfgData(r.Context(), b, "tree")
	if err := os.WriteFile(targetFile, out, 0o644); err != nil {
		data.Error = "Ошибка сохранения: " + err.Error()
		renderCfg(w, data)
		return
	}
	data.FieldsSaved = true
	data.FieldsSavedEntity = subName
	renderCfg(w, data)

	// reload tree to reflect changes
	fresh := h.loadCfgData(r.Context(), b, "tree")
	fresh.FieldsSaved = true
	fresh.FieldsSavedEntity = subName
	renderCfg(w, fresh)
}

// ── App config save ───────────────────────────────────────────────────────────

func (h *handler) configuratorSaveApp(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	newName := strings.TrimSpace(r.FormValue("app_name"))
	newVersion := strings.TrimSpace(r.FormValue("app_version"))
	if newName == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = "Имя конфигурации не может быть пустым"
		renderCfg(w, data)
		return
	}

	type saveAppConfig struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version,omitempty"`
	}
	out, _ := yaml.Marshal(saveAppConfig{Name: newName, Version: newVersion})

	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := storage.Connect(r.Context(), b.DB)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			_, saveErr = db.Pool().Exec(r.Context(), `
				INSERT INTO _onebase_config (path, content, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_at=now()
			`, "config/app.yaml", out)
		}
	} else {
		dir := filepath.Join(b.Path, "config")
		os.MkdirAll(dir, 0o755)
		saveErr = os.WriteFile(filepath.Join(dir, "app.yaml"), out, 0o644)
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = "Ошибка сохранения: " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = "__app__"
	}
	renderCfg(w, data)
}

// ── debug proxy ──────────────────────────────────────────────────────────────

// debugProxy forwards debug API requests from the configurator (launcher server)
// to the UI server, avoiding CORS issues in the webview.
func (h *handler) debugProxy(w http.ResponseWriter, r *http.Request) {
	baseID := chi.URLParam(r, "id")
	action := chi.URLParam(r, "action")

	b, err := h.store.Get(baseID)
	if err != nil {
		http.Error(w, "base not found", 404)
		return
	}

	uiURL := fmt.Sprintf("http://localhost:%d/debug/global/%s", b.Port, action)

	req, err := http.NewRequestWithContext(r.Context(), r.Method, uiURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Forward Content-Type from original request
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "UI server unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
