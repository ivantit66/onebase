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

// Проверка полей: колонки журнала, не резолвящиеся ни в одном документе, и поля
// таблицы печатной формы, отсутствующие в табличной части.
func TestCheckCrossRefs_Fields(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "documents", "реализация.yaml"), `name: Реализация
fields:
  - name: Дата
    type: date
  - name: Покупатель
    type: reference:Контрагент
tableparts:
  - name: Товары
    fields:
      - name: Номенклатура
        type: reference:Номенклатура
      - name: Сумма
        type: number`)
	mkFile(t, filepath.Join(dir, "catalogs", "контрагент.yaml"), `name: Контрагент
fields:
  - name: Наименование
    type: string`)
	// журнал: Дата резолвится, НетПоля — нет
	mkFile(t, filepath.Join(dir, "journals", "ж.yaml"), `name: Ж
documents: [Реализация]
columns:
  - field: Дата
  - field: НетПоля`)
	// печатная форма: Сумма есть в ТЧ, НетКолонки — нет; @row допустимо
	mkFile(t, filepath.Join(dir, "printforms", "накладная.yaml"), `name: Накладная
document: Реализация
table:
  source: Товары
  columns:
    - field: "@row"
    - field: Сумма
    - field: НетКолонки
  totals:
    - field: Сумма
      sum: true`)

	proj, err := project.Load(dir)
	if err != nil {
		t.Fatalf("project.Load: %v", err)
	}
	defer proj.Close()

	issues := CheckCrossRefs(proj, nil)
	var jBad, jGood, pfBad, pfGood, pfRow bool
	for _, i := range issues {
		switch {
		case i.Kind == "Журнал" && strings.Contains(i.Message, "НетПоля"):
			jBad = true
		case i.Kind == "Журнал" && strings.Contains(i.Message, `"Дата"`):
			jGood = true
		case i.Kind == "Печатная форма" && strings.Contains(i.Message, "НетКолонки"):
			pfBad = true
		case i.Kind == "Печатная форма" && strings.Contains(i.Message, `"Сумма"`):
			pfGood = true
		case i.Kind == "Печатная форма" && strings.Contains(i.Message, "@row"):
			pfRow = true
		}
	}
	if !jBad {
		t.Errorf("ожидалась ошибка журнала о колонке НетПоля: %+v", issues)
	}
	if jGood {
		t.Errorf("колонка Дата резолвится — ошибки быть не должно: %+v", issues)
	}
	if !pfBad {
		t.Errorf("ожидалась ошибка печатной формы о колонке НетКолонки: %+v", issues)
	}
	if pfGood || pfRow {
		t.Errorf("Сумма и @row валидны — ошибки быть не должно: %+v", issues)
	}
}
