package cli

import (
	"encoding/json"
	"os"
	"sort"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/spf13/cobra"
)

var describeCmd = &cobra.Command{
	Use:   "describe",
	Short: "Выгрузить структуру конфигурации в JSON (для ИИ-инструментов)",
	Long: `Сериализует всю метаданность конфигурации (справочники, документы,
регистры, перечисления, константы, отчёты, обработки, подсистемы, журналы,
модули с именами процедур) и список встроенных функций DSL в JSON.
Read-only «рентген» — контракт для ИИ и будущего MCP.

Примеры:
  onebase describe --project C:\Projects\OneBaseConfs\PuT
  onebase describe --id <baseID> | jq .processors`,
	RunE:          runDescribe,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	addBaseFlags(describeCmd)
	rootCmd.AddCommand(describeCmd)
}

type descField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Ref  string `json:"ref,omitempty"`
	Enum string `json:"enum,omitempty"`
}

type descTablePart struct {
	Name   string      `json:"name"`
	Fields []descField `json:"fields"`
}

type descEntity struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Hierarchical bool            `json:"hierarchical,omitempty"`
	Posting      bool            `json:"posting,omitempty"`
	Fields       []descField     `json:"fields"`
	TableParts   []descTablePart `json:"tableParts,omitempty"`
	BasedOn      []string        `json:"basedOn,omitempty"`
}

type descRegister struct {
	Name       string      `json:"name"`
	Dimensions []descField `json:"dimensions,omitempty"`
	Resources  []descField `json:"resources,omitempty"`
	Attributes []descField `json:"attributes,omitempty"`
}

type descInfoReg struct {
	Name       string      `json:"name"`
	Periodic   bool        `json:"periodic,omitempty"`
	Dimensions []descField `json:"dimensions,omitempty"`
	Resources  []descField `json:"resources,omitempty"`
}

type descNamedValues struct {
	Name   string   `json:"name"`
	Values []string `json:"values,omitempty"`
}

type descConstant struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type descParam struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Label   string   `json:"label,omitempty"`
	Options []string `json:"options,omitempty"`
}

type descProcessor struct {
	Name   string      `json:"name"`
	Params []descParam `json:"params,omitempty"`
}

type descModule struct {
	Name       string   `json:"name"`
	Procedures []string `json:"procedures,omitempty"`
}

type describeOutput struct {
	Catalogs      []descEntity      `json:"catalogs"`
	Documents     []descEntity      `json:"documents"`
	Registers     []descRegister    `json:"registers"`
	InfoRegisters []descInfoReg     `json:"infoRegisters"`
	Enums         []descNamedValues `json:"enums"`
	Constants     []descConstant    `json:"constants"`
	Reports       []string          `json:"reports"`
	Processors    []descProcessor   `json:"processors"`
	Subsystems    []string          `json:"subsystems"`
	Journals      []string          `json:"journals"`
	Widgets       []string          `json:"widgets"`
	Modules       []descModule      `json:"modules"`
	Builtins      []string          `json:"builtins"`
}

func runDescribe(cmd *cobra.Command, _ []string) error {
	bc, err := resolveBase(cmd)
	if err != nil {
		return err
	}
	defer bc.Cleanup()

	proj, err := project.Load(bc.Dir)
	if err != nil {
		return err
	}
	defer proj.Close()

	out := describeOutput{
		Catalogs:      []descEntity{},
		Documents:     []descEntity{},
		Registers:     []descRegister{},
		InfoRegisters: []descInfoReg{},
		Enums:         []descNamedValues{},
		Constants:     []descConstant{},
		Reports:       []string{},
		Processors:    []descProcessor{},
		Subsystems:    []string{},
		Journals:      []string{},
		Widgets:       []string{},
		Modules:       []descModule{},
	}

	for _, e := range proj.Entities {
		de := descEntity{
			Name: e.Name, Title: e.Title,
			Hierarchical: e.Hierarchical, Posting: e.Posting,
			Fields:  toDescFields(e.Fields),
			BasedOn: e.BasedOn,
		}
		for _, tp := range e.TableParts {
			de.TableParts = append(de.TableParts, descTablePart{Name: tp.Name, Fields: toDescFields(tp.Fields)})
		}
		if e.Kind == metadata.KindDocument {
			out.Documents = append(out.Documents, de)
		} else {
			out.Catalogs = append(out.Catalogs, de)
		}
	}
	for _, r := range proj.Registers {
		out.Registers = append(out.Registers, descRegister{
			Name: r.Name, Dimensions: toDescFields(r.Dimensions),
			Resources: toDescFields(r.Resources), Attributes: toDescFields(r.Attributes),
		})
	}
	for _, ir := range proj.InfoRegisters {
		out.InfoRegisters = append(out.InfoRegisters, descInfoReg{
			Name: ir.Name, Periodic: ir.Periodic,
			Dimensions: toDescFields(ir.Dimensions), Resources: toDescFields(ir.Resources),
		})
	}
	for _, en := range proj.Enums {
		out.Enums = append(out.Enums, descNamedValues{Name: en.Name, Values: en.Values})
	}
	for _, c := range proj.Constants {
		out.Constants = append(out.Constants, descConstant{Name: c.Name, Type: string(c.Type)})
	}
	for _, rep := range proj.Reports {
		out.Reports = append(out.Reports, rep.Name)
	}
	for _, p := range proj.Processors {
		dp := descProcessor{Name: p.Name}
		for _, par := range p.Params {
			dp.Params = append(dp.Params, descParam{Name: par.Name, Type: par.Type, Label: par.Label, Options: par.Options})
		}
		out.Processors = append(out.Processors, dp)
	}
	for _, s := range proj.Subsystems {
		out.Subsystems = append(out.Subsystems, s.Name)
	}
	for _, j := range proj.Journals {
		out.Journals = append(out.Journals, j.Name)
	}
	for _, w := range proj.Widgets {
		out.Widgets = append(out.Widgets, w.Name)
	}

	// Модули: общие модули + модули объектов/обработок (имена процедур).
	for name, prog := range proj.Modules {
		out.Modules = append(out.Modules, descModule{Name: name, Procedures: procNames(prog)})
	}
	for name, prog := range proj.Programs {
		out.Modules = append(out.Modules, descModule{Name: name, Procedures: procNames(prog)})
	}
	sort.Slice(out.Modules, func(i, j int) bool { return out.Modules[i].Name < out.Modules[j].Name })

	for n := range interpreter.KnownBuiltinNames() {
		out.Builtins = append(out.Builtins, n)
	}
	sort.Strings(out.Builtins)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func toDescFields(fields []metadata.Field) []descField {
	out := make([]descField, 0, len(fields))
	for _, f := range fields {
		out = append(out, descField{Name: f.Name, Type: string(f.Type), Ref: f.RefEntity, Enum: f.EnumName})
	}
	return out
}

func procNames(prog *ast.Program) []string {
	if prog == nil {
		return nil
	}
	names := make([]string, 0, len(prog.Procedures))
	for _, p := range prog.Procedures {
		names = append(names, p.Name.Literal)
	}
	return names
}
