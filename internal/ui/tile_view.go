package ui

import "github.com/ivantit66/onebase/internal/metadata"

type tileViewRender struct {
	ImageFields   []metadata.Field
	TitleField    *metadata.Field
	SubtitleField *metadata.Field
	Fields        []metadata.Field
}

func resolveTileView(entity *metadata.Entity) tileViewRender {
	var out tileViewRender
	if entity == nil {
		return out
	}
	cfg := entity.TileView
	if cfg == nil {
		for _, f := range entity.Fields {
			if metadata.IsImage(f.Type) {
				out.ImageFields = append(out.ImageFields, f)
				continue
			}
			if out.TitleField == nil {
				out.TitleField = fieldPtr(f)
				continue
			}
			out.Fields = append(out.Fields, f)
		}
		return out
	}

	if cfg.Image != "" {
		if f := uiFieldByName(entity, cfg.Image); f != nil && metadata.IsImage(f.Type) {
			out.ImageFields = append(out.ImageFields, *f)
		}
	} else if f := firstImageField(entity); f != nil {
		out.ImageFields = append(out.ImageFields, *f)
	}

	if cfg.Title != "" {
		out.TitleField = uiFieldByName(entity, cfg.Title)
	}
	if out.TitleField == nil {
		out.TitleField = firstNonImageField(entity)
	}
	if cfg.Subtitle != "" {
		out.SubtitleField = uiFieldByName(entity, cfg.Subtitle)
	}

	if cfg.FieldsSet || len(cfg.Fields) > 0 {
		for _, name := range cfg.Fields {
			if f := uiFieldByName(entity, name); f != nil && !tileFieldUsed(out, f.Name) {
				out.Fields = append(out.Fields, *f)
			}
		}
		return out
	}

	for _, f := range entity.Fields {
		if metadata.IsImage(f.Type) || tileFieldUsed(out, f.Name) {
			continue
		}
		out.Fields = append(out.Fields, f)
	}
	return out
}

func fieldPtr(f metadata.Field) *metadata.Field {
	ff := f
	return &ff
}

func uiFieldByName(entity *metadata.Entity, name string) *metadata.Field {
	for i := range entity.Fields {
		if entity.Fields[i].Name == name {
			return &entity.Fields[i]
		}
	}
	return nil
}

func firstImageField(entity *metadata.Entity) *metadata.Field {
	for i := range entity.Fields {
		if metadata.IsImage(entity.Fields[i].Type) {
			return &entity.Fields[i]
		}
	}
	return nil
}

func firstNonImageField(entity *metadata.Entity) *metadata.Field {
	for i := range entity.Fields {
		if !metadata.IsImage(entity.Fields[i].Type) {
			return &entity.Fields[i]
		}
	}
	return nil
}

func tileFieldUsed(view tileViewRender, name string) bool {
	if view.TitleField != nil && view.TitleField.Name == name {
		return true
	}
	if view.SubtitleField != nil && view.SubtitleField.Name == name {
		return true
	}
	for _, f := range view.ImageFields {
		if f.Name == name {
			return true
		}
	}
	return false
}
