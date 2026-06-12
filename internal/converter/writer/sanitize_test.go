package writer

import (
	"strings"
	"testing"
)

// Директивы препроцессора 1С (#Область, #Если…) не поддерживаются DSL —
// загрузка конфигурации падала на «expected Procedure or Function, got "#"»
// (issue #48 п.2). Конвертер обязан их вырезать, сохраняя содержимое блоков.
func TestSanitizeBSL(t *testing.T) {
	in := strings.Join([]string{
		"#Область Сервис",
		"Процедура Привет() Экспорт",
		"  #Если Сервер Тогда",
		"  а = 1;",
		"  #ИначеЕсли Клиент Тогда",
		"  а = 2;",
		"  #Иначе",
		"  а = 3;",
		"  #КонецЕсли",
		"КонецПроцедуры",
		"#КонецОбласти",
		"#Region English",
		"#EndRegion",
	}, "\n")
	got := sanitizeBSL(in)
	if strings.Contains(got, "#") {
		t.Fatalf("директивы не вырезаны:\n%s", got)
	}
	for _, want := range []string{"Процедура Привет() Экспорт", "а = 1;", "а = 2;", "а = 3;", "КонецПроцедуры"} {
		if !strings.Contains(got, want) {
			t.Fatalf("потеряно содержимое %q:\n%s", want, got)
		}
	}
}

// Краевые случаи: BOM перед первой директивой, CRLF-окончания, табуляция,
// регистр, директива в конце файла без перевода строки — вырезаются; строки,
// НЕ начинающиеся с известной директивы (#Использовать, # в коде/комментарии),
// сохраняются.
func TestSanitizeBSLEdgeCases(t *testing.T) {
	const bom = "\xef\xbb\xbf" // UTF-8 BOM (U+FEFF)
	in := bom + "#Область Сервис\r\n" +
		"\t#ОБЛАСТЬ Вложенная\r\n" +
		"а = 1; // #Область в комментарии\r\n" +
		"#Использовать lib\r\n" +
		"\t#конецобласти\r\n" +
		"#КонецОбласти"
	got := sanitizeBSL(in)
	for _, want := range []string{"а = 1; // #Область в комментарии", "#Использовать lib"} {
		if !strings.Contains(got, want) {
			t.Fatalf("потеряна строка %q:\n%s", want, got)
		}
	}
	for _, banned := range []string{"Область Сервис", "ОБЛАСТЬ Вложенная", "конецобласти", "КонецОбласти", bom} {
		if strings.Contains(got, banned) {
			t.Fatalf("не вырезано %q:\n%s", banned, got)
		}
	}
}
