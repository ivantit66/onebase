package ui

// Issue #151 (опциональная фича): actions.delete.visible=false скрывает
// платформенную кнопку «Удалить» в managed-форме объекта. Это позволяет
// конфигу увести удаление в собственный процессор (например, с расширенным
// аудитом). Само платформенное удаление и так пишется в _audit и закрыто
// правом delete — здесь речь только об управлении UI-кнопкой.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/metadata"
)

func renderObjectFormGET(t *testing.T, s *Server, ent *metadata.Entity, id uuid.UUID) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/ui/catalog/"+ent.Name+"/"+id.String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "catalog")
	rctx.URLParams.Add("entity", ent.Name)
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.formEdit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получен %d", rec.Code)
	}
	return rec.Body.String()
}

func setupDeleteActionServer(t *testing.T) (*Server, *metadata.Entity, uuid.UUID) {
	t.Helper()
	srv, ent := setupManagedEventsServer(t, ``, nil, []*metadata.FormElement{
		{Kind: metadata.FormElementField, Name: "Наименование", DataPath: "Объект.Наименование"},
	})
	id := uuid.New()
	if err := srv.store.Upsert(context.Background(), ent.Name, id,
		map[string]any{"Наименование": "X"}, ent); err != nil {
		t.Fatal(err)
	}
	return srv, ent, id
}

func TestManagedForm_DeleteShownByDefault(t *testing.T) {
	srv, ent, id := setupDeleteActionServer(t)
	body := renderObjectFormGET(t, srv, ent, id)
	if !strings.Contains(body, id.String()+"/delete") {
		t.Errorf("по умолчанию кнопка удаления должна присутствовать в форме")
	}
}

func TestManagedForm_HidesDeleteWhenActionInvisible(t *testing.T) {
	srv, ent, id := setupDeleteActionServer(t)
	vis := false
	ent.Forms[0].Actions = map[string]*metadata.FormAction{"delete": {Visible: &vis}}

	body := renderObjectFormGET(t, srv, ent, id)
	if strings.Contains(body, id.String()+"/delete") {
		t.Errorf("при actions.delete.visible=false кнопка удаления должна быть скрыта")
	}
}
