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
		if err := validateTileView(e); err != nil {
			return err
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

func validateTileView(e *Entity) error {
	if e == nil || e.TileView == nil {
		return nil
	}
	if e.TileView.Image != "" {
		f := findEntityField(e, e.TileView.Image)
		if f == nil {
			return fmt.Errorf("entity %s: tile_view.image references unknown field %s", e.Name, e.TileView.Image)
		}
		if !IsImage(f.Type) {
			return fmt.Errorf("entity %s: tile_view.image field %s must have type image", e.Name, e.TileView.Image)
		}
	}
	for _, item := range []struct {
		role string
		name string
	}{
		{"title", e.TileView.Title},
		{"subtitle", e.TileView.Subtitle},
	} {
		if item.name == "" {
			continue
		}
		if findEntityField(e, item.name) == nil {
			return fmt.Errorf("entity %s: tile_view.%s references unknown field %s", e.Name, item.role, item.name)
		}
	}
	for _, name := range e.TileView.Fields {
		if findEntityField(e, name) == nil {
			return fmt.Errorf("entity %s: tile_view.fields references unknown field %s", e.Name, name)
		}
	}
	return nil
}

func findEntityField(e *Entity, name string) *Field {
	for i := range e.Fields {
		if e.Fields[i].Name == name {
			return &e.Fields[i]
		}
	}
	return nil
}
