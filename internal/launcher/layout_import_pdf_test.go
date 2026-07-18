package launcher

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	fpdf "github.com/go-pdf/fpdf"
	"github.com/ivantit66/onebase/internal/pdfimport"
	"github.com/ivantit66/onebase/internal/printform"
	"github.com/ivantit66/onebase/internal/sheet"
)

// План 64, этап 6, фаза 2: эндпоинт импорта макета из PDF.

// syntheticTablePDF строит PDF с кириллической таблицей (границы per-side, спан
// в заголовке) — пригодный для ImportPage черновик.
func syntheticTablePDF(t *testing.T) []byte {
	t.Helper()
	d := sheet.NewDocument()
	d.SetColumnWidth(1, 200)
	d.SetColumnWidth(2, 120)
	d.SetColumnWidth(3, 120)
	allB := func(c *sheet.Cell) {
		c.BorderTop, c.BorderBottom, c.BorderLeft, c.BorderRight = "thin", "thin", "thin", "thin"
	}
	title := d.GetOrCreateCell(0, 0)
	title.Text, title.Bold, title.Align, title.ColSpan = "Накладная № 7", true, "center", 3
	allB(title)
	for c, h := range []string{"Наименование", "Количество", "Сумма"} {
		cell := d.GetOrCreateCell(1, c)
		cell.Text, cell.Bold, cell.Align = h, true, "center"
		allB(cell)
	}
	for r, row := range [][]string{{"Стол", "2", "1000,00"}, {"Стул", "4", "2000,00"}} {
		for c, v := range row {
			cell := d.GetOrCreateCell(2+r, c)
			cell.Text = v
			allB(cell)
		}
	}
	b, err := d.PDF(sheet.PDFOptions{Title: "Накладная"})
	if err != nil {
		t.Fatalf("sheet.PDF: %v", err)
	}
	return b
}

// imageOnlyPDF — валидный PDF без текста (имитация скана).
func imageOnlyPDF(t *testing.T) []byte {
	t.Helper()
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFillColor(180, 180, 180)
	pdf.Rect(20, 20, 100, 60, "F")
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatalf("fpdf: %v", err)
	}
	return buf.Bytes()
}

// postImportPDF собирает multipart-запрос и вызывает хендлер.
func postImportPDF(t *testing.T, h *handler, b *Base, name, doc, page string, pdfBytes []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if name != "" {
		_ = mw.WriteField("name", name)
	}
	if doc != "" {
		_ = mw.WriteField("document", doc)
	}
	if page != "" {
		_ = mw.WriteField("page", page)
	}
	if pdfBytes != nil {
		fw, err := mw.CreateFormFile("file", "doc.pdf")
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(pdfBytes)
	}
	mw.Close()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", b.ID)
	req := httptest.NewRequest(http.MethodPost, "/bases/"+b.ID+"/configurator/layout/import-pdf", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.configuratorImportPDFLayout(rec, req)
	return rec
}

func TestImportPDF_HappyPath(t *testing.T) {
	h, b, dir := newLayoutTestBase(t)
	rec := postImportPDF(t, h, b, "ИзPDFНакладная", "Реализация", "1", syntheticTablePDF(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	path := filepath.Join(dir, "printforms", "ИзPDFНакладная.layout.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл макета не создан: %v", err)
	}
	parsed, err := printform.ParseLayoutBytes(data)
	if err != nil {
		t.Fatalf("созданный макет не парсится: %v", err)
	}
	// Привязка к документу: без document: runtime не покажет форму в печати.
	if parsed.Document != "Реализация" {
		t.Errorf("document = %q, ожидалось «Реализация»", parsed.Document)
	}
	// Импортированная standalone-форма видна в дереве конфигуратора (узел
	// макета), и SelectedTreeID указывает на существующий элемент.
	if !strings.Contains(rec.Body.String(), `data-id="mkt-ИзPDFНакладная"`) {
		t.Error("после импорта в дереве нет узла mkt-ИзPDFНакладная")
	}
	if len(parsed.Areas) != 1 || parsed.Areas[0].Name != "Страница1" {
		t.Fatalf("ожидалась область «Страница1», got %+v", parsed.Areas)
	}
	// Кириллица сохранилась.
	var all strings.Builder
	for _, row := range parsed.Areas[0].Rows {
		for _, c := range row.Cells {
			all.WriteString(c.Text)
			all.WriteString(" ")
		}
	}
	for _, want := range []string{"Накладная", "Наименование", "Стол", "Стул"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("текст %q не найден в импортированном макете: %s", want, all.String())
		}
	}
}

func TestImportPDF_ScanReportsError(t *testing.T) {
	h, b, _ := newLayoutTestBase(t)
	rec := postImportPDF(t, h, b, "Скан", "Реализация", "1", imageOnlyPDF(t))
	// Хендлер перерисовывает конфигуратор с баннером ошибки (код 200, текст внутри).
	body := rec.Body.String()
	if !strings.Contains(body, "текстовый слой") && !strings.Contains(body, "скан") {
		t.Fatalf("ожидалось сообщение про отсутствие текстового слоя, тело:\n%s", truncate(body, 600))
	}
}

func TestImportPDF_MissingName(t *testing.T) {
	h, b, _ := newLayoutTestBase(t)
	rec := postImportPDF(t, h, b, "", "Реализация", "1", syntheticTablePDF(t))
	if !strings.Contains(rec.Body.String(), "обязательно") && !strings.Contains(rec.Body.String(), "required") {
		t.Errorf("ожидалось сообщение про обязательное имя, тело:\n%s", truncate(rec.Body.String(), 400))
	}
}

// Без привязки к документу импорт отклоняется: форма с пустым document: не
// попадает в список печати (симптом «после импорта формы нигде нет»).
func TestImportPDF_MissingDocument(t *testing.T) {
	h, b, dir := newLayoutTestBase(t)
	rec := postImportPDF(t, h, b, "БезДокумента", "", "1", syntheticTablePDF(t))
	if !strings.Contains(rec.Body.String(), "выберите документ") && !strings.Contains(rec.Body.String(), "select a document") {
		t.Errorf("ожидалось сообщение про выбор документа, тело:\n%s", truncate(rec.Body.String(), 400))
	}
	if _, err := os.Stat(filepath.Join(dir, "printforms", "БезДокумента.layout.yaml")); err == nil {
		t.Error("макет без документа не должен создаваться")
	}
}

func TestImportPDF_MissingFile(t *testing.T) {
	h, b, _ := newLayoutTestBase(t)
	rec := postImportPDF(t, h, b, "БезФайла", "Реализация", "1", nil)
	if !strings.Contains(rec.Body.String(), "PDF") {
		t.Errorf("ожидалось сообщение про выбор PDF, тело:\n%s", truncate(rec.Body.String(), 400))
	}
}

func TestImportPDF_Oversize(t *testing.T) {
	h, b, _ := newLayoutTestBase(t)
	// Тело больше лимита: набиваем мусором сверх maxPDFUpload.
	big := bytes.Repeat([]byte("A"), maxPDFUpload+1024)
	rec := postImportPDF(t, h, b, "Большой", "Реализация", "1", big)
	// MaxBytesReader/ParseMultipartForm должны отвергнуть: макет НЕ создаётся.
	// Сообщение об ошибке oversize — «Файл слишком большой или форма повреждена».
	body := rec.Body.String()
	if rec.Code == http.StatusOK && strings.Contains(body, ".layout.yaml") && !strings.Contains(body, "слишком большой") {
		t.Errorf("oversize PDF не должен создавать макет, тело:\n%s", truncate(body, 400))
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// Проверка, что лимит аплоада согласован с лимитом парсера.
func TestMaxPDFUploadAboveParserLimit(t *testing.T) {
	if maxPDFUpload <= pdfimport.MaxFileSize {
		t.Errorf("maxPDFUpload (%d) должен быть больше лимита парсера (%d) на запас multipart",
			maxPDFUpload, pdfimport.MaxFileSize)
	}
}
