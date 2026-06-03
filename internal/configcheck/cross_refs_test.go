package configcheck

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/project"
)

// CheckCrossRefs должен ловить ссылки на несуществующие объекты (документы в
// журналах, виджеты на главной, права ролей) и не падать на легитимных случаях
// (печатная форма с document: general).
func TestCheckCrossRefs(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "documents", "заказ.yaml"), `name: Заказ
fields:
  - name: Наименование
    type: string`)
	// журнал ссылается на несуществующий документ
	mkFile(t, filepath.Join(dir, "journals", "ж.yaml"), `name: Ж
documents: [Заказ, НетТакого]
columns:
  - field: Наименование`)
	// печатная форма с general — не должна давать ошибку источника
	mkFile(t, filepath.Join(dir, "printforms", "общая.yaml"), `name: Общая
document: general
title: Отчёт
header: "x"`)
	// роль ссылается на несуществующий справочник
	mkFile(t, filepath.Join(dir, "roles", "р.yaml"), `name: Р
permissions:
  catalogs:
    НетТакогоСправочника: [read]`)

	proj, err := project.Load(dir)
	if err != nil {
		t.Fatalf("project.Load: %v", err)
	}
	defer proj.Close()
	roles, _ := auth.LoadRolesYAML(filepath.Join(dir, "roles"))

	issues := CheckCrossRefs(proj, roles)

	var hasJournal, hasRole, hasGeneral bool
	for _, i := range issues {
		if i.Kind == "Журнал" && strings.Contains(i.Message, "НетТакого") {
			hasJournal = true
		}
		if i.Kind == "Роль" && strings.Contains(i.Message, "НетТакогоСправочника") {
			hasRole = true
		}
		if i.Object == "Общая" {
			hasGeneral = true
		}
	}
	if !hasJournal {
		t.Errorf("ожидалась ошибка журнала о несуществующем документе: %+v", issues)
	}
	if !hasRole {
		t.Errorf("ожидалась ошибка роли о несуществующем справочнике: %+v", issues)
	}
	if hasGeneral {
		t.Errorf("печатная форма с document: general не должна давать ошибку: %+v", issues)
	}
}
