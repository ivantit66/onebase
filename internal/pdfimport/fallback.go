package pdfimport

import (
	"math"
	"sort"

	"github.com/ivantit66/onebase/internal/printform"
)

// buildFallback строит макет без линий сетки: кластеризуем слова по строкам
// (близкий Y), внутри строки определяем колонки по гистограмме X-зазоров и
// раскладываем слова по этим колонкам. Это даёт грубый, но осмысленный черновик
// для PDF без рамок (счета/акты с малым числом линий).
func buildFallback(ep *extractedPage, flipY func(float64) float64) *printform.LayoutTemplate {
	words := clusterWords(ep.Runs)
	if len(words) == 0 {
		// Пустой макет с одной областью — вызывающий уже проверил, что runs есть,
		// но слова могли все оказаться пробелами.
		return &printform.LayoutTemplate{
			Areas: []*printform.LayoutArea{{Name: "Страница1"}},
		}
	}

	// Группируем слова в визуальные строки по Y (top-down).
	type lineWords struct {
		y  float64
		ws []word
	}
	sort.Slice(words, func(i, j int) bool { return words[i].Y > words[j].Y })

	var lines []*lineWords
	for _, w := range words {
		y := flipY(w.Y)
		placed := false
		tol := maxF(w.FontSize, 6) * 0.6
		for _, ln := range lines {
			if math.Abs(ln.y-y) <= tol {
				ln.ws = append(ln.ws, w)
				placed = true
				break
			}
		}
		if !placed {
			lines = append(lines, &lineWords{y: y, ws: []word{w}})
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].y < lines[j].y })

	// Определяем общие границы колонок по началам слов (X0) всех строк —
	// гистограмма стартовых X. Колонки = кластеры X0 с допуском.
	var starts []float64
	for _, ln := range lines {
		for _, w := range ln.ws {
			starts = append(starts, w.X0)
		}
	}
	colStarts := snapPositions(starts, 6.0) // 6pt ≈ 2мм — допуск выравнивания колонок

	// Назначаем каждому слову индекс колонки (ближайший colStart слева).
	colIndex := func(x float64) int {
		idx := 0
		for i, cs := range colStarts {
			if x >= cs-3 {
				idx = i
			}
		}
		return idx
	}

	nCols := len(colStarts)
	if nCols == 0 {
		nCols = 1
	}

	area := &printform.LayoutArea{Name: "Страница1"}
	for _, ln := range lines {
		sort.Slice(ln.ws, func(i, j int) bool { return ln.ws[i].X0 < ln.ws[j].X0 })
		// Собираем текст по колонкам.
		colText := make([]string, nCols)
		colFS := make([]float64, nCols)
		colBold := make([]bool, nCols)
		colItalic := make([]bool, nCols)
		for _, w := range ln.ws {
			ci := colIndex(w.X0)
			if ci >= nCols {
				ci = nCols - 1
			}
			if colText[ci] != "" {
				colText[ci] += " "
			}
			colText[ci] += w.S
			if w.FontSize > colFS[ci] {
				colFS[ci] = w.FontSize
			}
			colBold[ci] = colBold[ci] || w.Bold
			colItalic[ci] = colItalic[ci] || w.Italic
		}
		row := printform.LayoutRow{}
		for c := 0; c < nCols; c++ {
			cell := printform.LayoutCell{Text: collapseSpaces(colText[c])}
			if colFS[c] > 0 {
				cell.FontSize = roundPt(colFS[c])
			}
			cell.Bold = colBold[c]
			cell.Italic = colItalic[c]
			row.Cells = append(row.Cells, cell)
		}
		area.Rows = append(area.Rows, row)
	}

	// Ширины колонок по интервалам colStarts (грубо).
	cols := make([]printform.LayoutColumn, nCols)
	pageRight := ep.Geom.MediaX1
	for c := 0; c < nCols; c++ {
		var wPt float64
		if c+1 < nCols {
			wPt = colStarts[c+1] - colStarts[c]
		} else {
			wPt = pageRight - colStarts[c]
		}
		if wPt < 5 {
			wPt = 20
		}
		cols[c] = printform.LayoutColumn{Width: mmStr(ptToMM(wPt))}
	}

	return &printform.LayoutTemplate{
		Columns: cols,
		Areas:   []*printform.LayoutArea{area},
	}
}
