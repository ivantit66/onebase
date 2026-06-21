package ui

// Issue #152: управляемая (managed) форма объекта должна показывать UI загрузки
// файлов-вложений — как это уже делает авто-генерируемая форма. Бэкенд вложений
// (handlers_attachments.go, таблица _attachments, роуты POST .../attachments)
// существует и работает; не хватало только UI в managed-форме, поэтому
// табличная часть «Файлы» рендерилась обычным гридом без <input type=file>.

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

func TestManagedForm_RendersAttachmentsUpload(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, ``, nil, []*metadata.FormElement{
		{Kind: metadata.FormElementField, Name: "Наименование", DataPath: "Объект.Наименование"},
	})
	id := uuid.New()
	if err := srv.store.Upsert(context.Background(), ent.Name, id,
		map[string]any{"Наименование": "Контрагент-1"}, ent); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/ui/catalog/"+ent.Name+"/"+id.String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "catalog")
	rctx.URLParams.Add("entity", ent.Name)
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	srv.formEdit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получен %d", rec.Code)
	}
	body := rec.Body.String()
	// id и /attachments — ASCII, не percent-кодируются html/template.
	if !strings.Contains(body, id.String()+"/attachments") {
		t.Errorf("в managed-форме нет формы загрузки вложений (action .../attachments)")
	}
	if !strings.Contains(body, `name="file"`) {
		t.Errorf("в managed-форме нет <input type=file name=file> для вложений")
	}
}
