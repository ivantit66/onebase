package configcheck

// Валидация планов обмена (план 86): дубли имён, состав ссылается на
// существующие сущности, парная топология, коды узлов непусты и уникальны,
// известное правило разрешения конфликтов.

import (
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/project"
)

// CheckExchangePlans проверяет exchange/*.yaml против загруженных сущностей.
func CheckExchangePlans(proj *project.Project) []Issue {
	var issues []Issue
	add := func(object, msg string) {
		issues = append(issues, Issue{File: "exchange", Object: object, Kind: "План обмена", Message: msg})
	}

	// Индекс сущностей: lowercase имя → набор видов (справочник/документ).
	kindsByName := map[string]map[metadata.Kind]bool{}
	for _, e := range proj.Entities {
		low := strings.ToLower(e.Name)
		if kindsByName[low] == nil {
			kindsByName[low] = map[metadata.Kind]bool{}
		}
		kindsByName[low][e.Kind] = true
	}
	constByName := map[string]bool{}
	for _, c := range proj.Constants {
		constByName[strings.ToLower(c.Name)] = true
	}
	infoRegByName := map[string]bool{}
	infoRegPeriodic := map[string]bool{}
	for _, ir := range proj.InfoRegisters {
		infoRegByName[strings.ToLower(ir.Name)] = true
		infoRegPeriodic[strings.ToLower(ir.Name)] = ir.Periodic
	}

	seenName := map[string]bool{}
	for _, plan := range proj.ExchangePlans {
		if strings.TrimSpace(plan.Name) == "" {
			add("(без имени)", "не задано имя плана обмена (name)")
			continue
		}
		low := strings.ToLower(plan.Name)
		if seenName[low] {
			add(plan.Name, fmt.Sprintf("имя плана обмена %q дублируется", plan.Name))
		}
		seenName[low] = true

		// Правило разрешения конфликтов.
		switch plan.Conflict {
		case metadata.ConflictByTime, metadata.ConflictByNodePriority, metadata.ConflictByHook:
		default:
			add(plan.Name, fmt.Sprintf("неизвестное правило конфликта %q (by_time|by_node_priority|hook)", plan.Conflict))
		}
		if plan.Conflict == metadata.ConflictByHook && !hasModuleProc(proj, "ПриКонфликтеОбмена") {
			add(plan.Name, "правило hook требует процедуру ПриКонфликтеОбмена Экспорт в общем модуле (src/*.module.os)")
		}

		// Допустимы прямой обмен пары баз и многоузловая звезда с одним хабом.
		// Плоская топология 3+ узлов со скалярной версией не гарантирует сходимость.
		if len(plan.Nodes) < 2 {
			add(plan.Name, "план обмена должен содержать минимум два узла (nodes)")
		}
		seenCode := map[string]bool{}
		hubCount, spokeCount := 0, 0
		for _, n := range plan.Nodes {
			if strings.TrimSpace(n.Code) == "" {
				add(plan.Name, "у узла не задан код (code)")
				continue
			}
			codeLow := strings.ToLower(n.Code)
			if seenCode[codeLow] {
				add(plan.Name, fmt.Sprintf("код узла %q дублируется", n.Code))
			}
			seenCode[codeLow] = true
			switch n.Role {
			case "", metadata.RoleHub, metadata.RoleSpoke:
			default:
				add(plan.Name, fmt.Sprintf("узел %q: неизвестная роль %q (допустимо: hub, spoke или пусто)", n.Code, n.Role))
			}
			if n.Role == metadata.RoleHub {
				hubCount++
			}
			if n.Role == metadata.RoleSpoke {
				spokeCount++
			}
		}
		// Топология «звезда»: спицам нужен хотя бы один хаб для маршрутизации.
		if spokeCount > 0 && hubCount == 0 {
			add(plan.Name, "в топологии есть узлы-спицы (spoke), но нет ни одного хаба (hub) — спицам некуда регистрировать изменения")
		}
		if hubCount > 1 {
			add(plan.Name, "топология «звезда» поддерживает ровно один хаб (hub)")
		}
		if len(plan.Nodes) > 2 && hubCount == 0 {
			add(plan.Name, "план с тремя и более узлами должен использовать топологию «звезда» с одним хабом (hub)")
		}

		// Состав: непуст и ссылается на существующие сущности нужного вида.
		if len(plan.ParsedContent()) == 0 {
			add(plan.Name, "пустой состав обмена (content)")
		}
		for _, c := range plan.ParsedContent() {
			switch c.Category {
			case metadata.ContentConstant:
				if !constByName[strings.ToLower(c.Name)] {
					add(plan.Name, fmt.Sprintf("в составе указана несуществующая константа %q", c.Name))
				}
			case metadata.ContentInfoRegister:
				low := strings.ToLower(c.Name)
				if !infoRegByName[low] {
					add(plan.Name, fmt.Sprintf("в составе указан несуществующий регистр сведений %q", c.Name))
				} else if infoRegPeriodic[low] {
					add(plan.Name, fmt.Sprintf("регистр сведений %q периодический — синхронизация периодических регистров пока не поддержана (обмен его пропустит)", c.Name))
				}
			default: // metadata.ContentEntity
				kinds, ok := kindsByName[strings.ToLower(c.Name)]
				if !ok {
					add(plan.Name, fmt.Sprintf("в составе указана несуществующая сущность %q", c.Name))
					continue
				}
				if c.Kind != "" && !kinds[c.Kind] {
					add(plan.Name, fmt.Sprintf("в составе %q — нет %s с таким именем", c.Name, kindWord(c.Kind)))
				}
			}
		}
	}
	return issues
}

// hasModuleProc сообщает, объявлена ли процедура с таким именем в каком-либо
// общем модуле (.module.os) — регистронезависимо.
func hasModuleProc(proj *project.Project, name string) bool {
	for _, prog := range proj.Modules {
		for _, p := range prog.Procedures {
			if strings.EqualFold(p.Name.Literal, name) {
				return true
			}
		}
	}
	return false
}

func kindWord(k metadata.Kind) string {
	if k == metadata.KindDocument {
		return "документа"
	}
	return "справочника"
}
