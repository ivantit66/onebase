package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

var jsonMarshal = json.Marshal

// Permission holds allowed operations per entity kind.
type Permission struct {
	Catalogs  map[string][]string `yaml:"catalogs"`
	Documents map[string][]string `yaml:"documents"`
	Registers map[string][]string `yaml:"registers"`
	InfoRegs  map[string][]string `yaml:"inforegs"`
	Reports   map[string][]string `yaml:"reports"`
}

// Role is a named set of permissions.
type Role struct {
	ID          string
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Permissions Permission  `yaml:"permissions"`
}

// Has reports whether the user has permission for (kind, entity, op).
// kind: "catalog"|"document"|"register"|"inforeg"|"report"
// op:   "read"|"write"|"delete"|"post"|"unpost"|"run"
func (u *User) Has(kind, entity, op string) bool {
	if u.IsAdmin {
		return true
	}
	for _, r := range u.Roles {
		var m map[string][]string
		switch kind {
		case "catalog":
			m = r.Permissions.Catalogs
		case "document":
			m = r.Permissions.Documents
		case "register":
			m = r.Permissions.Registers
		case "inforeg":
			m = r.Permissions.InfoRegs
		case "report":
			m = r.Permissions.Reports
		}
		for _, allowed := range m[entity] {
			if allowed == op {
				return true
			}
		}
	}
	return false
}

// EnsureRolesSchema creates the _roles and _user_roles tables if they don't exist.
func (r *Repo) EnsureRolesSchema(ctx context.Context) error {
	d := r.db.Dialect()
	rolesDDL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS _roles (
			id %s PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			permissions %s NOT NULL DEFAULT '{}',
			updated_at %s DEFAULT %s
		)`, d.TypeUUID(), d.TypeJSON(), d.TypeTimestamp(), d.CurrentTimestampTZ())
	if _, err := r.db.Exec(ctx, rolesDDL); err != nil {
		return fmt.Errorf("auth: create _roles: %w", err)
	}
	userRolesDDL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS _user_roles (
			user_id %s REFERENCES _users(id) ON DELETE CASCADE,
			role_id %s REFERENCES _roles(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, role_id)
		)`, d.TypeUUID(), d.TypeUUID())
	if _, err := r.db.Exec(ctx, userRolesDDL); err != nil {
		return fmt.Errorf("auth: create _user_roles: %w", err)
	}
	return nil
}

// SyncRoles upserts YAML roles into _roles table.
func (r *Repo) SyncRoles(ctx context.Context, roles []*Role) error {
	d := r.db.Dialect()
	jc := d.JSONCast()
	for _, role := range roles {
		permJSON, err := marshalPermissions(role.Permissions)
		if err != nil {
			return err
		}
		q := fmt.Sprintf(
			`INSERT INTO _roles (id, name, description, permissions, updated_at)
			 VALUES (%s, %s, %s, %s%s, %s)
			 ON CONFLICT (name) DO UPDATE SET description=EXCLUDED.description, permissions=EXCLUDED.permissions, updated_at=%s`,
			d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4), jc, d.Now(), d.Now())
		newID := uuid.New().String()
		if _, err := r.db.Exec(ctx, q, newID, role.Name, role.Description, permJSON); err != nil {
			return fmt.Errorf("auth: sync role %s: %w", role.Name, err)
		}
		var id string
		qSel := fmt.Sprintf(`SELECT id FROM _roles WHERE name=%s`, d.Placeholder(1))
		if err := r.db.QueryRow(ctx, qSel, role.Name).Scan(&id); err != nil {
			return fmt.Errorf("auth: read role id %s: %w", role.Name, err)
		}
		role.ID = id
	}
	return nil
}

// ListRoles returns all roles from the database.
func (r *Repo) ListRoles(ctx context.Context) ([]*Role, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, name, description, permissions FROM _roles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []*Role
	for rows.Next() {
		role := &Role{}
		var permJSON []byte
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &permJSON); err != nil {
			return nil, err
		}
		role.Permissions = unmarshalPermissions(permJSON)
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// GetRolesForUser loads all roles assigned to a user.
func (r *Repo) GetRolesForUser(ctx context.Context, userID string) ([]*Role, error) {
	d := r.db.Dialect()
	q := fmt.Sprintf(`
		SELECT rl.id, rl.name, rl.description, rl.permissions
		FROM _roles rl
		JOIN _user_roles ur ON ur.role_id = rl.id
		WHERE ur.user_id = %s
		ORDER BY rl.name`, d.Placeholder(1))
	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []*Role
	for rows.Next() {
		role := &Role{}
		var permJSON []byte
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &permJSON); err != nil {
			return nil, err
		}
		role.Permissions = unmarshalPermissions(permJSON)
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// GetUserRoleIDs returns the set of role IDs assigned to a user.
func (r *Repo) GetUserRoleIDs(ctx context.Context, userID string) (map[string]bool, error) {
	d := r.db.Dialect()
	q := fmt.Sprintf(`SELECT role_id FROM _user_roles WHERE user_id = %s`, d.Placeholder(1))
	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

// AssignRole assigns a role to a user (idempotent).
func (r *Repo) AssignRole(ctx context.Context, userID, roleID string) error {
	d := r.db.Dialect()
	q := fmt.Sprintf(
		`INSERT INTO _user_roles (user_id, role_id) VALUES (%s, %s) ON CONFLICT DO NOTHING`,
		d.Placeholder(1), d.Placeholder(2))
	_, err := r.db.Exec(ctx, q, userID, roleID)
	return err
}

// UnassignRole removes a role from a user.
func (r *Repo) UnassignRole(ctx context.Context, userID, roleID string) error {
	d := r.db.Dialect()
	q := fmt.Sprintf(`DELETE FROM _user_roles WHERE user_id = %s AND role_id = %s`,
		d.Placeholder(1), d.Placeholder(2))
	_, err := r.db.Exec(ctx, q, userID, roleID)
	return err
}

// DeleteRoleByName removes a role and (via ON DELETE CASCADE) its assignments.
func (r *Repo) DeleteRoleByName(ctx context.Context, name string) error {
	d := r.db.Dialect()
	q := fmt.Sprintf(`DELETE FROM _roles WHERE name = %s`, d.Placeholder(1))
	_, err := r.db.Exec(ctx, q, name)
	return err
}

// LoadRolesYAML reads all *.yaml files from dir and returns Role slices.
func LoadRolesYAML(dir string) ([]*Role, error) {
	items, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: readdir roles %s: %w", dir, err)
	}
	var roles []*Role
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, item.Name()))
		if err != nil {
			return nil, err
		}
		var role Role
		if err := yaml.Unmarshal(data, &role); err != nil {
			return nil, fmt.Errorf("auth: parse role %s: %w", item.Name(), err)
		}
		roles = append(roles, &role)
	}
	return roles, nil
}

// marshalPermissions converts Permission to JSON string.
func marshalPermissions(p Permission) (string, error) {
	type permJSON struct {
		Catalogs  map[string][]string `json:"catalogs,omitempty"`
		Documents map[string][]string `json:"documents,omitempty"`
		Registers map[string][]string `json:"registers,omitempty"`
		InfoRegs  map[string][]string `json:"inforegs,omitempty"`
		Reports   map[string][]string `json:"reports,omitempty"`
	}
	b, err := jsonMarshal(permJSON{
		Catalogs:  p.Catalogs,
		Documents: p.Documents,
		Registers: p.Registers,
		InfoRegs:  p.InfoRegs,
		Reports:   p.Reports,
	})
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

// unmarshalPermissions parses JSONB permissions from the database.
func unmarshalPermissions(data []byte) Permission {
	if len(data) == 0 {
		return Permission{}
	}
	var raw struct {
		Catalogs  map[string][]string `json:"catalogs"`
		Documents map[string][]string `json:"documents"`
		Registers map[string][]string `json:"registers"`
		InfoRegs  map[string][]string `json:"inforegs"`
		Reports   map[string][]string `json:"reports"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Permission{}
	}
	return Permission{
		Catalogs:  raw.Catalogs,
		Documents: raw.Documents,
		Registers: raw.Registers,
		InfoRegs:  raw.InfoRegs,
		Reports:   raw.Reports,
	}
}
