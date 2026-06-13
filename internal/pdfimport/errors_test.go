package pdfimport

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
)

func TestImportCorruptPDFReturnsErrorNoPanic(t *testing.T) {
	// Заведомо битые данные: не PDF вовсе.
	garbage := []byte("это не PDF, просто мусорные байты \x00\x01\x02 без структуры")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ImportBytes запаниковал на битом PDF: %v", r)
		}
	}()
	tpl, err := ImportBytes(garbage, 1)
	if err == nil {
		t.Fatalf("ожидалась ошибка на битом PDF, получен макет: %+v", tpl)
	}
}

func TestImportTruncatedPDFHeaderNoPanic(t *testing.T) {
	// Начинается как PDF, но обрезан — частый недоверенный ввод.
	data := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ImportBytes запаниковал на обрезанном PDF: %v", r)
		}
	}()
	_, err := ImportBytes(data, 1)
	if err == nil {
		t.Fatal("ожидалась ошибка на обрезанном PDF")
	}
}

func TestImportEmptyInput(t *testing.T) {
	_, err := ImportBytes(nil, 1)
	if !errors.Is(err, ErrParse) {
		t.Fatalf("ожидался ErrParse на пустом вводе, получено: %v", err)
	}
}

func TestImportOversizeRejected(t *testing.T) {
	// Подаём size больше лимита (содержимое не важно — отсечётся по размеру).
	big := bytes.NewReader([]byte("%PDF"))
	_, err := ImportPage(big, MaxFileSize+1, 1)
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("ожидался ErrFileTooLarge, получено: %v", err)
	}
}

// buildImageOnlyPDF строит валидный PDF без текстового слоя (только заливка) —
// имитация скана/картинки. fpdf без вызовов Cell/Text даёт страницу без текста.
func buildImageOnlyPDF(t *testing.T) []byte {
	t.Helper()
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFillColor(200, 200, 200)
	pdf.Rect(20, 20, 100, 50, "F") // только прямоугольник, никакого текста
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatalf("fpdf output: %v", err)
	}
	return buf.Bytes()
}

func TestImageOnlyPDFReportsNoTextLayer(t *testing.T) {
	data := buildImageOnlyPDF(t)
	_, err := ImportBytes(data, 1)
	if !errors.Is(err, ErrNoTextLayer) {
		t.Fatalf("ожидался ErrNoTextLayer для PDF без текста, получено: %v", err)
	}
	if !strings.Contains(err.Error(), "текстовый слой") {
		t.Errorf("сообщение об ошибке не упоминает текстовый слой: %v", err)
	}
}

func TestImportPageNotFound(t *testing.T) {
	data := buildSyntheticTablePDF(t)
	_, err := ImportBytes(data, 99)
	if !errors.Is(err, ErrPageNotFound) {
		t.Fatalf("ожидался ErrPageNotFound для несуществующей страницы, получено: %v", err)
	}
}
