package printform

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LayoutTemplate defines a макет (template) with named areas for print forms.
// Each area contains rows of cells with static text or parameter placeholders.
type LayoutTemplate struct {
	Name     string                 `yaml:"name"`
	Document string                 `yaml:"document,omitempty"`
	Columns  []LayoutColumn         `yaml:"columns,omitempty"`
	Areas    map[string]*LayoutArea `yaml:"areas"`
}

// LayoutColumn defines column-level properties (width applies to all areas).
type LayoutColumn struct {
	Width string `yaml:"width,omitempty"` // CSS value: "120px", "10%", "auto"
}

// LayoutArea defines a named rectangular area with rows of cells.
type LayoutArea struct {
	Rows []LayoutRow `yaml:"rows"`
}

// LayoutRow is a single row of cells in a layout area.
type LayoutRow struct {
	Height string       `yaml:"height,omitempty"` // CSS value: "20px", "auto"
	Cells  []LayoutCell `yaml:"cells"`
}

// LayoutCell defines a single cell in a layout template.
// A cell can contain either static text or a named parameter placeholder.
type LayoutCell struct {
	Text        string `yaml:"text,omitempty"`
	Parameter   string `yaml:"parameter,omitempty"`
	Bold        bool   `yaml:"bold,omitempty"`
	Italic      bool   `yaml:"italic,omitempty"`
	Align       string `yaml:"align,omitempty"`       // left/center/right
	VAlign      string `yaml:"valign,omitempty"`      // top/middle/bottom
	FontSize    int    `yaml:"fontSize,omitempty"`
	FontFamily  string `yaml:"fontFamily,omitempty"`  // e.g. "Arial", "Times New Roman"
	BackColor   string `yaml:"backColor,omitempty"`
	TextColor   string `yaml:"textColor,omitempty"`
	ColSpan     int    `yaml:"colspan,omitempty"`
	RowSpan     int    `yaml:"rowspan,omitempty"`
	Border      string `yaml:"border,omitempty"`      // none/all/thin/thick
	BorderColor string `yaml:"borderColor,omitempty"` // CSS color, default #ccc
}

// LoadLayout loads a layout template from a YAML file.
func LoadLayout(path string) (*LayoutTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("layout: read %s: %w", path, err)
	}
	var lt LayoutTemplate
	if err := yaml.Unmarshal(data, &lt); err != nil {
		return nil, fmt.Errorf("layout: parse %s: %w", path, err)
	}
	if lt.Name == "" {
		lt.Name = strings.TrimSuffix(filepath.Base(path), ".layout.yaml")
	}
	return &lt, nil
}

// FindLayoutFile looks for a .layout.yaml file matching the given .os file path.
// For "накладная.os", it looks for "накладная.layout.yaml" in the same directory.
func FindLayoutFile(osPath string) string {
	dir := filepath.Dir(osPath)
	base := strings.TrimSuffix(filepath.Base(osPath), ".os")
	layoutPath := filepath.Join(dir, base+".layout.yaml")
	if _, err := os.Stat(layoutPath); err == nil {
		return layoutPath
	}
	return ""
}

// borderCSS returns the CSS border declaration for the given preset.
func borderCSS(preset, color string) string {
	if color == "" {
		color = "#ccc"
	}
	switch strings.ToLower(preset) {
	case "", "all", "thin":
		return "1px solid " + color
	case "thick":
		return "2px solid " + color
	case "none":
		return "none"
	default:
		return "1px solid " + color
	}
}

// PreviewHTML returns an HTML preview of the layout template showing all named areas.
func (lt *LayoutTemplate) PreviewHTML() string {
	var sb strings.Builder
	sb.WriteString(`<div style="font-family:Arial,sans-serif;font-size:12px">`)
	// Stable area ordering not required for preview; map iteration is acceptable.
	for areaName, area := range lt.Areas {
		sb.WriteString(fmt.Sprintf(
			`<div style="margin-bottom:16px"><div style="font-weight:bold;color:#4a9;margin-bottom:4px">%s</div>`,
			html.EscapeString(areaName),
		))
		sb.WriteString(`<table style="border-collapse:collapse">`)
		// <colgroup> for column widths.
		if len(lt.Columns) > 0 {
			sb.WriteString("<colgroup>")
			for _, c := range lt.Columns {
				if c.Width != "" {
					sb.WriteString(fmt.Sprintf(`<col style="width:%s">`, html.EscapeString(c.Width)))
				} else {
					sb.WriteString("<col>")
				}
			}
			sb.WriteString("</colgroup>")
		}
		for _, row := range area.Rows {
			if row.Height != "" {
				sb.WriteString(fmt.Sprintf(`<tr style="height:%s">`, html.EscapeString(row.Height)))
			} else {
				sb.WriteString("<tr>")
			}
			for _, cell := range row.Cells {
				style := "padding:4px 8px;min-width:40px;"
				style += "border:" + borderCSS(cell.Border, cell.BorderColor) + ";"
				if cell.Bold {
					style += "font-weight:bold;"
				}
				if cell.Italic {
					style += "font-style:italic;"
				}
				if cell.FontSize > 0 {
					style += fmt.Sprintf("font-size:%dpt;", cell.FontSize)
				}
				if cell.FontFamily != "" {
					style += "font-family:" + cell.FontFamily + ";"
				}
				if cell.BackColor != "" {
					style += "background-color:" + cell.BackColor + ";"
				}
				if cell.TextColor != "" {
					style += "color:" + cell.TextColor + ";"
				}
				if cell.Align != "" {
					style += "text-align:" + cell.Align + ";"
				}
				if cell.VAlign != "" {
					switch strings.ToLower(cell.VAlign) {
					case "middle", "center":
						style += "vertical-align:middle;"
					case "top":
						style += "vertical-align:top;"
					case "bottom":
						style += "vertical-align:bottom;"
					}
				}
				attrs := ""
				if cell.ColSpan > 1 {
					attrs += fmt.Sprintf(` colspan="%d"`, cell.ColSpan)
				}
				if cell.RowSpan > 1 {
					attrs += fmt.Sprintf(` rowspan="%d"`, cell.RowSpan)
				}
				var text string
				if cell.Parameter != "" {
					text = fmt.Sprintf(`<span style="color:#888">[%s]</span>`, html.EscapeString(cell.Parameter))
				} else if cell.Text != "" {
					text = html.EscapeString(cell.Text)
				} else {
					text = "&nbsp;"
				}
				sb.WriteString(fmt.Sprintf(`<td style="%s"%s>%s</td>`, style, attrs, text))
			}
			sb.WriteString("</tr>")
		}
		sb.WriteString("</table></div>")
	}
	sb.WriteString("</div>")
	return sb.String()
}
