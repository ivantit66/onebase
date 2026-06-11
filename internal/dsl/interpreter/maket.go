package interpreter

import (
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/printform"
)

// Макет wraps a LayoutTemplate as a DSL-accessible object.
// DSL code uses it: Макет.Область("Заголовок") → returns SpreadsheetDocumentArea.
type Макет struct {
	template *printform.LayoutTemplate
}

// NewMaket creates a Макет DSL object from a layout template.
func NewMaket(lt *printform.LayoutTemplate) *Макет {
	return &Макет{template: lt}
}

// InjectMaket adds the «Макет» DSL variable to vars when a layout template is
// present. With a nil layout it is a no-op (the variable is not added, so DSL
// without a макет behaves exactly as before). Used by all processor run paths
// (UI, CLI procrun, scheduler) to expose src/<имя>.proc.layout.yaml as Макет.
func InjectMaket(vars map[string]any, lt *printform.LayoutTemplate) {
	if vars == nil || lt == nil {
		return
	}
	vars["Макет"] = NewMaket(lt)
}

// Get allows property access: Макет.Заголовок → same as Макет.Область("Заголовок").
func (m *Макет) Get(field string) any {
	return m.getArea(field)
}

// Set is not supported on макет.
func (m *Макет) Set(field string, v any) {}

// CallMethod handles method calls on the макет.
func (m *Макет) CallMethod(name string, args []any) any {
	switch strings.ToLower(name) {
	case "область", "area", "получитьобласть", "getarea":
		if len(args) > 0 {
			return m.getArea(strArg(args, 0))
		}
	case "имя", "name":
		return m.template.Name
	}
	return nil
}

// getArea returns a FRESH SpreadsheetDocumentArea for the named layout area.
// Each call creates a new area with its own cell storage, so the same area
// definition can be used multiple times with different parameter values (e.g., in loops).
func (m *Макет) getArea(name string) *SpreadsheetDocumentArea {
	if m.template == nil || m.template.Areas == nil {
		return nil
	}
	areaDef, ok := m.template.Areas[strings.ToLower(name)]
	if !ok {
		// Try case-sensitive
		for k, v := range m.template.Areas {
			if strings.EqualFold(k, name) {
				areaDef = v
				ok = true
				break
			}
		}
	}
	if !ok {
		return nil
	}

	rows := len(areaDef.Rows)
	cols := 0
	for _, row := range areaDef.Rows {
		c := 0
		for _, cell := range row.Cells {
			if cell.ColSpan > 1 {
				c += cell.ColSpan
			} else {
				c++
			}
		}
		if c > cols {
			cols = c
		}
	}

	area := &SpreadsheetDocumentArea{
		cells:  make(map[string]*SpreadsheetDocumentCell),
		top:    0,
		left:   0,
		bottom: rows - 1,
		right:  cols - 1,
	}

	for r, row := range areaDef.Rows {
		colIdx := 0
		for _, cellDef := range row.Cells {
			cell := NewSpreadsheetDocumentCell(cellDef.Text)
			cell.bold = cellDef.Bold
			cell.italic = cellDef.Italic
			if cellDef.Align != "" {
				cell.align = strings.ToLower(cellDef.Align)
			}
			if cellDef.VAlign != "" {
				cell.vertical = strings.ToLower(cellDef.VAlign)
			}
			if cellDef.FontSize > 0 {
				cell.fontSize = cellDef.FontSize
			}
			if cellDef.FontFamily != "" {
				cell.fontFamily = cellDef.FontFamily
			}
			if cellDef.BackColor != "" {
				cell.backColor = cellDef.BackColor
			}
			if cellDef.TextColor != "" {
				cell.textColor = cellDef.TextColor
			}
			if cellDef.Border != "" {
				cell.border = strings.ToLower(cellDef.Border)
			}
			if cellDef.ColSpan > 1 {
				cell.colSpan = cellDef.ColSpan
			}
			if cellDef.RowSpan > 1 {
				cell.rowSpan = cellDef.RowSpan
			}
			// Store parameter name for named parameter access
			if cellDef.Parameter != "" {
				cell.parameterName = cellDef.Parameter
			}

			key := fmt.Sprintf("%d,%d", r, colIdx)
			area.cells[key] = cell

			colIdx += cell.colSpan
			if cell.colSpan <= 0 {
				colIdx++
			}
		}
	}

	return area
}
