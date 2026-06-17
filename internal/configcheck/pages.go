package configcheck

// Валидация страниц (план 66): у каждой pages/*.yaml должен быть обработчик
// src/<имя>.page.os с процедурой ПриФормировании. Тексты запросов внутри
// обработчика компилирует CheckModuleQueries (он читает все src/*.os).

import (
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/project"
)

// CheckPages проверяет pages/*.yaml против загруженных обработчиков.
func CheckPages(proj *project.Project) []Issue {
	var issues []Issue
	add := func(object, msg string) {
		issues = append(issues, Issue{File: "pages", Object: object, Kind: "Страница", Message: msg})
	}

	// PagePrograms ключуется капитализированным именем файла — ищем
	// регистронезависимо (как сервисы, план 61/66): страницы хранятся отдельно
	// от Programs, чтобы не конфликтовать с модулем одноимённого объекта.
	progByLower := map[string]*ast.Program{}
	for name, prog := range proj.PagePrograms {
		progByLower[strings.ToLower(name)] = prog
	}

	seen := map[string]bool{}
	for _, pg := range proj.Pages {
		if strings.TrimSpace(pg.Name) == "" {
			add("(без имени)", "не задано имя страницы (name)")
			continue
		}
		low := strings.ToLower(pg.Name)
		if seen[low] {
			add(pg.Name, "имя страницы дублируется")
		}
		seen[low] = true

		prog, ok := progByLower[low]
		if !ok {
			add(pg.Name, fmt.Sprintf("не найден обработчик src/%s.page.os", strings.ToLower(pg.Name)))
			continue
		}
		hasHandler := false
		for _, p := range prog.Procedures {
			if strings.EqualFold(p.Name.Literal, "ПриФормировании") {
				hasHandler = true
				break
			}
		}
		if !hasHandler {
			add(pg.Name, fmt.Sprintf("в src/%s.page.os нет процедуры ПриФормировании(Страница, Параметры)", strings.ToLower(pg.Name)))
		}
	}
	return issues
}
