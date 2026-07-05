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

func TestUserHas_CommaSeparatedOperations(t *testing.T) {
	u := &User{Roles: []*Role{{
		Permissions: Permission{
			Documents:  map[string][]string{"ВходящееПисьмо": {"read,write", "post, unpost"}},
			Processors: map[string][]string{"Заполнить": {"run"}},
		},
	}}}

	for _, op := range []string{"read", "write", "post", "unpost"} {
		if !u.Has("document", "ВходящееПисьмо", op) {
			t.Fatalf("document permission %q from comma-separated token must pass", op)
		}
	}
	if u.Has("document", "ВходящееПисьмо", "delete") {
		t.Fatal("delete was not granted")
	}
	if !u.Has("processor", "Заполнить", "run") {
		t.Fatal("processor run permission must still pass")
	}
}

func TestUnmarshalPermissions_LegacyRussianKeys(t *testing.T) {
	p := unmarshalPermissions([]byte(`{
		"Политики": {
			"Справочники": {"ПользователиСЭД": ["read"]},
			"Документы": {"ВходящееПисьмо": ["read,write"], "ИсходящееПисьмо": "read"},
			"Регистры": {"ДвиженияДокументов": ["read"]},
			"РегистрыСведений": {"НастройкиПользователей": ["read, write"]},
			"Отчёты": {"ЖурналПисем": ["run"]},
			"Обработки": {"ЗаполнитьДемо": ["run"]}
		}
	}`))

	u := &User{Roles: []*Role{{Permissions: p}}}
	cases := []struct {
		kind, entity, op string
	}{
		{"catalog", "ПользователиСЭД", "read"},
		{"document", "ВходящееПисьмо", "read"},
		{"document", "ВходящееПисьмо", "write"},
		{"document", "ИсходящееПисьмо", "read"},
		{"register", "ДвиженияДокументов", "read"},
		{"inforeg", "НастройкиПользователей", "write"},
		{"report", "ЖурналПисем", "run"},
		{"processor", "ЗаполнитьДемо", "run"},
	}
	for _, c := range cases {
		if !u.Has(c.kind, c.entity, c.op) {
			t.Fatalf("legacy permission %s/%s/%s must pass; parsed=%+v", c.kind, c.entity, c.op, p)
		}
	}
}

func TestUserAllowsAIDataAccess(t *testing.T) {
	if (&User{}).AllowsAIDataAccess() {
		t.Fatal("user without explicit AI data access must be denied")
	}
	if !(&User{IsAdmin: true}).AllowsAIDataAccess() {
		t.Fatal("admin must be allowed")
	}
	if !(&User{AIDataAccess: true}).AllowsAIDataAccess() {
		t.Fatal("user-level AIDataAccess must be allowed")
	}
	u := &User{Roles: []*Role{{Permissions: Permission{AIDataAccess: true}}}}
	if !u.AllowsAIDataAccess() {
		t.Fatal("role-level ai_data_access must be allowed")
	}
}
