package pdfimport

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/ivantit66/onebase/internal/printform"
	"github.com/ivantit66/onebase/internal/sheet"
)

// Параметры алгоритма (в пунктах PDF, если не указано иное).
const (
	snapEps      = 1.2  // допуск слияния близких координат cut'ов (pt)
	collinearEps = 0.7  // допуск коллинеарности при слиянии линий (pt)
	mergeGapEps  = 2.0  // максимальный разрыв между коллинеарными сегментами для слияния (pt)
	thickLineW   = 1.0  // линия толще этого (pt) считается thick границей
	minGridLines = 4    // меньше линий каждого направления → fallback-кластеризация
	wordGapPt    = 2.0  // зазор между глифами больше — новый «токен»
)

// buildLayout строит LayoutTemplate из извлечённых данных страницы.
func buildLayout(ep *extractedPage) *printform.LayoutTemplate {
	page := buildPageSetup(ep.Geom)

	// Нормализуем координаты в систему top-down (Y растёт вниз), оставаясь в pt.
	// flipY(y) = mediaTop - y.
	top := ep.Geom.MediaY1
	flipY := func(y float64) float64 { return top - y }

	hCuts, vCuts := buildCuts(ep.Lines, flipY, ep.Geom)

	var tpl *printform.LayoutTemplate
	if len(hCuts) >= minGridLines && len(vCuts) >= minGridLines {
		tpl = buildFromGrid(ep, hCuts, vCuts, flipY)
	} else {
		tpl = buildFallback(ep, flipY)
	}
	tpl.Page = page
	if tpl.Name == "" {
		tpl.Name = "ИзPDF"
	}
	return tpl
}

// buildPageSetup переводит MediaBox+Rotate в PageSetup (ориентация/формат/поля).
func buildPageSetup(g pageGeom) *sheet.PageSetup {
	wPt, hPt := g.widthPt(), g.heightPt()
	// Учёт поворота: при 90/270 меняем местами стороны.
	if g.Rotate == 90 || g.Rotate == 270 {
		wPt, hPt = hPt, wPt
	}
	orient := "portrait"
	if wPt > hPt {
		orient = "landscape"
	}
	format := guessFormat(wPt, hPt)
	return &sheet.PageSetup{
		Orientation: orient,
		Format:      format,
		MarginsMM:   sheet.Margins{Top: 10, Bottom: 10, Left: 10, Right: 10},
	}
}

// guessFormat определяет формат листа по размерам в pt (портретная ориентация).
func guessFormat(wPt, hPt float64) string {
	// Нормализуем к портрету для сравнения.
	w, h := wPt, hPt
	if w > h {
		w, h = h, w
	}
	wmm, hmm := ptToMM(w), ptToMM(h)
	type fmtDef struct {
		name string
		w, h float64
	}
	defs := []fmtDef{
		{"A3", 297, 420}, {"A4", 210, 297}, {"A5", 148, 210},
		{"Letter", 215.9, 279.4}, {"Legal", 215.9, 355.6},
	}
	best := "A4"
	bestErr := math.MaxFloat64
	for _, d := range defs {
		e := math.Abs(wmm-d.w) + math.Abs(hmm-d.h)
		if e < bestErr {
			bestErr = e
			best = d.name
		}
	}
	// Если ничего близко (нестандартный размер) — оставляем A4.
	if bestErr > 20 {
		return "A4"
	}
	return best
}

// buildCuts собирает координаты линий разреза (cut'ы) по обоим направлениям.
// Возвращает hCuts (Y top-down, pt) — позиции горизонтальных линий, и vCuts
// (X, pt) — позиции вертикальных линий. Коллинеарные сегменты сливаются, близкие
// позиции схлопываются (snap).
func buildCuts(lines []lineSeg, flipY func(float64) float64, g pageGeom) (hCuts, vCuts []float64) {
	var hPos, vPos []float64
	for _, ln := range lines {
		if ln.Horizontal {
			hPos = append(hPos, flipY(ln.Y1))
		} else {
			vPos = append(vPos, ln.X1)
		}
	}
	hCuts = snapPositions(hPos, snapEps)
	vCuts = snapPositions(vPos, snapEps)
	return
}

// snapPositions сортирует позиции и схлопывает близкие (в пределах eps) в одну
// (среднюю), возвращая отсортированный список уникальных cut'ов.
func snapPositions(pos []float64, eps float64) []float64 {
	if len(pos) == 0 {
		return nil
	}
	sort.Float64s(pos)
	var out []float64
	groupSum := pos[0]
	groupCnt := 1.0
	groupLast := pos[0]
	for i := 1; i < len(pos); i++ {
		if pos[i]-groupLast <= eps {
			groupSum += pos[i]
			groupCnt++
		} else {
			out = append(out, groupSum/groupCnt)
			groupSum = pos[i]
			groupCnt = 1
		}
		groupLast = pos[i]
	}
	out = append(out, groupSum/groupCnt)
	return out
}

// edgeIndex — карта присутствия линии на ребре сетки. Ключ — индекс cut'а;
// значение — отсортированные интервалы [start,end] (вдоль линии), покрытые
// сегментами. Используется для определения границ ячеек по сторонам.
type edgeIndex struct {
	cuts     []float64
	segments map[int][]interval // cutIndex → интервалы вдоль перпендикулярной оси
	widthAt  map[int]float64    // максимальная толщина линии на этом cut'е
}

type interval struct{ a, b, w float64 }

// buildEdgeIndex проецирует линии на ближайшие cut'ы и собирает покрытые интервалы.
// horizontal=true: линии горизонтальные, cuts — по Y, интервалы — по X.
func buildEdgeIndex(lines []lineSeg, cuts []float64, flipY func(float64) float64, horizontal bool) *edgeIndex {
	ei := &edgeIndex{cuts: cuts, segments: map[int][]interval{}, widthAt: map[int]float64{}}
	for _, ln := range lines {
		if ln.Horizontal != horizontal {
			continue
		}
		var pos, a, b float64
		if horizontal {
			pos = flipY(ln.Y1)
			a, b = minMax(ln.X1, ln.X2)
		} else {
			pos = ln.X1
			a, b = minMax(flipY(ln.Y1), flipY(ln.Y2))
		}
		idx := nearestCut(cuts, pos)
		if idx < 0 {
			continue
		}
		ei.segments[idx] = append(ei.segments[idx], interval{a, b, ln.Width})
		if ln.Width > ei.widthAt[idx] {
			ei.widthAt[idx] = ln.Width
		}
	}
	return ei
}

// covers сообщает, покрыт ли интервал [a,b] линиями на cut'е idx (с допуском).
// Возвращает также максимальную толщину покрывающих сегментов.
func (ei *edgeIndex) covers(idx int, a, b float64) (bool, float64) {
	segs := ei.segments[idx]
	if len(segs) == 0 {
		return false, 0
	}
	// Требуем покрытия середины интервала — устойчиво к мелким разрывам.
	mid := (a + b) / 2
	const tol = 1.5
	maxW := 0.0
	covered := false
	for _, s := range segs {
		if mid >= s.a-tol && mid <= s.b+tol {
			covered = true
			if s.w > maxW {
				maxW = s.w
			}
		}
	}
	return covered, maxW
}

// buildFromGrid строит макет по найденной сетке линий.
func buildFromGrid(ep *extractedPage, hCuts, vCuts []float64, flipY func(float64) float64) *printform.LayoutTemplate {
	nRows := len(hCuts) - 1
	nCols := len(vCuts) - 1

	hEdges := buildEdgeIndex(ep.Lines, hCuts, flipY, true)
	vEdges := buildEdgeIndex(ep.Lines, vCuts, flipY, false)

	// Базовая сетка ячеек: cell[r][c] существует, если не «поглощён» спаном.
	// Определяем спаны: ячейка (r,c) расширяется вправо, пока между колонками нет
	// вертикальной линии, и вниз, пока нет горизонтальной линии. Поглощённые
	// помечаем.
	consumed := make([][]bool, nRows)
	for r := range consumed {
		consumed[r] = make([]bool, nCols)
	}

	var regions []*region

	for r := 0; r < nRows; r++ {
		for c := 0; c < nCols; c++ {
			if consumed[r][c] {
				continue
			}
			// colSpan: пока правая граница колонки c+span не закрыта вертикальной
			// линией на всю высоту строки r.
			colSpan := 1
			for c+colSpan < nCols {
				vIdx := c + colSpan
				if covered, _ := vEdges.covers(vIdx, hCuts[r], hCuts[r+1]); covered {
					break
				}
				colSpan++
			}
			// rowSpan: пока нижняя граница строки r+span не закрыта горизонтальной
			// линией на всю ширину региона.
			rowSpan := 1
			for r+rowSpan < nRows {
				hIdx := r + rowSpan
				if covered, _ := hEdges.covers(hIdx, vCuts[c], vCuts[c+colSpan]); covered {
					break
				}
				rowSpan++
			}

			for rr := r; rr < r+rowSpan; rr++ {
				for cc := c; cc < c+colSpan; cc++ {
					consumed[rr][cc] = true
				}
			}

			reg := &region{
				r: r, c: c, rowSpan: rowSpan, colSpan: colSpan,
				x0: vCuts[c], x1: vCuts[c+colSpan],
				y0: hCuts[r], y1: hCuts[r+rowSpan],
			}
			// Границы по сторонам региона.
			tCov, tw := hEdges.covers(r, vCuts[c], vCuts[c+colSpan])
			bCov, bw := hEdges.covers(r+rowSpan, vCuts[c], vCuts[c+colSpan])
			lCov, lw := vEdges.covers(c, hCuts[r], hCuts[r+rowSpan])
			rCov, rw := vEdges.covers(c+colSpan, hCuts[r], hCuts[r+rowSpan])
			reg.hasTop, reg.bTop = tCov, tw
			reg.hasBot, reg.bBottom = bCov, bw
			reg.hasLeft, reg.bLeft = lCov, lw
			reg.hasRght, reg.bRight = rCov, rw
			regions = append(regions, reg)
		}
	}

	// Тексты → регионы.
	words := clusterWords(ep.Runs)
	assignWordsToRegions(words, regions, flipY)

	// Строим LayoutTemplate: одна область «Страница1», nRows строк, nCols колонок.
	area := &printform.LayoutArea{Name: "Страница1"}
	colWidthsMM := make([]float64, nCols)
	for c := 0; c < nCols; c++ {
		colWidthsMM[c] = ptToMM(vCuts[c+1] - vCuts[c])
	}

	// Карта «верхний-левый угол региона» → регион, чтобы вывести ячейки по сетке.
	cornerRegion := map[[2]int]*region{}
	for _, reg := range regions {
		cornerRegion[[2]int{reg.r, reg.c}] = reg
	}

	for r := 0; r < nRows; r++ {
		row := printform.LayoutRow{Height: mmStr(ptToMM(hCuts[r+1] - hCuts[r]))}
		for c := 0; c < nCols; c++ {
			if consumed[r][c] && cornerRegion[[2]int{r, c}] == nil {
				// Поглощён спаном слева/сверху — ячейку не выводим.
				continue
			}
			reg := cornerRegion[[2]int{r, c}]
			if reg == nil {
				// Базовая ячейка без региона (не должно случаться, но на всякий).
				row.Cells = append(row.Cells, printform.LayoutCell{})
				continue
			}
			row.Cells = append(row.Cells, regionToCell(reg))
		}
		area.Rows = append(area.Rows, row)
	}

	cols := make([]printform.LayoutColumn, nCols)
	for c := 0; c < nCols; c++ {
		cols[c] = printform.LayoutColumn{Width: mmStr(colWidthsMM[c])}
	}

	return &printform.LayoutTemplate{
		Columns: cols,
		Areas:   []*printform.LayoutArea{area},
	}
}

// region — прямоугольная зона сетки (одна или несколько базовых ячеек,
// объединённых спанами). Координаты в pt, top-down.
type region struct {
	r, c             int
	rowSpan, colSpan int
	x0, x1, y0, y1   float64 // границы региона (pt, top-down)
	bTop, bBottom    float64 // толщина границ (pt)
	bLeft, bRight    float64
	hasTop, hasBot   bool
	hasLeft, hasRght bool

	// заполняется assignWordsToRegions:
	text     string
	fontSize float64
	bold     bool
	italic   bool
	minX     float64 // левый край текста (для эвристики выравнивания)
	maxX     float64 // правый край текста
}

// regionToCell собирает LayoutCell из региона: текст, спаны, границы, шрифт,
// выравнивание (эвристикой по отступам).
func regionToCell(reg *region) printform.LayoutCell {
	cell := printform.LayoutCell{Text: reg.text}
	if reg.colSpan > 1 {
		cell.ColSpan = reg.colSpan
	}
	if reg.rowSpan > 1 {
		cell.RowSpan = reg.rowSpan
	}
	if reg.fontSize > 0 {
		cell.FontSize = roundPt(reg.fontSize)
	}
	cell.Bold = reg.bold
	cell.Italic = reg.italic

	// Границы по сторонам.
	b := &printform.CellBorders{
		Top:    borderClass(reg.hasTop, reg.bTop),
		Bottom: borderClass(reg.hasBot, reg.bBottom),
		Left:   borderClass(reg.hasLeft, reg.bLeft),
		Right:  borderClass(reg.hasRght, reg.bRight),
	}
	if !b.IsZero() {
		cell.Borders = b
	}

	// Выравнивание: эвристика по отступам текста в ячейке (±10%).
	if reg.text != "" {
		cell.Align = guessAlign(reg)
	}
	return cell
}

// borderClass переводит флаг наличия + толщину линии в класс границы.
func borderClass(has bool, widthPt float64) string {
	if !has {
		return ""
	}
	if widthPt >= thickLineW {
		return "thick"
	}
	return "thin"
}

// guessAlign определяет выравнивание ячейки по отступам текста слева/справа.
// Если текст прижат вправо (правый отступ много меньше левого) → right; если
// центрирован (отступы примерно равны и заметны) → center; иначе left.
func guessAlign(reg *region) string {
	w := reg.x1 - reg.x0
	if w <= 0 {
		return ""
	}
	const pad = 2.0 // pt: внутренний отступ ячейки, который игнорируем
	leftGap := reg.minX - reg.x0 - pad
	rightGap := reg.x1 - reg.maxX - pad
	if leftGap < 0 {
		leftGap = 0
	}
	if rightGap < 0 {
		rightGap = 0
	}
	tol := w * 0.1
	switch {
	case rightGap < tol && leftGap > tol:
		return "right"
	case math.Abs(leftGap-rightGap) < tol && leftGap > tol:
		return "center"
	default:
		return "left"
	}
}

// roundPt округляет кегль до ближайшего целого pt.
func roundPt(fs float64) int { return int(math.Round(fs)) }

// minMax возвращает (min, max) пары.
func minMax(a, b float64) (float64, float64) {
	if a <= b {
		return a, b
	}
	return b, a
}

// nearestCut возвращает индекс ближайшего cut'а к pos или -1, если все дальше eps.
func nearestCut(cuts []float64, pos float64) int {
	best := -1
	bestD := snapEps * 2
	for i, c := range cuts {
		d := math.Abs(c - pos)
		if d < bestD {
			bestD = d
			best = i
		}
	}
	return best
}

// mmStr форматирует размер в мм как CSS-значение "12.3mm" (1 знак, без хвостовых нулей).
func mmStr(mm float64) string {
	s := strconv.FormatFloat(mm, 'f', 1, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		s = "0"
	}
	return s + "mm"
}
