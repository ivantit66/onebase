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

	// Гвард как у реквизитов: если перечисления вообще не загружены, enum-типы
	// не сверяются (не даём ложных срабатываний на частичной загрузке).
	if err := ValidateConstants(
		[]*Constant{{Name: "НДС", Type: "enum:Нет", EnumName: "Нет"}},
		entities, nil,
	); err != nil {
		t.Errorf("при пустом списке перечислений enum-константа не должна падать: %v", err)
	}
}
