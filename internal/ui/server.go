package ui

import (
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/debugger"
	"github.com/ivantit66/onebase/internal/mailer"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/scheduler"
	"github.com/ivantit66/onebase/internal/storage"
)

// Config holds static info shown in «О программе».
type Config struct {
	AppName       string
	AppVersion    string
	DSN           string
	PlatVersion   string
	Mailer        *mailer.Mailer
	MaxFileSizeMB int // 0 = use default 50
}

type Server struct {
	reg              *runtime.Registry
	store            *storage.DB
	interp           *interpreter.Interpreter
	authRepo         *auth.Repo
	cfg              Config
	sched            *scheduler.Scheduler
	mailer           *mailer.Mailer
	maxFileSizeBytes int64
	globalDebug      *debugger.GlobalDebugController
	messages         *MessageStore
}

func New(reg *runtime.Registry, store *storage.DB, interp *interpreter.Interpreter, authRepo *auth.Repo, cfg Config, sched *scheduler.Scheduler) *Server {
	maxBytes := int64(cfg.MaxFileSizeMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 50 * 1024 * 1024
	}
	s := &Server{reg: reg, store: store, interp: interp, authRepo: authRepo, cfg: cfg, sched: sched, mailer: cfg.Mailer, maxFileSizeBytes: maxBytes, globalDebug: debugger.NewGlobalDebugController(), messages: NewMessageStore()}
	if sched != nil {
		sched.SetMessageSink(func(userID, text string) { s.messages.Push(userID, text) })
	}
	return s
}

// Messages returns the per-user message store (used to inject Сообщить sink).
func (s *Server) Messages() *MessageStore { return s.messages }

// GlobalDebug returns the global debug controller for the server.
func (s *Server) GlobalDebug() *debugger.GlobalDebugController { return s.globalDebug }

func (s *Server) Mount(r chi.Router) {
	r.Get("/ui", s.index)
	r.Get("/ui/", s.index)
	r.Get("/ui/{kind}/{entity}", s.list)
	r.Get("/ui/{kind}/{entity}/new", s.form)
	r.Post("/ui/{kind}/{entity}/new", s.submit)
	r.Get("/ui/{kind}/{entity}/{id}", s.formEdit)
	r.Post("/ui/{kind}/{entity}/{id}", s.submitEdit)
	r.Get("/ui/register/{name}", s.registerMovements)
	r.Get("/ui/register/{name}/balances", s.registerBalances)
	r.Get("/ui/inforeg/{name}", s.infoRegList)
	r.Get("/ui/inforeg/{name}/new", s.infoRegForm)
	r.Post("/ui/inforeg/{name}/new", s.infoRegSubmit)
	r.Post("/ui/inforeg/{name}/delete", s.infoRegDelete)
	r.Get("/ui/report/{name}", s.reportForm)
	r.Post("/ui/report/{name}", s.reportRun)
	r.Get("/ui/processor/{name}", s.processorForm)
	r.Post("/ui/processor/{name}", s.processorRun)

	// Document posting
	r.Post("/ui/{kind}/{entity}/{id}/post", s.postDocument)
	r.Post("/ui/{kind}/{entity}/{id}/unpost", s.unpostDocument)

	// Delete record / mark for deletion
	r.Post("/ui/{kind}/{entity}/{id}/delete", s.deleteRecord)
	r.Post("/ui/{kind}/{entity}/delete-marked", s.deleteMarked)

	// Global delete-marked page
	r.Get("/ui/delete-marked", s.deleteMarkedAll)
	r.Post("/ui/delete-marked", s.deleteMarkedAll)

	// Admin: user management
	r.Get("/ui/admin/users", s.adminUsers)
	r.Get("/ui/admin/users/new", s.adminUserNew)
	r.Post("/ui/admin/users/new", s.adminUserCreate)
	r.Post("/ui/admin/users/{id}/delete", s.adminUserDelete)

	// Admin: active sessions
	r.Get("/ui/admin/sessions", s.adminSessions)
	r.Post("/ui/admin/sessions/{login}/kick", s.adminKickUser)

	// Admin: roles
	r.Get("/ui/admin/roles", s.adminRoles)
	r.Get("/ui/admin/users/{id}/roles", s.adminUserRoles)
	r.Post("/ui/admin/users/{id}/roles", s.adminUserRolesUpdate)

	// Admin: audit log
	r.Get("/ui/admin/audit", s.adminAudit)
	r.Get("/ui/{kind}/{entity}/{id}/history", s.recordHistory)

	// Admin: orphan movements cleanup
	r.Get("/ui/admin/cleanup", s.adminCleanup)
	r.Post("/ui/admin/cleanup", s.adminCleanup)

	// Admin: scheduled jobs
	r.Get("/ui/admin/scheduled", s.scheduledList)
	r.Get("/ui/admin/scheduled/{name}", s.scheduledDetail)
	r.Post("/ui/admin/scheduled/{name}/run-now", s.scheduledRunNow)

	// Account registers
	r.Get("/ui/accounts/{plan}", s.accountsList)
	r.Get("/ui/accountreg/{name}", s.accountRegMovements)
	r.Get("/ui/accountreg/{name}/balances", s.accountRegBalances)

	// Query builder
	r.Get("/ui/query-builder", s.queryBuilder)

	// Developer tools
	r.Get("/ui/dev/query-console", s.queryConsolePage)
	r.Post("/ui/dev/query-exec", s.queryConsoleExec)
	r.Post("/ui/dev/query-analyze", s.queryConsoleAnalyze)
	r.Get("/ui/dev/entity-search", s.devEntitySearch)
	r.Get("/ui/dev/code-console", s.codeConsolePage)
	r.Post("/ui/dev/code-exec", s.codeConsoleExec)

	// All functions (admin only)
	r.Get("/ui/all-functions", s.allFunctions)

	// Constants
	r.Get("/ui/constants", s.constantsList)
	r.Post("/ui/constants", s.constantsSave)

	// Print forms
	r.Get("/ui/{kind}/{entity}/{id}/print/{form}", s.printDocument)
	r.Get("/ui/{kind}/{entity}/{id}/print/{form}/pdf", s.printDocumentPDF)
	r.Get("/ui/{kind}/{entity}/{id}/print-dsl/{pfName}", s.printDocumentDSLPF)

	// Attachments
	r.Get("/ui/{kind}/{entity}/{id}/attachments", s.attachmentsList)
	r.Post("/ui/{kind}/{entity}/{id}/attachments", s.attachmentUpload)
	r.Get("/ui/attachments/{aid}/download", s.attachmentDownload)
	r.Post("/ui/attachments/{aid}/delete", s.attachmentDelete)

	// Excel exports
	r.Get("/ui/{kind}/{entity}/excel", s.listExcel)
	r.Get("/ui/report/{name}/excel", s.reportExcel)
	r.Get("/ui/journal/{name}/excel", s.journalExcel)

	// Journals
	r.Get("/ui/journal/{name}", s.journalList)

	// Оборудование кассира (мост браузер→локальный device-agent)
	r.Get("/ui/pos", s.posPage)
	r.Get("/ui/settings/agent", s.agentSettings)

	// About
	r.Get("/ui/about", s.about)

	// Messages panel
	r.Get("/ui/messages", s.messagesList)
	r.Post("/ui/messages/clear", s.messagesClear)
}


// MountDebug registers debug API routes WITHOUT auth middleware.
// Must be called outside the auth-protected group so the configurator
// (running on a different port) can reach the endpoints cross-origin.
func (s *Server) MountDebug(r chi.Router) {
	r.Route("/debug/global", func(r chi.Router) {
		r.Use(corsMiddleware)
		r.Post("/enable", s.debugGlobalEnable)
		r.Post("/disable", s.debugGlobalDisable)
		r.Get("/status", s.debugGlobalStatus)
		r.Post("/breakpoint", s.debugGlobalBreakpoint)
		r.Post("/continue", s.debugGlobalContinue)
		r.Post("/step", s.debugGlobalStep)
		r.Post("/stop", s.debugGlobalStop)
		r.Post("/evaluate", s.debugGlobalEvaluate)
	})
}
type navSection struct {
	Kind     string
	Entities []*metadata.Entity
}

type navItem struct {
	Label string
	URL   string
}

type navGroup struct {
	Kind  string
	Items []navItem
}

func (s *Server) buildNav(sub string) []navGroup {
	subs := s.reg.Subsystems()
	if len(subs) > 0 {
		cur := s.reg.GetSubsystem(sub)
		if cur == nil {
			cur = subs[0]
		}
		return s.buildNavForSubsystem(cur, sub)
	}
	return s.buildFlatNav()
}

func strSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func (s *Server) buildNavForSubsystem(sub *metadata.Subsystem, subName string) []navGroup {
	q := "?subsystem=" + subName
	var nav []navGroup

	if len(sub.Contents.Catalogs) > 0 || len(sub.Contents.Documents) > 0 {
		catSet := strSet(sub.Contents.Catalogs)
		docSet := strSet(sub.Contents.Documents)
		entities := s.reg.Entities()
		sort.Slice(entities, func(i, j int) bool { return entities[i].Name < entities[j].Name })
		var catalogs, documents []navItem
		for _, e := range entities {
			url := "/ui/" + strings.ToLower(string(e.Kind)) + "/" + e.Name + q
			if e.Kind == metadata.KindCatalog && catSet[e.Name] {
				catalogs = append(catalogs, navItem{Label: e.Name, URL: url})
			} else if e.Kind == metadata.KindDocument && docSet[e.Name] {
				documents = append(documents, navItem{Label: e.Name, URL: url})
			}
		}
		if len(catalogs) > 0 {
			nav = append(nav, navGroup{Kind: "Справочники", Items: catalogs})
		}
		if len(documents) > 0 {
			nav = append(nav, navGroup{Kind: "Документы", Items: documents})
		}
	}

	if len(sub.Contents.Registers) > 0 {
		regSet := strSet(sub.Contents.Registers)
		registers := s.reg.Registers()
		sort.Slice(registers, func(i, j int) bool { return registers[i].Name < registers[j].Name })
		var regItems []navItem
		for _, reg := range registers {
			if !regSet[reg.Name] {
				continue
			}
			regItems = append(regItems, navItem{
				Label: reg.Name + " (движения)",
				URL:   "/ui/register/" + strings.ToLower(reg.Name) + q,
			})
			regItems = append(regItems, navItem{
				Label: reg.Name + " (остатки)",
				URL:   "/ui/register/" + strings.ToLower(reg.Name) + "/balances" + q,
			})
		}
		if len(regItems) > 0 {
			nav = append(nav, navGroup{Kind: "Регистры", Items: regItems})
		}
	}

	if len(sub.Contents.InfoRegs) > 0 {
		irSet := strSet(sub.Contents.InfoRegs)
		inforegs := s.reg.InfoRegisters()
		sort.Slice(inforegs, func(i, j int) bool { return inforegs[i].Name < inforegs[j].Name })
		var irItems []navItem
		for _, ir := range inforegs {
			if !irSet[ir.Name] {
				continue
			}
			label := ir.Name
			if ir.Periodic {
				label += " (периодический)"
			}
			irItems = append(irItems, navItem{Label: label, URL: "/ui/inforeg/" + strings.ToLower(ir.Name) + q})
		}
		if len(irItems) > 0 {
			nav = append(nav, navGroup{Kind: "Регистры сведений", Items: irItems})
		}
	}

	if len(sub.Contents.Reports) > 0 {
		repSet := strSet(sub.Contents.Reports)
		reps := s.reg.Reports()
		sort.Slice(reps, func(i, j int) bool { return reps[i].Name < reps[j].Name })
		var repItems []navItem
		for _, rep := range reps {
			if !repSet[rep.Name] {
				continue
			}
			label := rep.Title
			if label == "" {
				label = rep.Name
			}
			repItems = append(repItems, navItem{Label: label, URL: "/ui/report/" + strings.ToLower(rep.Name) + q})
		}
		if len(repItems) > 0 {
			nav = append(nav, navGroup{Kind: "Отчёты", Items: repItems})
		}
	}

	if len(sub.Contents.Processors) > 0 {
		procSet := strSet(sub.Contents.Processors)
		procs := s.reg.Processors()
		sort.Slice(procs, func(i, j int) bool { return procs[i].Name < procs[j].Name })
		var procItems []navItem
		for _, proc := range procs {
			if !procSet[proc.Name] {
				continue
			}
			label := proc.Title
			if label == "" {
				label = proc.Name
			}
			procItems = append(procItems, navItem{Label: label, URL: "/ui/processor/" + strings.ToLower(proc.Name) + q})
		}
		if len(procItems) > 0 {
			nav = append(nav, navGroup{Kind: "Обработки", Items: procItems})
		}
	}

	if len(sub.Contents.Journals) > 0 {
		jSet := strSet(sub.Contents.Journals)
		journals := s.reg.Journals()
		sort.Slice(journals, func(i, j int) bool { return journals[i].Name < journals[j].Name })
		var jItems []navItem
		for _, j2 := range journals {
			if !jSet[j2.Name] {
				continue
			}
			label := j2.Title
			if label == "" {
				label = j2.Name
			}
			jItems = append(jItems, navItem{Label: label, URL: "/ui/journal/" + strings.ToLower(j2.Name) + q})
		}
		if len(jItems) > 0 {
			nav = append(nav, navGroup{Kind: "Журналы", Items: jItems})
		}
	}

	return nav
}

func (s *Server) buildFlatNav() []navGroup {
	entities := s.reg.Entities()
	sort.Slice(entities, func(i, j int) bool { return entities[i].Name < entities[j].Name })

	var catalogs, documents []navItem
	for _, e := range entities {
		url := "/ui/" + strings.ToLower(string(e.Kind)) + "/" + e.Name
		item := navItem{Label: e.Name, URL: url}
		if e.Kind == metadata.KindCatalog {
			catalogs = append(catalogs, item)
		} else {
			documents = append(documents, item)
		}
	}

	registers := s.reg.Registers()
	sort.Slice(registers, func(i, j int) bool { return registers[i].Name < registers[j].Name })
	var regItems []navItem
	for _, reg := range registers {
		regItems = append(regItems, navItem{
			Label: reg.Name + " (движения)",
			URL:   "/ui/register/" + strings.ToLower(reg.Name),
		})
		regItems = append(regItems, navItem{
			Label: reg.Name + " (остатки)",
			URL:   "/ui/register/" + strings.ToLower(reg.Name) + "/balances",
		})
	}

	var nav []navGroup
	if len(catalogs) > 0 {
		nav = append(nav, navGroup{Kind: "Справочники", Items: catalogs})
	}
	if len(documents) > 0 {
		nav = append(nav, navGroup{Kind: "Документы", Items: documents})
	}
	if len(regItems) > 0 {
		nav = append(nav, navGroup{Kind: "Регистры", Items: regItems})
	}

	inforegs := s.reg.InfoRegisters()
	sort.Slice(inforegs, func(i, j int) bool { return inforegs[i].Name < inforegs[j].Name })
	var inforegItems []navItem
	for _, ir := range inforegs {
		label := ir.Name
		if ir.Periodic {
			label += " (периодический)"
		}
		inforegItems = append(inforegItems, navItem{
			Label: label,
			URL:   "/ui/inforeg/" + strings.ToLower(ir.Name),
		})
	}
	if len(inforegItems) > 0 {
		nav = append(nav, navGroup{Kind: "Регистры сведений", Items: inforegItems})
	}

	reps := s.reg.Reports()
	sort.Slice(reps, func(i, j int) bool { return reps[i].Name < reps[j].Name })
	var repItems []navItem
	for _, rep := range reps {
		label := rep.Title
		if label == "" {
			label = rep.Name
		}
		repItems = append(repItems, navItem{
			Label: label,
			URL:   "/ui/report/" + strings.ToLower(rep.Name),
		})
	}
	if len(repItems) > 0 {
		nav = append(nav, navGroup{Kind: "Отчёты", Items: repItems})
	}

	procs := s.reg.Processors()
	sort.Slice(procs, func(i, j int) bool { return procs[i].Name < procs[j].Name })
	var procItems []navItem
	for _, proc := range procs {
		label := proc.Title
		if label == "" {
			label = proc.Name
		}
		procItems = append(procItems, navItem{
			Label: label,
			URL:   "/ui/processor/" + strings.ToLower(proc.Name),
		})
	}
	if len(procItems) > 0 {
		nav = append(nav, navGroup{Kind: "Обработки", Items: procItems})
	}

	journals := s.reg.Journals()
	sort.Slice(journals, func(i, j int) bool { return journals[i].Name < journals[j].Name })
	var journalItems []navItem
	for _, j := range journals {
		label := j.Title
		if label == "" {
			label = j.Name
		}
		journalItems = append(journalItems, navItem{Label: label, URL: "/ui/journal/" + strings.ToLower(j.Name)})
	}
	if len(journalItems) > 0 {
		nav = append(nav, navGroup{Kind: "Журналы", Items: journalItems})
	}

	if len(s.reg.Constants()) > 0 {
		nav = append(nav, navGroup{Kind: "Настройки", Items: []navItem{
			{Label: "Константы", URL: "/ui/constants"},
		}})
	}
	return nav
}
