package metadata

import "fmt"

func Validate(entities []*Entity, enums []*Enum) error {
	entityNames := make(map[string]bool, len(entities))
	for _, e := range entities {
		entityNames[e.Name] = true
	}
	enumNames := make(map[string]bool, len(enums))
	for _, en := range enums {
		enumNames[en.Name] = true
	}
	for _, e := range entities {
		for _, f := range e.Fields {
			if f.RefEntity != "" && !entityNames[f.RefEntity] {
				return fmt.Errorf("entity %s: field %s references unknown entity %s", e.Name, f.Name, f.RefEntity)
			}
			if f.EnumName != "" && len(enums) > 0 && !enumNames[f.EnumName] {
				return fmt.Errorf("entity %s: field %s references unknown enum %s", e.Name, f.Name, f.EnumName)
			}
		}
		for _, tp := range e.TableParts {
			for _, f := range tp.Fields {
				if IsRichText(f.Type) {
					return fmt.Errorf("поле %s.%s: тип richtext не поддерживается в табличных частях", tp.Name, f.Name)
				}
				if IsImage(f.Type) {
					return fmt.Errorf("поле %s.%s: тип image не поддерживается в табличных частях", tp.Name, f.Name)
				}
			}
		}
		for _, src := range e.BasedOn {
			if !entityNames[src] {
				return fmt.Errorf("entity %s: based_on references unknown entity %s", e.Name, src)
			}
		}
	}
	return nil
}
