package pdfimport

import (
	"os"
	"testing"
)

// TestDumpSyntheticPDF — вспомогательный «тест» для спайка: пишет синтетический
// PDF на диск, чтобы прогнать внешним инструментом. Активируется переменной
// окружения DUMP_PDF.
func TestDumpSyntheticPDF(t *testing.T) {
	if os.Getenv("DUMP_PDF") == "" {
		t.Skip("set DUMP_PDF to write fixture to disk")
	}
	b := buildSyntheticTablePDF(t)
	if err := os.WriteFile("C:/Projects/onebase/tmp_spike/synthetic.pdf", b, 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d bytes", len(b))
}
