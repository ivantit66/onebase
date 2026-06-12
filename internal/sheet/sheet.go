// Package sheet — нейтральная модель табличного документа (ячейки, спаны,
// страницы) и его рендеры. Не зависит ни от DSL-интерпретатора, ни от
// printform — здесь сходятся все пути формирования печатных форм (план 64).
//
// Пакет импортирует только stdlib: цикл импортов interpreter↔printform решается
// тем, что и interpreter (тонкая DSL-обвязка), и будущий декларативный движок
// строят именно sheet.Document, а рендереры (HTML здесь, PDF — этап 2) живут
// рядом с моделью.
package sheet

import (
	"fmt"
	"strings"
)

// Cell — одна ячейка табличного документа со стилями и спанами.
// Поля экспортированы: interpreter-обёртки (Ячейка/Область/Параметры)
// читают и пишут их напрямую, сохраняя прежний DSL-контракт.
type Cell struct {
	Text       string
	Value      any
	Align      string // left, center, right, justify
	Vertical   string // top, center, bottom
	Width      float64
	Height     float64
	Bold       bool
	Italic     bool
	FontSize   int
	FontFamily string
	BackColor  string
	TextColor  string
	Border     string
	Fill       string
	Picture    string
	ColSpan    int
	RowSpan    int
	// ParameterName — для макета: имя именованного параметра, заполняющего ячейку.
	ParameterName string
}

// NewCell создаёт ячейку с дефолтным форматированием (как в 1С:ТабличныйДокумент).
func NewCell(text string) *Cell {
	return &Cell{
		Text:       text,
		Value:      text,
		Align:      "left",
		Vertical:   "top",
		FontSize:   10,
		FontFamily: "Times New Roman",
		Border:     "all",
	}
}

// Area — прямоугольная область-шаблон с собственным хранилищем ячеек в
// относительных координатах (R1C1 = левый-верхний угол). Параметры
// устанавливаются относительно области и копируются в документ при Вывести/
// Присоединить. Это чистая модель-вью; DSL-протокол (CallMethod/Get/Set) —
// в обёртке interpreter.SpreadsheetDocumentArea.
type Area struct {
	Cells  map[string]*Cell // относительные координаты "row,col"
	Top    int
	Left   int
	Bottom int
	Right  int
	Name   string // имя для именованных областей (необязательно)
}

// NewArea создаёт пустую область с заданными границами (0-based).
func NewArea(top, left, bottom, right int) *Area {
	return &Area{
		Cells:  make(map[string]*Cell),
		Top:    top,
		Left:   left,
		Bottom: bottom,
		Right:  right,
	}
}

// Rows возвращает число строк в области.
func (a *Area) Rows() int { return a.Bottom - a.Top + 1 }

// Cols возвращает число колонок в области.
func (a *Area) Cols() int { return a.Right - a.Left + 1 }

// GetOrCreateCell возвращает (создавая при необходимости) ячейку в
// относительных координатах (row, col) внутри области.
func (a *Area) GetOrCreateCell(row, col int) *Cell {
	key := fmt.Sprintf("%d,%d", row, col)
	if cell, ok := a.Cells[key]; ok {
		return cell
	}
	cell := NewCell("")
	a.Cells[key] = cell
	return cell
}

// Clear очищает все ячейки области.
func (a *Area) Clear() {
	a.Cells = make(map[string]*Cell)
}

// Merge объединяет все ячейки области в одну (через colSpan/rowSpan ячейки 0,0).
func (a *Area) Merge() {
	cols := a.Cols()
	rows := a.Rows()
	if rows == 1 && cols == 1 {
		return
	}
	cell := a.GetOrCreateCell(0, 0)
	cell.ColSpan = cols
	cell.RowSpan = rows
}

// Document — табличный документ для печатных форм. Содержит данные: ячейки,
// размеры, разрывы страниц, повтор шапки, текущую позицию вывода. Рендеры
// (HTML/PDF) и операции вывода — методы этого пакета; DSL-протокол поверх —
// в interpreter.SpreadsheetDocument, делегирующем сюда.
type Document struct {
	Cells        map[string]*Cell // "row,col" -> cell
	RowCount     int
	ColCount     int
	CurrentRow   int
	CurrentCol   int
	PageBreaks   []int
	ShowMode     bool
	FileName     string
	RepeatHeader bool
	HeaderArea   *Area
	BackURL      string // URL для кнопки «Назад» в тулбаре HTML
}

// NewDocument создаёт пустой документ с дефолтным размером сетки (как в старом
// SpreadsheetDocument: 100×50).
func NewDocument() *Document {
	return &Document{
		Cells:      make(map[string]*Cell),
		RowCount:   100,
		ColCount:   50,
		CurrentRow: 0,
		CurrentCol: 0,
	}
}

// GetCell возвращает ячейку или nil.
func (d *Document) GetCell(row, col int) *Cell {
	key := fmt.Sprintf("%d,%d", row, col)
	return d.Cells[key]
}

// GetOrCreateCell возвращает (создавая при необходимости) ячейку (row, col).
func (d *Document) GetOrCreateCell(row, col int) *Cell {
	key := fmt.Sprintf("%d,%d", row, col)
	if cell, ok := d.Cells[key]; ok {
		return cell
	}
	cell := NewCell("")
	d.Cells[key] = cell
	return cell
}

// SetCell записывает текст и значение в ячейку.
func (d *Document) SetCell(row, col int, text string) {
	cell := d.GetOrCreateCell(row, col)
	cell.Text = text
	cell.Value = text
}

// Put выводит область в текущую позицию и переходит на следующую строку.
func (d *Document) Put(area *Area) {
	if area == nil {
		return
	}
	rows := area.Rows()
	cols := area.Cols()

	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			key := fmt.Sprintf("%d,%d", r, c)
			if srcCell, exists := area.Cells[key]; exists {
				targetRow := d.CurrentRow + r
				targetCol := d.CurrentCol + c
				destCell := d.GetOrCreateCell(targetRow, targetCol)
				*destCell = *srcCell
			}
		}
	}

	d.CurrentRow += rows
	d.CurrentCol = 0
}

// Append выводит область справа от ранее выведенной (без перехода на строку).
func (d *Document) Append(area *Area) {
	if area == nil {
		return
	}
	// Найти правую границу текущей строки.
	maxCol := 0
	for col := 0; col < d.ColCount; col++ {
		if d.GetCell(d.CurrentRow, col) != nil {
			maxCol = col + 1
		}
	}
	d.CurrentCol = maxCol

	rows := area.Rows()
	cols := area.Cols()

	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			key := fmt.Sprintf("%d,%d", r, c)
			if srcCell, exists := area.Cells[key]; exists {
				targetRow := d.CurrentRow + r
				targetCol := d.CurrentCol + c
				destCell := d.GetOrCreateCell(targetRow, targetCol)
				*destCell = *srcCell
			}
		}
	}
}

// Clear очищает документ и сбрасывает позицию вывода.
func (d *Document) Clear() {
	d.Cells = make(map[string]*Cell)
	d.CurrentRow = 0
	d.CurrentCol = 0
	d.PageBreaks = nil
}

// RemoveArea удаляет содержимое прямоугольника области из документа.
func (d *Document) RemoveArea(area *Area) {
	if area == nil {
		return
	}
	for row := area.Top; row <= area.Bottom; row++ {
		for col := area.Left; col <= area.Right; col++ {
			key := fmt.Sprintf("%d,%d", row, col)
			delete(d.Cells, key)
		}
	}
}

// PageBreak вставляет разрыв страницы в текущей позиции.
func (d *Document) PageBreak() {
	d.PageBreaks = append(d.PageBreaks, d.CurrentRow)
}

// CheckOutput проверяет, поместится ли область на текущей странице (50 строк/стр.).
func (d *Document) CheckOutput(area *Area) bool {
	if area == nil {
		return true
	}
	areaHeight := area.Rows()
	rowsRemaining := 50 - (d.CurrentRow % 50)
	return float64(areaHeight) <= float64(rowsRemaining)
}

// EndPage завершает текущую страницу и переходит к началу следующей.
func (d *Document) EndPage() {
	nextPage := (d.CurrentRow/50 + 1) * 50
	d.CurrentRow = nextPage
	d.CurrentCol = 0
}

// RepeatOnPrint помечает область как повторяемую на каждой странице.
func (d *Document) RepeatOnPrint(area *Area, repeat bool) {
	if area != nil && repeat {
		d.RepeatHeader = true
		d.HeaderArea = area
	}
}

// Draw вставляет рисунок в текущую позицию и сдвигает колонку.
func (d *Document) Draw(picture string) {
	cell := d.GetOrCreateCell(d.CurrentRow, d.CurrentCol)
	cell.Picture = picture
	d.CurrentCol++
}

// GetPicture извлекает первый рисунок из области.
func (d *Document) GetPicture(area *Area) string {
	if area == nil {
		return ""
	}
	for row := area.Top; row <= area.Bottom; row++ {
		for col := area.Left; col <= area.Right; col++ {
			if cell := d.GetCell(row, col); cell != nil && cell.Picture != "" {
				return cell.Picture
			}
		}
	}
	return ""
}

// SetColumnWidth задаёт ширину колонки (1-based) — пишется во все строки сетки.
func (d *Document) SetColumnWidth(col int, width float64) {
	for row := 0; row < d.RowCount; row++ {
		cell := d.GetOrCreateCell(row, col-1)
		cell.Width = width
	}
}

// SetRowHeight задаёт высоту строки (1-based) — пишется во все колонки сетки.
func (d *Document) SetRowHeight(row int, height float64) {
	for col := 0; col < d.ColCount; col++ {
		cell := d.GetOrCreateCell(row-1, col)
		cell.Height = height
	}
}

// SetAlign задаёт выравнивание для прямоугольника области (координаты 0-based).
func (d *Document) SetAlign(area *Area, hAlign, vAlign string) {
	if area == nil {
		return
	}
	for row := area.Top; row <= area.Bottom; row++ {
		for col := area.Left; col <= area.Right; col++ {
			if cell := d.GetOrCreateCell(row, col); cell != nil {
				cell.Align = strings.ToLower(hAlign)
				cell.Vertical = strings.ToLower(vAlign)
			}
		}
	}
}

// Merge объединяет ячейки прямоугольника (1-based координаты, как в DSL).
func (d *Document) Merge(top, left, bottom, right int) {
	if top < 0 || left < 0 || bottom < top || right < left {
		return
	}
	top--
	left--
	bottom--
	right--

	if cell := d.GetOrCreateCell(top, left); cell != nil {
		cell.ColSpan = right - left + 1
		cell.RowSpan = bottom - top + 1
	}
}

// ContentBounds возвращает максимальные индексы строки и колонки с содержимым
// (с учётом colSpan).
func (d *Document) ContentBounds() (int, int) {
	maxRow, maxCol := 0, 0
	for key, cell := range d.Cells {
		if cell == nil {
			continue
		}
		var r, c int
		fmt.Sscanf(key, "%d,%d", &r, &c)
		extent := c + cell.ColSpan - 1
		if cell.ColSpan <= 0 {
			extent = c
		}
		if r > maxRow {
			maxRow = r
		}
		if extent > maxCol {
			maxCol = extent
		}
	}
	return maxRow, maxCol
}

// TextString возвращает табличный текст документа (колонки через \t, строки \n).
func (d *Document) TextString() string {
	var sb strings.Builder
	maxRow, maxCol := d.ContentBounds()
	for row := 0; row <= maxRow; row++ {
		for col := 0; col <= maxCol; col++ {
			if cell := d.GetCell(row, col); cell != nil {
				sb.WriteString(cell.Text)
				sb.WriteString("\t")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
