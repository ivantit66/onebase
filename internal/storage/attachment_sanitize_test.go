package storage

// Имя загружаемого вложения (header.Filename) полностью контролируется клиентом.
// Без нормализации оно попадало бы в путь/БД/DOM как есть → path-traversal и
// хранимый XSS. SanitizeAttachmentName — серверная нормализация на входе (вторая
// линия к DOM-экранированию на рендере): убирает путь, управляющие символы,
// ограничивает длину. Единый источник для UI- и REST-пути загрузки.

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
			if got := SanitizeAttachmentName(c.in); got != c.want {
				t.Fatalf("SanitizeAttachmentName(%q) = %q, ожидалось %q", c.in, got, c.want)
			}
		})
	}
}

// XSS-payload в имени файла не должен сохранять управляющие символы: проверяем,
// что они вырезаны и результат остаётся валидным UTF-8 (инертность даёт DOM-рендер).
func TestSanitizeAttachmentName_StripsControlChars(t *testing.T) {
	in := "ev\r\nil\t\x00<img src=x onerror=alert(1)>.txt"
	got := SanitizeAttachmentName(in)

	if strings.ContainsAny(got, "\r\n\t\x00") {
		t.Fatalf("управляющие символы не вырезаны: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("результат не валидный UTF-8: %q", got)
	}
	if got == "" {
		t.Fatal("имя не должно стать пустым")
	}
}

// Очень длинное имя обрезается и остаётся валидным UTF-8 (без «рваного» хвоста
// многобайтовой руны).
func TestSanitizeAttachmentName_LimitsLength(t *testing.T) {
	in := strings.Repeat("я", 1000) + ".txt" // 'я' = 2 байта
	got := SanitizeAttachmentName(in)
	if len(got) > 255 {
		t.Fatalf("длина %d превышает лимит 255", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("обрезка испортила UTF-8: %q", got)
	}
}

func TestAttachmentExtAllowed(t *testing.T) {
	allowed := []string{"pdf", "png", ".jpg", "DOCX"}
	cases := []struct {
		name    string
		allowed []string
		file    string
		want    bool
	}{
		{"пустой список — всё разрешено", nil, "anything.exe", true},
		{"разрешённое расширение", allowed, "отчёт.pdf", true},
		{"регистр расширения не важен", allowed, "СКАН.PNG", true},
		{"элемент списка с точкой", allowed, "фото.jpg", true},
		{"элемент списка в верхнем регистре", allowed, "договор.docx", true},
		{"запрещённое расширение", allowed, "script.exe", false},
		{"без расширения при непустом списке", allowed, "Makefile", false},
		{"двойное расширение проверяется по последнему", allowed, "archive.pdf.exe", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AttachmentExtAllowed(c.allowed, c.file); got != c.want {
				t.Fatalf("AttachmentExtAllowed(%v, %q) = %v, ожидалось %v", c.allowed, c.file, got, c.want)
			}
		})
	}
}
