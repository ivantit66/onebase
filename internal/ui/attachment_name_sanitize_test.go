package ui

// Замечание #2 (HIGH): имя загружаемого вложения (header.Filename) полностью
// контролируется клиентом. До фикса оно сохранялось как есть и потом вставлялось
// в innerHTML без экранирования → хранимый XSS. sanitizeAttachmentName —
// серверная нормализация на входе (вторая линия к DOM-экранированию на рендере):
// убирает путь, управляющие символы, ограничивает длину.

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeAttachmentName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"обычное имя", "отчёт.pdf", "отчёт.pdf"},
		{"posix-путь срезается", "/etc/passwd", "passwd"},
		{"относительный путь срезается", "../../secret.txt", "secret.txt"},
		{"windows-путь срезается", `C:\Users\admin\evil.txt`, "evil.txt"},
		{"только обратный слеш", `a\b\c.doc`, "c.doc"},
		{"пустое имя", "", "file"},
		{"имя-точка", ".", "file"},
		{"имя-двоеточие", "..", "file"},
		{"пробелы по краям обрезаются", "  файл.txt  ", "файл.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeAttachmentName(c.in); got != c.want {
				t.Fatalf("sanitizeAttachmentName(%q) = %q, ожидалось %q", c.in, got, c.want)
			}
		})
	}
}

// XSS-payload в имени файла не должен сохранять опасные конструкции в чистом
// виде на уровне символов: проверяем, что управляющие символы вырезаны и что
// результат остаётся валидным UTF-8 (а реальную инертность даёт DOM-рендер).
func TestSanitizeAttachmentName_StripsControlChars(t *testing.T) {
	in := "ev\r\nil\t\x00<img src=x onerror=alert(1)>.txt"
	got := sanitizeAttachmentName(in)

	if strings.ContainsAny(got, "\r\n\t\x00") {
		t.Fatalf("управляющие символы не вырезаны: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("результат не валидный UTF-8: %q", got)
	}
	// Угловые скобки как СИМВОЛЫ сами по себе допустимы в имени файла — защита от
	// XSS обеспечивается экранированием на выводе; здесь убеждаемся лишь, что путь
	// и управляющие символы удалены, а имя не пустое.
	if got == "" {
		t.Fatal("имя не должно стать пустым")
	}
}

// Очень длинное имя обрезается и остаётся валидным UTF-8 (без «рваного» хвоста
// многобайтовой руны).
func TestSanitizeAttachmentName_LimitsLength(t *testing.T) {
	in := strings.Repeat("я", 1000) + ".txt" // 'я' = 2 байта
	got := sanitizeAttachmentName(in)
	if len(got) > 255 {
		t.Fatalf("длина %d превышает лимит 255", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("обрезка испортила UTF-8: %q", got)
	}
}
