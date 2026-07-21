package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func uiClientEntity() *metadata.Entity {
	return &metadata.Entity{
		Name: "Клиент",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Телефон", Type: metadata.FieldTypeString},
		},
	}
}

func uiMaskUser(ops []string, fields auth.FieldPolicies) *auth.User {
	return &auth.User{
		ID:    "op",
		Login: "operator",
		Roles: []*auth.Role{{
			Name: "Оператор",
			Permissions: auth.Permission{
				Catalogs:    map[string][]string{"Клиент": ops},
				FieldAccess: auth.FieldAccess{Catalogs: map[string]auth.FieldPolicies{"Клиент": fields}},
			},
		}},
	}
}

// Основная защита формы: пользователь, видящий поле лишь замаскированным, не
// перезаписывает реальное значение при сохранении карточки (submitEdit).
func TestUI_SubmitEdit_MaskedFieldWriteGuard(t *testing.T) {
	cat := uiClientEntity()
	s, ctx := newSubmitTestServer(t, []*metadata.Entity{cat})
	id := uuid.New()
	if err := s.store.Upsert(ctx, "Клиент", id, map[string]any{
		"Наименование": "Иванов", "Телефон": "+79161234455",
	}, cat); err != nil {
		t.Fatal(err)
	}
	user := uiMaskUser([]string{"read", "write"}, auth.FieldPolicies{"Телефон": {Read: "mask_tail", Keep: 4}})

	form := url.Values{"Наименование": {"Петров"}, "Телефон": {"7770001122"}}
	r := reqWithChi("POST", "/ui/catalog/Клиент/"+id.String(), form,
		map[string]string{"kind": "catalog", "entity": "Клиент", "id": id.String()})
	r = r.WithContext(auth.ContextWithUser(r.Context(), user))
	w := httptest.NewRecorder()
	s.submitEdit(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("submitEdit: expected 303, got %d: %s", w.Code, w.Body.String())
	}
	row, err := s.store.GetByID(ctx, "Клиент", id, cat)
	if err != nil {
		t.Fatal(err)
	}
	if row["Телефон"] != "+79161234455" {
		t.Fatalf("masked field must NOT be overwritten, got %v", row["Телефон"])
	}
	if row["Наименование"] != "Петров" {
		t.Fatalf("visible field must update, got %v", row["Наименование"])
	}
}

// Раскрытие под правом disclose: возвращает полное значение и пишет аудит без
// значения.
func TestUI_DiscloseField(t *testing.T) {
	cat := uiClientEntity()
	s, ctx := newSubmitTestServer(t, []*metadata.Entity{cat})
	if err := s.store.EnsureAuditSchema(ctx); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := s.store.Upsert(ctx, "Клиент", id, map[string]any{"Телефон": "+79161234455"}, cat); err != nil {
		t.Fatal(err)
	}
	user := uiMaskUser([]string{"read", "disclose"}, auth.FieldPolicies{"Телефон": {Read: "mask_tail", Keep: 4}})

	form := url.Values{"field": {"Телефон"}, "reason": {"звонок клиента"}}
	r := reqWithChi("POST", "/ui/catalog/Клиент/"+id.String()+"/disclose", form,
		map[string]string{"kind": "catalog", "entity": "Клиент", "id": id.String()})
	rctx := auth.ContextWithUser(r.Context(), user)
	rctx = storage.WithAuditUser(rctx, user.ID, user.Login)
	r = r.WithContext(rctx)
	w := httptest.NewRecorder()
	s.discloseField(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("disclose: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Value     string `json:"value"`
		Disclosed bool   `json:"disclosed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Value != "+79161234455" || !resp.Disclosed {
		t.Fatalf("disclose response = %+v", resp)
	}
	entries, err := s.store.AuditByRecord(ctx, "Клиент", id)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		if e.Action == "disclose" {
			found = true
			if e.Reason != "звонок клиента" || e.Field != "Телефон" {
				t.Fatalf("audit entry wrong: %+v", e)
			}
			if sv, _ := e.NewValue.(string); sv == "+79161234455" {
				t.Fatal("audit must not store disclosed value")
			}
		}
	}
	if !found {
		t.Fatal("disclose audit entry missing")
	}
}
