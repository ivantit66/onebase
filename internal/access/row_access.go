package access

import (
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

type Decision struct {
	Allowed      bool
	Unrestricted bool
	Predicate    *storage.Predicate
}

func Decide(u *auth.User, kind, entity, op string, meta *metadata.Entity) (Decision, error) {
	if u == nil || u.IsAdmin {
		return Decision{Allowed: true, Unrestricted: true}, nil
	}
	var predicates []storage.Predicate
	granted := false
	for _, role := range u.Roles {
		if role == nil || !auth.PermissionHas(role.Permissions, kind, entity, op) {
			continue
		}
		granted = true
		policy, ok := role.Permissions.RowAccess.Policy(kind, entity, op)
		if !ok {
			return Decision{Allowed: true, Unrestricted: true}, nil
		}
		pred, err := compilePolicy(policy, u, meta)
		if err != nil {
			return Decision{}, err
		}
		predicates = append(predicates, pred)
	}
	if !granted {
		return Decision{}, nil
	}
	if len(predicates) == 0 {
		return Decision{Allowed: true, Unrestricted: true}, nil
	}
	if len(predicates) == 1 {
		return Decision{Allowed: true, Predicate: &predicates[0]}, nil
	}
	return Decision{Allowed: true, Predicate: &storage.Predicate{Any: predicates}}, nil
}

func HasRestrictedPolicy(u *auth.User, kind, entity, op string) bool {
	if u == nil || u.IsAdmin {
		return false
	}
	restricted := false
	for _, role := range u.Roles {
		if role == nil || !auth.PermissionHas(role.Permissions, kind, entity, op) {
			continue
		}
		if _, ok := role.Permissions.RowAccess.Policy(kind, entity, op); !ok {
			return false
		}
		restricted = true
	}
	return restricted
}

// ValidatePolicy checks a row policy with the same compiler path used at
// runtime. It is intended for diagnostics/lint: callers pass already resolved
// same_as policies.
func ValidatePolicy(p auth.RowPolicy, meta *metadata.Entity) error {
	pred, err := compilePolicy(p, lintUser(), meta)
	if err != nil {
		return err
	}
	_, _, _, err = storage.PredicateSQL(storage.SQLiteDialect{}, meta, &pred, 1)
	return err
}

func lintUser() *auth.User {
	return &auth.User{
		ID:               "_lint_user_id",
		Login:            "_lint_user_login",
		FullName:         "_lint_user_full_name",
		Lang:             "_lint_user_lang",
		IsAdmin:          false,
		DenyPasswdChange: false,
		ShowInList:       false,
		AIDataAccess:     false,
	}
}

func compilePolicy(p auth.RowPolicy, u *auth.User, meta *metadata.Entity) (storage.Predicate, error) {
	if len(p.All) > 0 {
		out := storage.Predicate{All: make([]storage.Predicate, 0, len(p.All))}
		for _, item := range p.All {
			compiled, err := compilePolicy(item, u, meta)
			if err != nil {
				return storage.Predicate{}, err
			}
			out.All = append(out.All, compiled)
		}
		return out, nil
	}
	if len(p.Any) > 0 {
		out := storage.Predicate{Any: make([]storage.Predicate, 0, len(p.Any))}
		for _, item := range p.Any {
			compiled, err := compilePolicy(item, u, meta)
			if err != nil {
				return storage.Predicate{}, err
			}
			out.Any = append(out.Any, compiled)
		}
		return out, nil
	}
	if p.Not != nil {
		compiled, err := compilePolicy(*p.Not, u, meta)
		if err != nil {
			return storage.Predicate{}, err
		}
		return storage.Predicate{Not: &compiled}, nil
	}
	field := strings.TrimSpace(p.Field)
	if field == "" {
		return storage.Predicate{}, fmt.Errorf("row policy has empty field")
	}
	if !fieldAllowed(meta, field) {
		return storage.Predicate{}, fmt.Errorf("row policy references unknown field %q", field)
	}
	if (p.Op == "in" || p.Op == "not_in") && len(p.Value.List) == 0 {
		if _, ok := p.Value.Literal.([]any); !ok {
			return storage.Predicate{}, fmt.Errorf("row policy op %q requires list value", p.Op)
		}
	}
	value, values, err := resolveValue(p.Value, u)
	if err != nil {
		return storage.Predicate{}, err
	}
	return storage.Predicate{
		Field:  field,
		Op:     p.Op,
		Value:  value,
		Values: values,
	}, nil
}

func resolveValue(v auth.RowValue, u *auth.User) (any, []any, error) {
	if strings.TrimSpace(v.User) != "" && strings.TrimSpace(v.UserAttr) != "" {
		return nil, nil, fmt.Errorf("row policy value cannot use both user and user_attr")
	}
	switch v.User {
	case "":
	case "id":
		return u.ID, nil, nil
	case "login":
		return u.Login, nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown row policy user value %q", v.User)
	}
	if strings.TrimSpace(v.UserAttr) != "" {
		value, ok := resolveUserAttr(u, v.UserAttr)
		if !ok {
			return nil, nil, fmt.Errorf("unknown row policy user_attr %q", v.UserAttr)
		}
		return value, nil, nil
	}
	if len(v.List) > 0 {
		return nil, v.List, nil
	}
	return v.Literal, nil, nil
}

func resolveUserAttr(u *auth.User, attr string) (any, bool) {
	if u == nil {
		return nil, false
	}
	key := strings.ToLower(strings.TrimSpace(attr))
	switch key {
	case "id", "user_id":
		return u.ID, true
	case "login":
		return u.Login, true
	case "full_name", "fullname":
		return u.FullName, true
	case "lang", "language":
		return u.Lang, true
	case "is_admin", "admin":
		return u.IsAdmin, true
	case "deny_passwd_change":
		return u.DenyPasswdChange, true
	case "show_in_list":
		return u.ShowInList, true
	case "ai_data_access":
		return u.AIDataAccess, true
	}
	for name, value := range u.Attrs {
		if strings.EqualFold(strings.TrimSpace(name), key) {
			return value, true
		}
	}
	return nil, false
}

func fieldAllowed(entity *metadata.Entity, field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "id", "ссылка", "deletion_mark", "пометкаудаления", "пометка_удаления", "_version":
		return true
	case "period", "период":
		return entityHasField(entity, "period")
	case "recorder", "регистратор":
		return entityHasField(entity, "recorder") || entityHasField(entity, "регистратор")
	case "recorder_type", "типрегистратора", "тип_регистратора":
		return entityHasField(entity, "recorder_type") || entityHasField(entity, "регистратор_тип")
	case "line_number", "номерстроки", "номер_строки":
		return entityHasField(entity, "line_number")
	case "вид_движения", "виддвижения":
		return entityHasField(entity, "вид_движения")
	case "posted", "проведен", "проведён":
		return entity != nil && entity.Kind == metadata.KindDocument
	case "parent_id", "is_folder":
		return entity != nil && entity.Hierarchical
	}
	if entity == nil {
		return false
	}
	for _, f := range entity.Fields {
		if strings.EqualFold(f.Name, field) {
			return true
		}
	}
	return false
}

func entityHasField(entity *metadata.Entity, field string) bool {
	if entity == nil {
		return false
	}
	for i := range entity.Fields {
		if strings.EqualFold(entity.Fields[i].Name, field) {
			return true
		}
	}
	return false
}
