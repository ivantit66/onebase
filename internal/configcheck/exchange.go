package configcheck

// Валидация планов обмена (план 86): дубли имён, состав ссылается на
// существующие сущности, коды узлов непусты и уникальны, известное правило
// разрешения конфликтов.

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

		// Узлы: минимум два участника, непустые уникальные коды.
		if len(plan.Nodes) < 2 {
			add(plan.Name, "план обмена должен содержать минимум два узла (nodes)")
		}
		seenCode := map[string]bool{}
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
		}

		// Состав: непуст и ссылается на существующие сущности нужного вида.
		if len(plan.ParsedContent()) == 0 {
			add(plan.Name, "пустой состав обмена (content)")
		}
		for _, c := range plan.ParsedContent() {
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
	return issues
}

func kindWord(k metadata.Kind) string {
	if k == metadata.KindDocument {
		return "документа"
	}
	return "справочника"
}
