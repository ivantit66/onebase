package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ivantit66/onebase/internal/configcheck"
)

// TestExamples_PassCheck — эталоны «пример_объекта» обязаны проходить ПОЛНЫЙ
// check (RunFull — та же линейка, которой генератор меряет свой результат) как
// связный проект: модель копирует их дословно, и устаревший формат молча учил
// бы её ошибкам. Стабы ниже — только объекты, на которые эталоны ссылаются,
// но которых нет среди самих эталонов (Контрагент, Склад и т.п.).
func TestExamples_PassCheck(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Все эталоны — по тем же путям, по которым их пишет генератор.
	write("catalogs/номенклатура.yaml", exCatalog)
	write("documents/реализациятоваров.yaml", exDocument)
	write("registers/остаткитоваров.yaml", exRegister)
	write("inforegs/ценыноменклатуры.yaml", exInfoReg)
	write("enums/статусзаказа.yaml", exEnum)
	write("reports/остаткитоваров.yaml", exReport)
	write("widgets/выручкамесяца.yaml", exWidget)
	write("accounts/основной.yaml", exChartOfAccounts)
	write("accountregs/бухучёт.yaml", exAccountReg)
	write("roles/менеджер.yaml", exRole)
	write("services/api.yaml", exService)
	write("scheduled/пересчётцен.yaml", exScheduled)
	write("pages/панельруководителя.yaml", exPage)
	write("journals/журналпродаж.yaml", exJournal)
	write("subsystems/продажи.yaml", exSubsystem)
	write("processors/пересчётцен.yaml", exProcessor)
	write("constants/константы.yaml", exConstants)
	write("printforms/счётнаоплату.layout.yaml", exPrintform)
	write("forms/реализациятоваров/ФормаОбъекта.form.yaml", exForm)
	write("src/реализациятоваров.posting.os", exPosting)

	// Стабы ссылок эталонов (в примерах их нет, но check требует существования).
	write("catalogs/единицаизмерения.yaml", "name: ЕдиницаИзмерения\nfields:\n  - {name: Наименование, type: string}\n")
	write("catalogs/контрагент.yaml", "name: Контрагент\nfields:\n  - {name: Наименование, type: string}\n")
	write("catalogs/склад.yaml", "name: Склад\nfields:\n  - {name: Наименование, type: string}\n")
	write("documents/заказпокупателя.yaml", "name: ЗаказПокупателя\nposting: true\nfields:\n  - {name: Дата, type: date}\n  - {name: Контрагент, type: reference:Контрагент}\n  - {name: Сумма, type: number}\n")
	write("registers/взаиморасчеты.yaml", "name: Взаиморасчеты\ndimensions:\n  - {name: Контрагент, type: reference:Контрагент}\nresources:\n  - {name: Сумма, type: number}\n")
	write("src/пересчётцен.proc.os", "Процедура Выполнить()\n    Сообщить(\"ок\");\nКонецПроцедуры\n")
	write("src/api.service.os", "Функция Здоровье(Запрос) Экспорт\n    Возврат \"ok\";\nКонецФункции\n\nФункция НоменклатураПоАртикулу(Запрос) Экспорт\n    Возврат \"ok\";\nКонецФункции\n")
	write("src/панельруководителя.page.os", "Процедура ПриФормировании(Страница, Параметры) Экспорт\n    Страница.Заголовок(\"Сводка\");\nКонецПроцедуры\n")

	res := configcheck.RunFull(dir)
	for _, is := range res.Issues {
		t.Errorf("check: [%s] %s: %s", is.Kind, is.File, is.Message)
	}
}

// TestKindAndExampleSynonymsAligned — каждый тип, который «создать_объект»
// умеет создавать, обязан отдавать эталон в «пример_объекта» под тем же
// синонимом (и наоборот для типов «только пример»). Регресс: «задание»
// принималось примером, но не создавалось.
func TestKindAndExampleSynonymsAligned(t *testing.T) {
	creatable := []string{
		"справочник", "документ", "регистр", "регистр сведений", "перечисление",
		"план счетов", "регистр бухгалтерии", "отчёт", "виджет", "журнал",
		"обработка", "страница", "подсистема", "роль", "сервис",
		"задание", "регламентное задание", "константы", "печатная форма",
	}
	for _, kind := range creatable {
		if _, ok := kindSubdir(kind); !ok {
			t.Errorf("kindSubdir(%q) = false — тип не создаётся", kind)
		}
		if _, ok := exampleForType(kind); !ok {
			t.Errorf("exampleForType(%q) = false — у создаваемого типа нет эталона", kind)
		}
	}
	// Только пример: создаются через «создать_файл» (двухфайловые/модульные).
	for _, kind := range []string{"форма", "проведение"} {
		if _, ok := exampleForType(kind); !ok {
			t.Errorf("exampleForType(%q) = false", kind)
		}
		if _, ok := kindSubdir(kind); ok {
			t.Errorf("kindSubdir(%q) = true — ожидался тип «только пример»", kind)
		}
	}
}
