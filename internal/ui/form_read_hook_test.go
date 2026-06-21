package ui

// Issue #148: серверный обработчик формы ПриЧтенииНаСервере должен вызываться
// платформой на GET формы объекта ДО рендера HTML. Если обработчик бросает
// исключение (ВызватьИсключение) — платформа обязана отдать 403 и НЕ раскрывать
// данные записи. Это даёт конфигурациям RLS на чтение (row-level security),
// которого раньше не было: ПриОткрытии исполнялся только на клиенте, после
// того как сервер уже отдал форму со всеми полями.

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

func executeFormEditGET(t *testing.T, s *Server, ent *metadata.Entity, id uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/ui/catalog/"+ent.Name+"/"+id.String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "catalog")
	rctx.URLParams.Add("entity", ent.Name)
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.formEdit(rec, req)
	return rec
}

func insertContragent(t *testing.T, s *Server, ent *metadata.Entity, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := s.store.Upsert(context.Background(), ent.Name, id,
		map[string]any{"Наименование": name}, ent); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestFormEdit_OnReadAtServerDeniesRead(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ПроверитьДоступ()
	ВызватьИсключение("Нет доступа к чужому документу");
КонецПроцедуры
`, map[metadata.FormEventType]string{
		metadata.FormEventType("ПриЧтенииНаСервере"): "ПроверитьДоступ",
	}, []*metadata.FormElement{
		{Kind: metadata.FormElementField, Name: "Наименование", DataPath: "Объект.Наименование"},
	})

	id := insertContragent(t, srv, ent, "СЕКРЕТНЫЙ-КОНТРАГЕНТ")
	rec := executeFormEditGET(t, srv, ent, id)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("ожидался 403 (ПриЧтенииНаСервере бросил исключение), получен %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "СЕКРЕТНЫЙ-КОНТРАГЕНТ") {
		t.Errorf("при отказе ПриЧтенииНаСервере форма не должна раскрывать данные записи")
	}
}

func TestFormEdit_OnReadAtServerAllowsRead(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ПроверитьДоступ()
КонецПроцедуры
`, map[metadata.FormEventType]string{
		metadata.FormEventType("ПриЧтенииНаСервере"): "ПроверитьДоступ",
	}, []*metadata.FormElement{
		{Kind: metadata.FormElementField, Name: "Наименование", DataPath: "Объект.Наименование"},
	})

	id := insertContragent(t, srv, ent, "ОБЫЧНЫЙ-КОНТРАГЕНТ")
	rec := executeFormEditGET(t, srv, ent, id)

	if rec.Code != http.StatusOK {
		body := rec.Body.String()
		if len(body) > 300 {
			body = body[:300]
		}
		t.Fatalf("ожидался 200, получен %d; body=%s", rec.Code, body)
	}
	if !strings.Contains(rec.Body.String(), "ОБЫЧНЫЙ-КОНТРАГЕНТ") {
		t.Errorf("форма должна показывать данные, когда ПриЧтенииНаСервере разрешает чтение")
	}
}
