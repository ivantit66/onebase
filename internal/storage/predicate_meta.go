package storage

import "github.com/ivantit66/onebase/internal/metadata"

// RegisterPredicateEntity adapts register metadata to the field model used by
// row-level predicates. The returned value is not a persisted entity.
func RegisterPredicateEntity(reg *metadata.Register) *metadata.Entity {
	if reg == nil {
		return nil
	}
	fields := []metadata.Field{
		{Name: "period", Type: metadata.FieldTypeDate},
		{Name: "recorder", Type: metadata.FieldTypeString, RefEntity: "_uuid"},
		{Name: "recorder_type", Type: metadata.FieldTypeString},
		{Name: "line_number", Type: metadata.FieldTypeNumber},
		{Name: "вид_движения", Type: metadata.FieldTypeString},
	}
	fields = append(fields, reg.Dimensions...)
	fields = append(fields, reg.Resources...)
	fields = append(fields, reg.Attributes...)
	return &metadata.Entity{Name: reg.Name, Kind: metadata.Kind("register"), Fields: fields}
}

// InfoRegisterPredicateEntity adapts information-register metadata to the field
// model used by row-level predicates. The returned value is not a persisted entity.
func InfoRegisterPredicateEntity(ir *metadata.InfoRegister) *metadata.Entity {
	if ir == nil {
		return nil
	}
	fields := []metadata.Field{
		{Name: "recorder", Type: metadata.FieldTypeString, RefEntity: "_uuid"},
		{Name: "recorder_type", Type: metadata.FieldTypeString},
	}
	if ir.Periodic {
		fields = append([]metadata.Field{{Name: "period", Type: metadata.FieldTypeDate}}, fields...)
	}
	fields = append(fields, ir.Dimensions...)
	fields = append(fields, ir.Resources...)
	return &metadata.Entity{Name: ir.Name, Kind: metadata.Kind("inforeg"), Fields: fields}
}
