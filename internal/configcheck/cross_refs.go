package configcheck

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/project"
)

// nameSet — множество имён объектов конфигурации (сравнение регистронезависимо,
// т.к. идентификаторы в OneBase case-insensitive).
type nameSet map[string]bool

func (s nameSet) add(n string)      { s[strings.ToLower(n)] = true }
func (s nameSet) has(n string) bool { return s[strings.ToLower(strings.TrimSpace(n))] }

// CheckCrossRefs проверяет, что ссылки между объектами указывают на
// существующие цели: документы в журналах/подсистемах/ролях, виджеты на
// главной странице, источник печатной формы, права ролей. Это ловит опечатки
// в именах, которые компиляция запросов не видит (объект просто не подключится).
func CheckCrossRefs(proj *project.Project, roles []*auth.Role) []Issue {
	docs := nameSet{}
	cats := nameSet{}
	entityByName := map[string]*metadata.Entity{}
	for _, e := range proj.Entities {
		entityByName[strings.ToLower(e.Name)] = e
		switch e.Kind {
		case metadata.KindDocument:
			docs.add(e.Name)
		case metadata.KindCatalog:
			cats.add(e.Name)
		}
	}
	reports := nameSet{}
	for _, r := range proj.Reports {
		reports.add(r.Name)
	}
	widgets := nameSet{}
	for _, w := range proj.Widgets {
		widgets.add(w.Name)
	}
	inforegs := nameSet{}
	for _, ir := range proj.InfoRegisters {
		inforegs.add(ir.Name)
	}
	// Регистры для ролей/подсистем: накопления + бухгалтерии (на оба ссылаются
	// в разделе registers).
	registers := nameSet{}
	for _, r := range proj.Registers {
		registers.add(r.Name)
	}
	for _, ar := range proj.AccountRegisters {
		registers.add(ar.Name)
	}
	processors := nameSet{}
	for _, p := range proj.Processors {
		processors.add(p.Name)
	}
	journals := nameSet{}
	for _, j := range proj.Journals {
		journals.add(j.Name)
	}

	var issues []Issue
	add := func(file, object, kind, msg string) {
		issues = append(issues, Issue{File: file, Object: object, Kind: kind, Message: msg})
	}
	// checkRefs проверяет список ссылок против набора; what — что за ссылка.
	checkRefs := func(file, object, kind string, refs []string, set nameSet, what string) {
		for _, r := range refs {
			if r != "" && !set.has(r) {
				add(file, object, kind, fmt.Sprintf("%s %q не найден(а)", what, r))
			}
		}
	}

	// Журналы → документы.
	for _, j := range proj.Journals {
		checkRefs("journals", j.Name, "Журнал", j.Documents, docs, "документ")
	}

	// Подсистемы → объекты в contents.
	for _, s := range proj.Subsystems {
		c := s.Contents
		checkRefs("subsystems", s.Name, "Подсистема", c.Documents, docs, "документ")
		checkRefs("subsystems", s.Name, "Подсистема", c.Catalogs, cats, "справочник")
		checkRefs("subsystems", s.Name, "Подсистема", c.Reports, reports, "отчёт")
		checkRefs("subsystems", s.Name, "Подсистема", c.InfoRegs, inforegs, "регистр сведений")
		checkRefs("subsystems", s.Name, "Подсистема", c.Registers, registers, "регистр")
		checkRefs("subsystems", s.Name, "Подсистема", c.Processors, processors, "обработка")
		checkRefs("subsystems", s.Name, "Подсистема", c.Journals, journals, "журнал")
	}

	// Главная страница (глобальная и подсистемные) → виджеты.
	checkHomePageWidgets := func(file, object string, hp *metadata.HomePage) {
		if hp == nil {
			return
		}
		for _, row := range hp.Rows {
			checkRefs(file, object, "Главная страница", row.Widgets, widgets, "виджет")
		}
		for _, w := range hp.Widgets {
			if w.Name != "" && !widgets.has(w.Name) {
				add(file, object, "Главная страница", fmt.Sprintf("виджет %q не найден", w.Name))
			}
		}
	}
	checkHomePageWidgets("config/home_page.yaml", "home_page", proj.HomePage)
	for _, s := range proj.Subsystems {
		checkHomePageWidgets("subsystems", s.Name, s.HomePage)
	}

	// Печатные формы → документ/справочник-источник и табличная часть.
	for _, pf := range proj.PrintForms {
		// «general» — зарезервированный источник для форм без привязки к
		// конкретному документу (сводные отчёты, рендерятся программно из
		// переданного контекста). Источник и table.source не проверяем.
		if strings.EqualFold(pf.Document, "general") {
			continue
		}
		if pf.Document != "" && !docs.has(pf.Document) && !cats.has(pf.Document) {
			add("printforms", pf.Name, "Печатная форма", fmt.Sprintf("источник %q не найден среди документов и справочников", pf.Document))
		}
		if pf.Table != nil && pf.Table.Source != "" {
			if e := entityByName[strings.ToLower(pf.Document)]; e != nil {
				found := false
				for _, tp := range e.TableParts {
					if strings.EqualFold(tp.Name, pf.Table.Source) {
						found = true
						break
					}
				}
				if !found {
					add("printforms", pf.Name, "Печатная форма", fmt.Sprintf("табличная часть %q не найдена в %q", pf.Table.Source, pf.Document))
				}
			}
		}
	}

	// Роли → объекты в правах.
	for _, r := range roles {
		checkRefs("roles", r.Name, "Роль", keys(r.Permissions.Catalogs), cats, "справочник")
		checkRefs("roles", r.Name, "Роль", keys(r.Permissions.Documents), docs, "документ")
		checkRefs("roles", r.Name, "Роль", keys(r.Permissions.Registers), registers, "регистр")
		checkRefs("roles", r.Name, "Роль", keys(r.Permissions.InfoRegs), inforegs, "регистр сведений")
		checkRefs("roles", r.Name, "Роль", keys(r.Permissions.Reports), reports, "отчёт")
	}

	return issues
}

// keys возвращает отсортированные ключи map прав (детерминированный порядок
// сообщений).
func keys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
