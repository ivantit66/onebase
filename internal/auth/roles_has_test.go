package auth

import "testing"

func TestUserHas(t *testing.T) {
	role := &Role{
		Name: "Менеджер",
		Permissions: Permission{
			Catalogs:  map[string][]string{"Контрагенты": {"read", "write"}},
			Documents: map[string][]string{"Реализация": {"read", "post"}},
			Registers: map[string][]string{"Продажи": {"read"}},
			InfoRegs:  map[string][]string{"Курсы": {"read", "write", "delete"}},
			Reports:   map[string][]string{"Остатки": {"run"}},
		},
	}
	u := &User{Roles: []*Role{role}}

	cases := []struct {
		kind, entity, op string
		want             bool
	}{
		{"catalog", "Контрагенты", "read", true},
		{"catalog", "Контрагенты", "write", true},
		{"catalog", "Контрагенты", "delete", false},
		{"catalog", "Номенклатура", "read", false}, // entity not granted
		{"document", "Реализация", "read", true},
		{"document", "Реализация", "post", true},
		{"document", "Реализация", "write", false},
		{"register", "Продажи", "read", true},
		{"register", "Продажи", "write", false},
		{"inforeg", "Курсы", "delete", true},
		{"report", "Остатки", "run", true},
		{"report", "Остатки", "read", false},
		{"unknownkind", "X", "read", false},
	}
	for _, c := range cases {
		if got := u.Has(c.kind, c.entity, c.op); got != c.want {
			t.Errorf("Has(%q,%q,%q)=%v, want %v", c.kind, c.entity, c.op, got, c.want)
		}
	}

	// Admin bypasses all checks.
	admin := &User{IsAdmin: true}
	if !admin.Has("catalog", "Что угодно", "delete") {
		t.Error("admin should pass any permission check")
	}

	// User with no roles is denied everything.
	none := &User{}
	if none.Has("catalog", "Контрагенты", "read") {
		t.Error("user without roles must be denied")
	}

	// Permissions union across multiple roles.
	multi := &User{Roles: []*Role{
		{Permissions: Permission{Catalogs: map[string][]string{"A": {"read"}}}},
		{Permissions: Permission{Catalogs: map[string][]string{"B": {"write"}}}},
	}}
	if !multi.Has("catalog", "A", "read") || !multi.Has("catalog", "B", "write") {
		t.Error("permissions should union across roles")
	}
}
