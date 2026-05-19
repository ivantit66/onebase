package runtime

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/printform"
	"github.com/ivantit66/onebase/internal/processor"
	"github.com/ivantit66/onebase/internal/report"
)

type Registry struct {
	mu              sync.RWMutex
	entities        map[string]*metadata.Entity
	entitySlug      map[string]*metadata.Entity // lowercase name → entity
	registers       map[string]*metadata.Register
	inforegs        map[string]*metadata.InfoRegister
	enums           map[string]*metadata.Enum
	constants       map[string]*metadata.Constant
	reports         map[string]*report.Report
	printForms      map[string][]*printform.PrintForm   // lowercase entity name → forms
	dslPrintForms   map[string][]*printform.DSLPrintForm // lowercase entity name → DSL forms
	procs           map[string]map[string]*ast.ProcedureDecl
	moduleProcs     map[string]*ast.ProcedureDecl // flat: proc name → decl
	processors      map[string]*processor.Processor
	subsystems      []*metadata.Subsystem // sorted by Order
	journals        map[string]*metadata.Journal
	accountRegs     map[string]*metadata.AccountRegister
	chartsOfAccount map[string]*metadata.ChartOfAccounts
	widgets         map[string]*metadata.Widget // lowercase name → widget
	widgetOrder     []string                    // preserves load order
	homePage        *metadata.HomePage
}

func NewRegistry() *Registry {
	return &Registry{
		entities:        make(map[string]*metadata.Entity),
		entitySlug:      make(map[string]*metadata.Entity),
		registers:       make(map[string]*metadata.Register),
		inforegs:        make(map[string]*metadata.InfoRegister),
		enums:           make(map[string]*metadata.Enum),
		constants:       make(map[string]*metadata.Constant),
		reports:         make(map[string]*report.Report),
		printForms:      make(map[string][]*printform.PrintForm),
		dslPrintForms:   make(map[string][]*printform.DSLPrintForm),
		procs:           make(map[string]map[string]*ast.ProcedureDecl),
		moduleProcs:     make(map[string]*ast.ProcedureDecl),
		processors:      make(map[string]*processor.Processor),
		journals:        make(map[string]*metadata.Journal),
		accountRegs:     make(map[string]*metadata.AccountRegister),
		chartsOfAccount: make(map[string]*metadata.ChartOfAccounts),
		widgets:         make(map[string]*metadata.Widget),
	}
}

// LoadWidgets registers dashboard widgets by name (case-insensitive).
func (r *Registry) LoadWidgets(widgets []*metadata.Widget) {
	m := make(map[string]*metadata.Widget, len(widgets))
	order := make([]string, 0, len(widgets))
	for _, w := range widgets {
		key := strings.ToLower(w.Name)
		m[key] = w
		order = append(order, w.Name)
	}
	r.mu.Lock()
	r.widgets = m
	r.widgetOrder = order
	r.mu.Unlock()
}

// GetWidget returns a widget by name (case-insensitive). nil if not found.
func (r *Registry) GetWidget(name string) *metadata.Widget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.widgets[strings.ToLower(name)]
}

// Widgets returns all registered widgets in load order.
func (r *Registry) Widgets() []*metadata.Widget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.Widget, 0, len(r.widgetOrder))
	for _, name := range r.widgetOrder {
		if w, ok := r.widgets[strings.ToLower(name)]; ok {
			out = append(out, w)
		}
	}
	return out
}

// LoadHomePage stores the dashboard layout. Pass nil to reset.
func (r *Registry) LoadHomePage(hp *metadata.HomePage) {
	r.mu.Lock()
	r.homePage = hp
	r.mu.Unlock()
}

// HomePage returns the registered dashboard layout. nil = none configured.
func (r *Registry) HomePage() *metadata.HomePage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.homePage
}

func (r *Registry) Load(entities []*metadata.Entity, programs map[string]*ast.Program, registers []*metadata.Register, inforegs []*metadata.InfoRegister, enums []*metadata.Enum, constants []*metadata.Constant, reports []*report.Report, forms ...[]*printform.PrintForm) {
	newEntities := make(map[string]*metadata.Entity, len(entities))
	newSlugs := make(map[string]*metadata.Entity, len(entities))
	for _, e := range entities {
		newEntities[e.Name] = e
		newSlugs[strings.ToLower(e.Name)] = e
	}
	newRegs := make(map[string]*metadata.Register, len(registers))
	for _, reg := range registers {
		newRegs[reg.Name] = reg
	}
	newInfoRegs := make(map[string]*metadata.InfoRegister, len(inforegs))
	for _, ir := range inforegs {
		newInfoRegs[ir.Name] = ir
	}
	newEnums := make(map[string]*metadata.Enum, len(enums))
	for _, e := range enums {
		newEnums[e.Name] = e
	}
	newConsts := make(map[string]*metadata.Constant, len(constants))
	for _, c := range constants {
		newConsts[c.Name] = c
	}
	newReps := make(map[string]*report.Report, len(reports))
	for _, rep := range reports {
		newReps[rep.Name] = rep
	}
	newProcs := make(map[string]map[string]*ast.ProcedureDecl)
	for entityName, prog := range programs {
		pm := make(map[string]*ast.ProcedureDecl, len(prog.Procedures))
		for _, p := range prog.Procedures {
			pm[strings.ToLower(p.Name.Literal)] = p
		}
		newProcs[entityName] = pm
	}
	newPrintForms := make(map[string][]*printform.PrintForm)
	if len(forms) > 0 {
		for _, pf := range forms[0] {
			key := strings.ToLower(pf.Document)
			newPrintForms[key] = append(newPrintForms[key], pf)
		}
	}

	r.mu.Lock()
	r.entities = newEntities
	r.entitySlug = newSlugs
	r.registers = newRegs
	r.inforegs = newInfoRegs
	r.enums = newEnums
	r.constants = newConsts
	r.reports = newReps
	r.printForms = newPrintForms
	r.procs = newProcs
	r.mu.Unlock()
}

// GetPrintForms returns all print forms registered for an entity name (case-insensitive).
func (r *Registry) GetPrintForms(entityName string) []*printform.PrintForm {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.printForms[strings.ToLower(entityName)]
}

// LoadDSLPrintForms registers DSL (.os) print forms indexed by entity name.
// Замечание #10: при коллизии «один и тот же name для одного и того же
// document» с YAML-формой .os перебивает YAML, а YAML удаляется из реестра.
// В лог печатается warning, чтобы автор конфигурации понимал, что
// дубликат игнорируется. Должен вызываться ПОСЛЕ Load (где регистрируются
// YAML-формы); проверка идёт под одной блокировкой.
func (r *Registry) LoadDSLPrintForms(forms []*printform.DSLPrintForm) {
	m := make(map[string][]*printform.DSLPrintForm)
	for _, f := range forms {
		key := strings.ToLower(f.Document)
		m[key] = append(m[key], f)
	}
	r.mu.Lock()
	r.dslPrintForms = m
	// Дедуп YAML/.os коллизий: удаляем YAML, если есть .os с тем же именем.
	for entityKey, dslList := range m {
		yamlList := r.printForms[entityKey]
		if len(yamlList) == 0 {
			continue
		}
		var kept []*printform.PrintForm
		for _, yf := range yamlList {
			collides := false
			for _, df := range dslList {
				if strings.EqualFold(yf.Name, df.Name) {
					collides = true
					fmt.Fprintf(os.Stderr,
						"warning: print form %q for %s — YAML и .os коллизия, используется .os (LayoutPath=%s); YAML-вариант игнорируется\n",
						yf.Name, yf.Document, df.LayoutPath)
					break
				}
			}
			if !collides {
				kept = append(kept, yf)
			}
		}
		r.printForms[entityKey] = kept
	}
	r.mu.Unlock()
}

// GetDSLPrintForms returns all DSL print forms for an entity name (case-insensitive).
func (r *Registry) GetDSLPrintForms(entityName string) []*printform.DSLPrintForm {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dslPrintForms[strings.ToLower(entityName)]
}

// GetDSLPrintForm returns a specific DSL print form by entity and form name.
func (r *Registry) GetDSLPrintForm(entityName, pfName string) *printform.DSLPrintForm {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, f := range r.dslPrintForms[strings.ToLower(entityName)] {
		if strings.EqualFold(f.Name, pfName) {
			return f
		}
	}
	// Fallback: search all entities
	for _, forms := range r.dslPrintForms {
		for _, f := range forms {
			if strings.EqualFold(f.Name, pfName) {
				return f
			}
		}
	}
	return nil
}

func (r *Registry) GetReport(name string) *report.Report {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if rep, ok := r.reports[name]; ok {
		return rep
	}
	// case-insensitive fallback
	nl := strings.ToLower(name)
	for k, v := range r.reports {
		if strings.ToLower(k) == nl {
			return v
		}
	}
	return nil
}

func (r *Registry) Reports() []*report.Report {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*report.Report, 0, len(r.reports))
	for _, rep := range r.reports {
		out = append(out, rep)
	}
	return out
}

func (r *Registry) GetEntity(name string) *metadata.Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.entities[name]; ok {
		return e
	}
	return r.entitySlug[strings.ToLower(name)]
}

// GetEntityBySlug looks up by lowercase slug — O(1), URL-safe.
func (r *Registry) GetEntityBySlug(slug string) *metadata.Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entitySlug[strings.ToLower(slug)]
}

func (r *Registry) GetRegister(name string) *metadata.Register {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if reg, ok := r.registers[name]; ok {
		return reg
	}
	// case-insensitive fallback (URL routes use lowercase names)
	nl := strings.ToLower(name)
	for k, v := range r.registers {
		if strings.ToLower(k) == nl {
			return v
		}
	}
	return nil
}

func (r *Registry) Registers() []*metadata.Register {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.Register, 0, len(r.registers))
	for _, reg := range r.registers {
		out = append(out, reg)
	}
	return out
}

func (r *Registry) GetInfoRegister(name string) *metadata.InfoRegister {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if ir, ok := r.inforegs[name]; ok {
		return ir
	}
	nl := strings.ToLower(name)
	for k, v := range r.inforegs {
		if strings.ToLower(k) == nl {
			return v
		}
	}
	return nil
}

func (r *Registry) InfoRegisters() []*metadata.InfoRegister {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.InfoRegister, 0, len(r.inforegs))
	for _, ir := range r.inforegs {
		out = append(out, ir)
	}
	return out
}

func (r *Registry) GetEnum(name string) *metadata.Enum {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enums[name]
}

func (r *Registry) Enums() []*metadata.Enum {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.Enum, 0, len(r.enums))
	for _, e := range r.enums {
		out = append(out, e)
	}
	return out
}

func (r *Registry) GetConstantMeta(name string) *metadata.Constant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.constants[name]
}

func (r *Registry) Constants() []*metadata.Constant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.Constant, 0, len(r.constants))
	for _, c := range r.constants {
		out = append(out, c)
	}
	return out
}

func (r *Registry) Entities() []*metadata.Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.Entity, 0, len(r.entities))
	for _, e := range r.entities {
		out = append(out, e)
	}
	return out
}

// eventAliases maps lowercase English event names to their Russian equivalents.
var eventAliases = map[string]string{
	"onwrite": "призаписи",
	"onpost":  "обработкапроведения",
}

func (r *Registry) LoadModules(modules map[string]*ast.Program) {
	flat := make(map[string]*ast.ProcedureDecl)
	for _, prog := range modules {
		for _, p := range prog.Procedures {
			flat[strings.ToLower(p.Name.Literal)] = p
		}
	}
	r.mu.Lock()
	r.moduleProcs = flat
	r.mu.Unlock()
}

func (r *Registry) LoadProcessors(procs []*processor.Processor) {
	m := make(map[string]*processor.Processor, len(procs))
	for _, p := range procs {
		m[p.Name] = p
	}
	r.mu.Lock()
	r.processors = m
	r.mu.Unlock()
}

func (r *Registry) GetModuleProc(name string) *ast.ProcedureDecl {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.moduleProcs[strings.ToLower(name)]
}

// GetSiblingProc resolves a helper procedure declared in the same source
// file as currentFile. Lets `.proc.os` (processor entry-point) call
// helpers also defined inside it without flattening every entity proc
// into a global namespace (which would let arbitrary code invoke
// OnWrite/OnPost handlers by name).
//
// currentFile comes from interpreter.curFile (the file:line of the
// last executed statement), so the resolver naturally scopes to the
// currently running source.
func (r *Registry) GetSiblingProc(currentFile, name string) *ast.ProcedureDecl {
	if currentFile == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	low := strings.ToLower(name)
	for _, pm := range r.procs {
		for procLow, decl := range pm {
			if procLow == low && decl.Name.File == currentFile {
				return decl
			}
		}
	}
	return nil
}

func (r *Registry) Processors() []*processor.Processor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*processor.Processor, 0, len(r.processors))
	for _, p := range r.processors {
		out = append(out, p)
	}
	return out
}

func (r *Registry) GetProcessor(name string) *processor.Processor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.processors[name]; ok {
		return p
	}
	nl := strings.ToLower(name)
	for k, v := range r.processors {
		if strings.ToLower(k) == nl {
			return v
		}
	}
	return nil
}

func (r *Registry) LoadSubsystems(subs []*metadata.Subsystem) {
	r.mu.Lock()
	r.subsystems = subs
	r.mu.Unlock()
}

func (r *Registry) Subsystems() []*metadata.Subsystem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.Subsystem, len(r.subsystems))
	copy(out, r.subsystems)
	return out
}

func (r *Registry) GetSubsystem(name string) *metadata.Subsystem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.subsystems {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (r *Registry) LoadJournals(journals []*metadata.Journal) {
	m := make(map[string]*metadata.Journal, len(journals))
	for _, j := range journals {
		m[strings.ToLower(j.Name)] = j
	}
	r.mu.Lock()
	r.journals = m
	r.mu.Unlock()
}

func (r *Registry) GetJournal(name string) *metadata.Journal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.journals[strings.ToLower(name)]
}

func (r *Registry) Journals() []*metadata.Journal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.Journal, 0, len(r.journals))
	for _, j := range r.journals {
		out = append(out, j)
	}
	return out
}

func (r *Registry) GetProcedure(entityName, procName string) *ast.ProcedureDecl {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pm, ok := r.procs[entityName]
	if !ok {
		// case-insensitive fallback: DSL filename may differ in case from entity name
		nl := strings.ToLower(entityName)
		for k, v := range r.procs {
			if strings.ToLower(k) == nl {
				pm = v
				break
			}
		}
		if pm == nil {
			return nil
		}
	}
	procLower := strings.ToLower(procName)
	if p, ok := pm[procLower]; ok {
		return p
	}
	// try English alias → Russian proc name (both stored as lowercase)
	if ru, ok := eventAliases[procLower]; ok {
		return pm[ru]
	}
	return nil
}

func (r *Registry) LoadAccountRegisters(regs []*metadata.AccountRegister, charts []*metadata.ChartOfAccounts) {
	newRegs := make(map[string]*metadata.AccountRegister, len(regs))
	for _, ar := range regs {
		newRegs[strings.ToLower(ar.Name)] = ar
	}
	newCharts := make(map[string]*metadata.ChartOfAccounts, len(charts))
	for _, c := range charts {
		newCharts[strings.ToLower(c.Name)] = c
	}
	r.mu.Lock()
	r.accountRegs = newRegs
	r.chartsOfAccount = newCharts
	r.mu.Unlock()
}

func (r *Registry) GetAccountRegister(name string) *metadata.AccountRegister {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.accountRegs[strings.ToLower(name)]
}

func (r *Registry) AccountRegisters() []*metadata.AccountRegister {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.AccountRegister, 0, len(r.accountRegs))
	for _, ar := range r.accountRegs {
		out = append(out, ar)
	}
	return out
}

func (r *Registry) GetChartOfAccounts(name string) *metadata.ChartOfAccounts {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.chartsOfAccount[strings.ToLower(name)]
}

func (r *Registry) ChartsOfAccounts() []*metadata.ChartOfAccounts {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*metadata.ChartOfAccounts, 0, len(r.chartsOfAccount))
	for _, c := range r.chartsOfAccount {
		out = append(out, c)
	}
	return out
}
