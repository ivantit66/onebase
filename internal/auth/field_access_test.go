package auth

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPermissionFieldAccessYAMLandJSONRoundTrip(t *testing.T) {
	src := []byte(`
name: Оператор КЦ
permissions:
  catalogs:
    Клиент: [read, disclose]
  field_access:
    catalogs:
      Клиент:
        Телефон: { read: mask_tail, keep: 4 }
        Адрес:   { read: mask_city }
        Паспорт: { read: hide }
    documents:
      Заявка:
        Телефон: { read: mask_tail, keep: 4 }
`)
	var role Role
	if err := yaml.Unmarshal(src, &role); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	pol := role.Permissions.FieldAccess.Policies("catalog", "Клиент")
	if pol == nil {
		t.Fatal("field_access for Клиент not parsed")
	}
	if pol["Телефон"].Read != "mask_tail" || pol["Телефон"].Keep != 4 {
		t.Fatalf("Телефон policy = %+v", pol["Телефон"])
	}
	if pol["Паспорт"].Read != "hide" {
		t.Fatalf("Паспорт policy = %+v", pol["Паспорт"])
	}
	if role.Permissions.FieldAccess.Policies("document", "Заявка")["Телефон"].Read != "mask_tail" {
		t.Fatal("document field_access not parsed")
	}
	// disclose survives as a plain object-level op alongside read.
	if !PermissionHas(role.Permissions, "catalog", "Клиент", "disclose") {
		t.Fatal("disclose op must be granted on Клиент")
	}

	raw, err := marshalPermissions(role.Permissions)
	if err != nil {
		t.Fatalf("marshalPermissions: %v", err)
	}
	got := unmarshalPermissions([]byte(raw))
	rpol := got.FieldAccess.Policies("catalog", "Клиент")
	if rpol["Телефон"].Read != "mask_tail" || rpol["Телефон"].Keep != 4 {
		t.Fatalf("json Телефон policy = %+v", rpol["Телефон"])
	}
	if rpol["Адрес"].Read != "mask_city" {
		t.Fatalf("json Адрес policy = %+v", rpol["Адрес"])
	}
}

func TestFieldAccessNormalisesStrategyCase(t *testing.T) {
	fa := FieldAccess{Catalogs: map[string]FieldPolicies{
		"Клиент": {"Телефон": {Read: "  MASK_TAIL ", Keep: 4}},
	}}
	fa = normalizeFieldAccess(fa)
	if got := fa.Policies("catalog", "Клиент")["Телефон"].Read; got != "mask_tail" {
		t.Fatalf("strategy not normalised: %q", got)
	}
}
