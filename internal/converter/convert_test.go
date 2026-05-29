package converter_test

// Сквозной регрессионный тест конвертера: выгрузка 1С (v8.3 XML) → onebase-проект.
// Проверяет, что Convert проходит целиком и раскладывает YAML-объекты по папкам
// с ожидаемым содержимым. Без этого теста любая правка парсера/писателя ломает
// конвертацию незаметно (пакет converter ранее имел 0% покрытия).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/converter"
)

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
    </ChildObjects>
  </Document>
</MetaDataObject>`

func writeV83(t *testing.T, kindDir, objName, xml string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(kindDir, objName), 0o755); err != nil {
		t.Fatalf("mkdir object: %v", err)
	}
	if err := os.WriteFile(filepath.Join(kindDir, objName+".xml"), []byte(xml), 0o644); err != nil {
		t.Fatalf("write xml: %v", err)
	}
}

func TestConvertEndToEnd(t *testing.T) {
	src := t.TempDir()
	out := filepath.Join(t.TempDir(), "result")
	writeV83(t, filepath.Join(src, "Catalogs"), "Контрагенты", catalogXML)
	writeV83(t, filepath.Join(src, "Documents"), "РеализацияТоваров", documentXML)

	report, err := converter.Convert(converter.Options{SourceDir: src, OutDir: out})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if report.Catalogs != 1 || report.Documents != 1 {
		t.Fatalf("отчёт: ожидалось 1 справочник и 1 документ, получено %+v", report)
	}

	// Файл справочника создан и содержит имя.
	catYAML := readFile(t, filepath.Join(out, "catalogs", "контрагенты.yaml"))
	if !strings.Contains(catYAML, "Контрагенты") {
		t.Errorf("в catalogs/контрагенты.yaml нет имени справочника:\n%s", catYAML)
	}
	if !strings.Contains(catYAML, "ИНН") {
		t.Errorf("в catalogs/контрагенты.yaml нет реквизита ИНН:\n%s", catYAML)
	}

	// Файл документа создан.
	docYAML := readFile(t, filepath.Join(out, "documents", "реализациятоваров.yaml"))
	if !strings.Contains(docYAML, "Контрагент") {
		t.Errorf("в документе нет реквизита Контрагент:\n%s", docYAML)
	}

	// Служебные артефакты.
	if _, err := os.Stat(filepath.Join(out, "config", "app.yaml")); err != nil {
		t.Errorf("не создан config/app.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "conversion_report.txt")); err != nil {
		t.Errorf("не создан conversion_report.txt: %v", err)
	}
}

func TestConvertRequiresDirs(t *testing.T) {
	if _, err := converter.Convert(converter.Options{OutDir: "x"}); err == nil {
		t.Error("Convert без SourceDir должен вернуть ошибку")
	}
	if _, err := converter.Convert(converter.Options{SourceDir: "x"}); err == nil {
		t.Error("Convert без OutDir должен вернуть ошибку")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	return string(data)
}
