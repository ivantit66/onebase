package parser1c

// Регрессионные тесты ядра парсера выгрузки 1С. Фиксируют текущее поведение
// разбора v8.3 MDClasses XML и распознавания типов, чтобы изменения формата
// или рефакторинг не ломали конвертацию незаметно.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseType(t *testing.T) {
	cases := []struct {
		name      string
		in        []string
		primary   string
		refObject string
		composite bool
	}{
		{"пусто → string", nil, "string", "", false},
		{"примитив xs:string", []string{"xs:string"}, "xs:string", "", false},
		{"ссылка на справочник", []string{"cfg:CatalogRef.Контрагенты"}, "cfg:CatalogRef.Контрагенты", "Контрагенты", false},
		{"ссылка на документ", []string{"DocumentRef.Реализация"}, "DocumentRef.Реализация", "Реализация", false},
		{"составной тип", []string{"xs:string", "cfg:CatalogRef.Контрагенты"}, "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseType(c.in)
			if c.composite {
				if !got.Composite {
					t.Fatalf("ожидался составной тип, получено %+v", got)
				}
				if len(got.AllTypes) != len(c.in) {
					t.Fatalf("AllTypes=%v, ожидалось %v", got.AllTypes, c.in)
				}
				return
			}
			if got.Primary != c.primary {
				t.Errorf("Primary=%q, ожидалось %q", got.Primary, c.primary)
			}
			if got.RefObject != c.refObject {
				t.Errorf("RefObject=%q, ожидалось %q", got.RefObject, c.refObject)
			}
		})
	}
}

// writeV83 создаёт пару «директория объекта + sibling .xml» в стиле выгрузки
// 1С v8.3, как её ожидает ParseDir.
func writeV83(t *testing.T, kindDir, objName, xml string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(kindDir, objName), 0o755); err != nil {
		t.Fatalf("mkdir object: %v", err)
	}
	if err := os.WriteFile(filepath.Join(kindDir, objName+".xml"), []byte(xml), 0o644); err != nil {
		t.Fatalf("write xml: %v", err)
	}
}

const catalogXML = `<?xml version="1.0" encoding="UTF-8"?>
<MetaDataObject>
  <Catalog>
    <Properties><Name>Контрагенты</Name></Properties>
    <ChildObjects>
      <Attribute><Properties>
        <Name>ИНН</Name>
        <Type><Type xmlns="http://v8.1c.ru/8.1/data/core">xs:string</Type></Type>
      </Properties></Attribute>
    </ChildObjects>
  </Catalog>
</MetaDataObject>`

const documentXML = `<?xml version="1.0" encoding="UTF-8"?>
<MetaDataObject>
  <Document>
    <Properties><Name>РеализацияТоваров</Name></Properties>
    <ChildObjects>
      <Attribute><Properties>
        <Name>Контрагент</Name>
        <Type><Type xmlns="http://v8.1c.ru/8.1/data/core">cfg:CatalogRef.Контрагенты</Type></Type>
      </Properties></Attribute>
      <TabularSection>
        <Properties><Name>Товары</Name></Properties>
        <ChildObjects>
          <Attribute><Properties>
            <Name>Количество</Name>
            <Type><Type xmlns="http://v8.1c.ru/8.1/data/core">xs:decimal</Type></Type>
          </Properties></Attribute>
        </ChildObjects>
      </TabularSection>
    </ChildObjects>
  </Document>
</MetaDataObject>`

func TestParseDirV83(t *testing.T) {
	src := t.TempDir()
	writeV83(t, filepath.Join(src, "Catalogs"), "Контрагенты", catalogXML)
	writeV83(t, filepath.Join(src, "Documents"), "РеализацияТоваров", documentXML)

	dump, err := ParseDir(src)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}

	// Справочник.
	if len(dump.Catalogs) != 1 {
		t.Fatalf("ожидался 1 справочник, получено %d", len(dump.Catalogs))
	}
	cat := dump.Catalogs[0]
	if cat.Name != "Контрагенты" {
		t.Errorf("имя справочника: %q", cat.Name)
	}
	if len(cat.Attributes) != 1 || cat.Attributes[0].Name != "ИНН" {
		t.Fatalf("реквизиты справочника: %+v", cat.Attributes)
	}
	if cat.Attributes[0].Type.Primary != "xs:string" {
		t.Errorf("тип ИНН: %+v", cat.Attributes[0].Type)
	}

	// Документ с табличной частью и ссылочным реквизитом.
	if len(dump.Documents) != 1 {
		t.Fatalf("ожидался 1 документ, получено %d", len(dump.Documents))
	}
	doc := dump.Documents[0]
	if doc.Name != "РеализацияТоваров" {
		t.Errorf("имя документа: %q", doc.Name)
	}
	if len(doc.Attributes) != 1 || doc.Attributes[0].Type.RefObject != "Контрагенты" {
		t.Fatalf("ссылочный реквизит документа разобран неверно: %+v", doc.Attributes)
	}
	if len(doc.TabularSections) != 1 {
		t.Fatalf("ожидалась 1 табличная часть, получено %d", len(doc.TabularSections))
	}
	ts := doc.TabularSections[0]
	if ts.Name != "Товары" || len(ts.Attributes) != 1 || ts.Attributes[0].Name != "Количество" {
		t.Fatalf("табличная часть разобрана неверно: %+v", ts)
	}
}

// Неизвестные/непарсящиеся объекты не должны валить разбор — они уходят в
// SkippedDirs или конвертируются как справочники (поведение по умолчанию).
func TestParseDirUnknownSection(t *testing.T) {
	src := t.TempDir()
	// Раздел, которого нет в switch и без распознаваемого XML.
	junk := filepath.Join(src, "ОченьСтранныйРаздел", "Объект1")
	if err := os.MkdirAll(junk, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	dump, err := ParseDir(src)
	if err != nil {
		t.Fatalf("ParseDir не должен падать на неизвестном разделе: %v", err)
	}
	// Объект попал в Catalogs (как пустой) либо в SkippedDirs — оба варианта
	// корректны; главное — без ошибки и без паники.
	_ = dump
}
