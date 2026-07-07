package access_test

import (
	"testing"

	"github.com/ivantit66/onebase/internal/access"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
)

func dealEntity() *metadata.Entity {
	return &metadata.Entity{
		Name: "Сделка",
		Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Ответственный", Type: metadata.FieldTypeString},
			{Name: "Автор", Type: metadata.FieldTypeString},
			{Name: "Подразделение", Type: metadata.FieldTypeString},
			{Name: "ФИО", Type: metadata.FieldTypeString},
		},
	}
}

// docRole строит роль с object-level правами на документ entity и, если задано,
// строковой политикой для операций.
func docRole(entity string, ops []string, policies auth.RowPolicies) *auth.Role {
	ra := auth.RowAccess{}
	if policies != nil {
		ra.Documents = map[string]auth.RowPolicies{entity: policies}
	}
	return &auth.Role{Permissions: auth.Permission{
		Documents: map[string][]string{entity: ops},
		RowAccess: ra,
	}}
}

func respPolicy() auth.RowPolicies {
	return auth.RowPolicies{"read": auth.RowPolicy{Field: "Ответственный", Op: "eq", Value: auth.RowValue{User: "id"}}}
}

func authorPolicy() auth.RowPolicies {
	return auth.RowPolicies{"read": auth.RowPolicy{Field: "Автор", Op: "eq", Value: auth.RowValue{User: "login"}}}
}

func TestDecide_AdminAndOpenDeploymentAreUnrestricted(t *testing.T) {
	meta := dealEntity()
	for _, u := range []*auth.User{nil, {IsAdmin: true}} {
		dec, err := access.Decide(u, "document", "Сделка", "read", meta)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if !dec.Allowed || !dec.Unrestricted || dec.Predicate != nil {
			t.Fatalf("admin/open must be allowed+unrestricted without predicate, got %+v", dec)
		}
	}
}

func TestDecide_GrantedWithoutPolicyIsUnrestricted(t *testing.T) {
	u := &auth.User{ID: "u1", Roles: []*auth.Role{docRole("Сделка", []string{"read"}, nil)}}
	dec, err := access.Decide(u, "document", "Сделка", "read", dealEntity())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !dec.Allowed || !dec.Unrestricted || dec.Predicate != nil {
		t.Fatalf("granted-but-unrestricted role must give open access, got %+v", dec)
	}
}

func TestDecide_RestrictedSingleRole(t *testing.T) {
	u := &auth.User{ID: "u1", Roles: []*auth.Role{docRole("Сделка", []string{"read"}, respPolicy())}}
	dec, err := access.Decide(u, "document", "Сделка", "read", dealEntity())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !dec.Allowed || dec.Unrestricted {
		t.Fatalf("restricted role must be allowed but not unrestricted, got %+v", dec)
	}
	if dec.Predicate == nil || dec.Predicate.Field != "Ответственный" || dec.Predicate.Op != "eq" || dec.Predicate.Value != "u1" {
		t.Fatalf("predicate = %+v, want Ответственный eq u1", dec.Predicate)
	}
}

func TestDecide_TwoRestrictedRolesMergeWithOR(t *testing.T) {
	u := &auth.User{
		ID:    "u1",
		Login: "ivan",
		Roles: []*auth.Role{
			docRole("Сделка", []string{"read"}, respPolicy()),
			docRole("Сделка", []string{"read"}, authorPolicy()),
		},
	}
	dec, err := access.Decide(u, "document", "Сделка", "read", dealEntity())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Unrestricted || dec.Predicate == nil {
		t.Fatalf("two restricted roles must stay restricted, got %+v", dec)
	}
	if len(dec.Predicate.Any) != 2 {
		t.Fatalf("restricted roles must be OR-merged into Any, got %+v", dec.Predicate)
	}
	if dec.Predicate.Any[0].Value != "u1" || dec.Predicate.Any[1].Value != "ivan" {
		t.Fatalf("OR members = %+v, want [id=u1, login=ivan]", dec.Predicate.Any)
	}
}

func TestDecide_UnrestrictedRoleOverridesRestricted(t *testing.T) {
	// Одна роль ограничивает, другая даёт read без политики — итог открытый (OR).
	u := &auth.User{
		ID: "u1",
		Roles: []*auth.Role{
			docRole("Сделка", []string{"read"}, respPolicy()),
			docRole("Сделка", []string{"read"}, nil),
		},
	}
	dec, err := access.Decide(u, "document", "Сделка", "read", dealEntity())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !dec.Allowed || !dec.Unrestricted || dec.Predicate != nil {
		t.Fatalf("an unrestricted granting role must win, got %+v", dec)
	}
}

func TestDecide_NoObjectPermissionIsNotAllowed(t *testing.T) {
	// Роль даёт права только на другой документ — на Сделку прав нет.
	u := &auth.User{ID: "u1", Roles: []*auth.Role{docRole("Заявка", []string{"read"}, nil)}}
	dec, err := access.Decide(u, "document", "Сделка", "read", dealEntity())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Allowed {
		t.Fatalf("no object-level grant must yield not-allowed, got %+v", dec)
	}
}

func TestDecide_InvalidPolicyFieldFailsClosed(t *testing.T) {
	bad := auth.RowPolicies{"read": auth.RowPolicy{Field: "НетТакогоПоля", Op: "eq", Value: auth.RowValue{User: "id"}}}
	u := &auth.User{ID: "u1", Roles: []*auth.Role{docRole("Сделка", []string{"read"}, bad)}}
	_, err := access.Decide(u, "document", "Сделка", "read", dealEntity())
	if err == nil {
		t.Fatal("policy on unknown field must fail closed with an error, got nil")
	}
}

func TestDecide_UserAttrPolicy(t *testing.T) {
	policies := auth.RowPolicies{"read": {
		Field: "Подразделение",
		Op:    "eq",
		Value: auth.RowValue{UserAttr: "department"},
	}}
	u := &auth.User{
		ID:    "u1",
		Attrs: map[string]any{"Department": "sales"},
		Roles: []*auth.Role{docRole("Сделка", []string{"read"}, policies)},
	}
	dec, err := access.Decide(u, "document", "Сделка", "read", dealEntity())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Predicate == nil || dec.Predicate.Field != "Подразделение" || dec.Predicate.Value != "sales" {
		t.Fatalf("predicate = %+v, want Подразделение eq sales", dec.Predicate)
	}
}

func TestDecide_UserAttrBuiltInPolicy(t *testing.T) {
	policies := auth.RowPolicies{"read": {
		Field: "ФИО",
		Op:    "eq",
		Value: auth.RowValue{UserAttr: "full_name"},
	}}
	u := &auth.User{
		ID:       "u1",
		FullName: "Иван Петров",
		Roles:    []*auth.Role{docRole("Сделка", []string{"read"}, policies)},
	}
	dec, err := access.Decide(u, "document", "Сделка", "read", dealEntity())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Predicate == nil || dec.Predicate.Value != "Иван Петров" {
		t.Fatalf("predicate = %+v, want full_name value", dec.Predicate)
	}
}

func TestDecide_UnknownUserAttrFailsClosed(t *testing.T) {
	policies := auth.RowPolicies{"read": {
		Field: "Подразделение",
		Op:    "eq",
		Value: auth.RowValue{UserAttr: "department"},
	}}
	u := &auth.User{ID: "u1", Roles: []*auth.Role{docRole("Сделка", []string{"read"}, policies)}}
	_, err := access.Decide(u, "document", "Сделка", "read", dealEntity())
	if err == nil {
		t.Fatal("policy with missing user_attr must fail closed")
	}
}

func TestValidatePolicyRejectsUnknownOperator(t *testing.T) {
	err := access.ValidatePolicy(auth.RowPolicy{
		Field: "Ответственный",
		Op:    "starts_with",
		Value: auth.RowValue{User: "login"},
	}, dealEntity())
	if err == nil {
		t.Fatal("unknown row policy operator must be rejected")
	}
}

func TestValidatePolicyRejectsUnknownUserAttr(t *testing.T) {
	err := access.ValidatePolicy(auth.RowPolicy{
		Field: "Подразделение",
		Op:    "eq",
		Value: auth.RowValue{UserAttr: "department"},
	}, dealEntity())
	if err == nil {
		t.Fatal("unknown row policy user_attr must be rejected")
	}
}

func TestValidatePolicyRejectsAmbiguousUserValue(t *testing.T) {
	err := access.ValidatePolicy(auth.RowPolicy{
		Field: "Ответственный",
		Op:    "eq",
		Value: auth.RowValue{User: "login", UserAttr: "full_name"},
	}, dealEntity())
	if err == nil {
		t.Fatal("row policy value with both user and user_attr must be rejected")
	}
}

func TestHasRestrictedPolicy(t *testing.T) {
	restricted := &auth.User{ID: "u1", Roles: []*auth.Role{docRole("Сделка", []string{"read"}, respPolicy())}}
	if !access.HasRestrictedPolicy(restricted, "document", "Сделка", "read") {
		t.Fatal("single restricted role must report restricted")
	}
	unrestricted := &auth.User{ID: "u1", Roles: []*auth.Role{docRole("Сделка", []string{"read"}, nil)}}
	if access.HasRestrictedPolicy(unrestricted, "document", "Сделка", "read") {
		t.Fatal("granted-but-unrestricted role must not report restricted")
	}
	mixed := &auth.User{ID: "u1", Roles: []*auth.Role{
		docRole("Сделка", []string{"read"}, respPolicy()),
		docRole("Сделка", []string{"read"}, nil),
	}}
	if access.HasRestrictedPolicy(mixed, "document", "Сделка", "read") {
		t.Fatal("an unrestricted role among restricted must not report restricted")
	}
	if access.HasRestrictedPolicy(&auth.User{IsAdmin: true}, "document", "Сделка", "read") {
		t.Fatal("admin must never be restricted")
	}
	if access.HasRestrictedPolicy(nil, "document", "Сделка", "read") {
		t.Fatal("open deployment must never be restricted")
	}
}

func TestQueryRowFilters_OnlyRestrictedSources(t *testing.T) {
	deal := dealEntity()
	other := &metadata.Entity{Name: "Заявка", Kind: metadata.KindDocument, Fields: []metadata.Field{{Name: "Автор", Type: metadata.FieldTypeString}}}
	u := &auth.User{
		ID: "u1",
		Roles: []*auth.Role{
			docRole("Сделка", []string{"read"}, respPolicy()), // restricted
			docRole("Заявка", []string{"read"}, nil),          // granted, unrestricted
		},
	}
	filters, err := access.QueryRowFilters(u, []*metadata.Entity{deal, other}, nil, nil, nil)
	if err != nil {
		t.Fatalf("QueryRowFilters: %v", err)
	}
	if pred := filters[query.SourceRef{Kind: "document", Name: "Сделка"}]; pred == nil || pred.Field != "Ответственный" {
		t.Fatalf("restricted source Сделка must have a predicate, got %+v", pred)
	}
	if _, ok := filters[query.SourceRef{Kind: "document", Name: "Заявка"}]; ok {
		t.Fatal("unrestricted source Заявка must not get a row filter")
	}
	// Убедимся, что admin не получает фильтров вовсе.
	if got, _ := access.QueryRowFilters(&auth.User{IsAdmin: true}, []*metadata.Entity{deal}, nil, nil, nil); got != nil {
		t.Fatalf("admin must get no row filters, got %+v", got)
	}
}
