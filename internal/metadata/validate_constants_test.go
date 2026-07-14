package metadata

import "testing"

func TestValidateConstants(t *testing.T) {
	entities := []*Entity{{Name: "Организация"}}
	enums := []*Enum{{Name: "СтавкаНДС", Values: []string{"20", "10"}}}

	okConsts := []*Constant{
		{Name: "Орг", Type: "reference:Организация", RefEntity: "Организация"},
		{Name: "НДС", Type: "enum:СтавкаНДС", EnumName: "СтавкаНДС"},
		{Name: "Валюта", Type: FieldTypeString},
	}
	if err := ValidateConstants(okConsts, entities, enums); err != nil {
		t.Fatalf("валидные константы отклонены: %v", err)
	}

	if err := ValidateConstants(
		[]*Constant{{Name: "Орг", Type: "reference:Нет", RefEntity: "Нет"}},
		entities, enums,
	); err == nil {
		t.Error("ссылка на несуществующую сущность должна отклоняться")
	}

	if err := ValidateConstants(
		[]*Constant{{Name: "НДС", Type: "enum:Нет", EnumName: "Нет"}},
		entities, enums,
	); err == nil {
		t.Error("ссылка на несуществующее перечисление должна отклоняться")
	}

	// Project.Load всегда передаёт полный список перечислений, поэтому ссылка на
	// enum должна отклоняться и когда в проекте нет ни одного перечисления.
	if err := ValidateConstants(
		[]*Constant{{Name: "НДС", Type: "enum:Нет", EnumName: "Нет"}},
		entities, nil,
	); err == nil {
		t.Error("ссылка на enum при пустом списке перечислений должна отклоняться")
	}
}
