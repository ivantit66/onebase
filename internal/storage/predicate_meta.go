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

// AccountRegisterPredicateEntity adapts accounting-register metadata to the
// field model used by row-level predicates. Physical accounting-register columns
// use Russian names, so aliases are handled by predicateColumn.
func AccountRegisterPredicateEntity(ar *metadata.AccountRegister) *metadata.Entity {
	if ar == nil {
		return nil
	}
	fields := []metadata.Field{
		{Name: "period", Type: metadata.FieldTypeDate},
		{Name: "регистратор", Type: metadata.FieldTypeString, RefEntity: "_uuid"},
		{Name: "регистратор_тип", Type: metadata.FieldTypeString},
		{Name: "счётдт", Type: metadata.FieldTypeString},
		{Name: "счёткт", Type: metadata.FieldTypeString},
	}
	fields = append(fields, ar.Resources...)
	for i, s := range ar.Subconto {
		f := s
		f.Name = metadata.SubcontoColumn(i + 1)
		fields = append(fields, f)
	}
	return &metadata.Entity{Name: ar.Name, Kind: metadata.Kind("register"), Fields: fields}
}
