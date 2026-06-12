package printform

import (
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/ivantit66/onebase/internal/sheet"
)

const (
	pdfFontSize    = 10.0
	pdfLineHeight  = 5.5
	pdfMargin      = 15.0
	pdfHeaderSize  = 13.0
	pdfTitleSize   = 14.0
	pdfCellPadding = 2.0
	// pdfFont — встроенный кириллический шрифт (PT Serif). Заменил Helvetica
	// + latinize (план 64, этап 2): транслитерация мертва.
	pdfFont = "PTSerif"
)

// registerPDFFonts регистрирует встроенные PT-шрифты из internal/sheet.
func registerPDFFonts(pdf *fpdf.Fpdf) {
	pdf.AddUTF8FontFromBytes(pdfFont, "", sheet.PTSerifRegular)
	pdf.AddUTF8FontFromBytes(pdfFont, "B", sheet.PTSerifBold)
	pdf.AddUTF8FontFromBytes(pdfFont, "I", sheet.PTSerifItalic)
	pdf.AddUTF8FontFromBytes(pdfFont, "BI", sheet.PTSerifBoldItalic)
}

// RenderPDF produces a PDF for the given print form and data context.
func RenderPDF(form *PrintForm, ctx *RenderContext) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMargin, pdfMargin, pdfMargin)
	registerPDFFonts(pdf)
	pdf.AddPage()

	// Встроенный кириллический шрифт PT Serif (без транслитерации).
	pdf.SetFont(pdfFont, "", pdfFontSize)

	title := interpolate(form.Title, ctx, 0)
	header := interpolate(form.Header, ctx, 0)
	footer := interpolate(form.Footer, ctx, 0)

	pw, _ := pdf.GetPageSize()
	usable := pw - 2*pdfMargin

	// Title
	if title != "" {
		pdf.SetFont(pdfFont, "B", pdfTitleSize)
		pdf.MultiCell(usable, 7, title, "", "C", false)
		pdf.Ln(4)
	}

	// Header block
	if header != "" {
		pdf.SetFont(pdfFont, "", pdfFontSize)
		for _, line := range splitMarkdownLines(header) {
			if line == "---" || line == "___" {
				pdf.Line(pdfMargin, pdf.GetY(), pw-pdfMargin, pdf.GetY())
				pdf.Ln(2)
				continue
			}
			bold := false
			text := line
			if strings.HasPrefix(line, "## ") {
				text = strings.TrimPrefix(line, "## ")
				bold = true
				pdf.SetFont(pdfFont, "B", pdfFontSize+1)
			} else if strings.HasPrefix(line, "# ") {
				text = strings.TrimPrefix(line, "# ")
				bold = true
				pdf.SetFont(pdfFont, "B", pdfHeaderSize)
			} else {
				// inline **bold**
				text = stripMarkdown(text)
			}
			pdf.MultiCell(usable, pdfLineHeight, text, "", "L", false)
			if bold {
				pdf.SetFont(pdfFont, "", pdfFontSize)
			}
		}
		pdf.Ln(3)
	}

	// Table
	if form.Table != nil {
		renderPDFTable(pdf, form.Table, ctx, usable)
		pdf.Ln(4)
	}

	// Footer block
	if footer != "" {
		pdf.Line(pdfMargin, pdf.GetY(), pw-pdfMargin, pdf.GetY())
		pdf.Ln(2)
		pdf.SetFont(pdfFont, "", pdfFontSize-1)
		for _, line := range splitMarkdownLines(footer) {
			pdf.MultiCell(usable, pdfLineHeight-1, stripMarkdown(line), "", "L", false)
		}
	}

	var buf strings.Builder
	_ = buf // use byte buffer
	var b []byte
	bSlice := &byteSliceWriter{}
	err := pdf.Output(bSlice)
	if err != nil {
		return nil, err
	}
	b = bSlice.data
	return b, nil
}

func renderPDFTable(pdf *fpdf.Fpdf, ts *TableSection, ctx *RenderContext, usable float64) {
	rows := ctx.TableParts[ts.Source]

	// Compute column widths
	colWidths := computeColWidths(ts.Columns, usable)

	// Header row
	pdf.SetFont(pdfFont, "B", pdfFontSize-0.5)
	pdf.SetFillColor(240, 240, 240)
	for i, col := range ts.Columns {
		pdf.CellFormat(colWidths[i], 6, col.Label, "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)

	// Compute totals
	totals := make(map[string]float64)
	for _, row := range rows {
		for _, tot := range ts.Totals {
			if tot.Sum {
				if v, ok := row[tot.Field]; ok {
					if f, ok2 := toFloat(v); ok2 {
						totals[tot.Field] += f
					}
				}
			}
		}
	}

	// Data rows
	pdf.SetFont(pdfFont, "", pdfFontSize-0.5)
	pdf.SetFillColor(255, 255, 255)
	for i, row := range rows {
		for ci, col := range ts.Columns {
			var val any
			if col.Field == "@row" {
				val = i + 1
			} else if idx := strings.Index(col.Field, "."); idx != -1 {
				fieldName := col.Field[:idx]
				subField := col.Field[idx+1:]
				if refVal, ok := row[fieldName]; ok {
					if refID, ok2 := refVal.(string); ok2 {
						if refData, ok3 := ctx.Refs[refID]; ok3 {
							val = refData[subField]
						}
					}
				}
			} else {
				val = row[col.Field]
			}
			cell := ApplyFormat(val, col.Format)
			align := "L"
			switch col.Align {
			case "right":
				align = "R"
			case "center":
				align = "C"
			}
			pdf.CellFormat(colWidths[ci], 5.5, cell, "1", 0, align, false, 0, "")
		}
		pdf.Ln(-1)
	}

	// Totals row
	if len(ts.Totals) > 0 {
		pdf.SetFont(pdfFont, "B", pdfFontSize-0.5)
		pdf.SetFillColor(248, 248, 248)
		totColIdx := make(map[int]TotalSpec)
		for _, tot := range ts.Totals {
			for ci, col := range ts.Columns {
				if col.Field == tot.Field {
					totColIdx[ci] = tot
				}
			}
		}
		for ci, col := range ts.Columns {
			cell := ""
			if tot, ok := totColIdx[ci]; ok {
				if tot.Label != "" {
					cell = fmt.Sprintf("%s: %s", tot.Label, ApplyFormat(totals[tot.Field], col.Format))
				} else {
					cell = ApplyFormat(totals[tot.Field], col.Format)
				}
			}
			pdf.CellFormat(colWidths[ci], 6, cell, "1", 0, "R", true, 0, "")
		}
		pdf.Ln(-1)
	}
}

func computeColWidths(cols []Column, usable float64) []float64 {
	widths := make([]float64, len(cols))
	// Parse explicit widths (e.g. "20%", "30mm")
	total := 0.0
	free := make([]int, 0)
	for i, col := range cols {
		w := col.Width
		if strings.HasSuffix(w, "%") {
			var pct float64
			fmt.Sscanf(w, "%f%%", &pct)
			widths[i] = usable * pct / 100
			total += widths[i]
		} else if strings.HasSuffix(w, "mm") {
			var mm float64
			fmt.Sscanf(w, "%fmm", &mm)
			widths[i] = mm
			total += mm
		} else {
			free = append(free, i)
		}
	}
	if len(free) > 0 {
		remaining := usable - total
		if remaining < 0 {
			remaining = 20
		}
		each := remaining / float64(len(free))
		for _, i := range free {
			widths[i] = each
		}
	}
	return widths
}

func splitMarkdownLines(text string) []string {
	return strings.Split(text, "\n")
}

func stripMarkdown(s string) string {
	s = reBold.ReplaceAllString(s, "$1")
	s = reItalic.ReplaceAllString(s, "$1")
	return s
}

type byteSliceWriter struct {
	data []byte
}

func (w *byteSliceWriter) Write(p []byte) (int, error) {
	w.data = append(w.data, p...)
	return len(p), nil
}
