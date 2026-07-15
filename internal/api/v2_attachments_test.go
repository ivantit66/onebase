package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// REST API v2 attachments (issue #315): Bearer-доступные эндпоинты вложений с
// той же RBAC/RLS-проверкой владельца, что и UI-путь. Тесты гоняют хендлеры
// напрямую (middleware Bearer/session — на слое роутера, см. api.New).

func multipartFile(t *testing.T, field, filename, mimeType string, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename))
	if mimeType != "" {
		hdr.Set("Content-Type", mimeType)
	}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

func attachTestHandler(t *testing.T) (*handler, *metadata.Entity) {
	t.Helper()
	cat := &metadata.Entity{
		Name: "Товар",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
		},
	}
	h, ctx := newAPITestHandler(t, []*metadata.Entity{cat}, nil)
	h.store.SetFilesDir(t.TempDir())
	if err := h.store.EnsureAttachmentTable(ctx); err != nil {
		t.Fatalf("EnsureAttachmentTable: %v", err)
	}
	return h, cat
}

func seedAttachOwner(t *testing.T, h *handler, entity *metadata.Entity) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := h.store.Upsert(context.Background(), entity.Name, id, map[string]any{"Наименование": "Владелец"}, entity); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	return id
}

// Полный цикл под пользователем с правами read+write на владельца.
func TestAPI_V2Attachments_UploadListDownloadDelete(t *testing.T) {
	h, entity := attachTestHandler(t)
	user := apiUser("editor", auth.Permission{Catalogs: map[string][]string{"Товар": {"read", "write"}}})
	ownerID := seedAttachOwner(t, h, entity)
	collParams := map[string]string{"name": "Товар", "id": ownerID.String()}

	// --- upload ---
	body, ctype := multipartFile(t, "file", "договор.pdf", "application/pdf", []byte("PDFDATA"))
	r := withUser(reqWithEntity("POST", "/api/v2/catalog/Товар/"+ownerID.String()+"/attachments", body, collParams, map[string]string{"Content-Type": ctype}), user)
	w := httptest.NewRecorder()
	h.uploadAttachmentV2(metadata.KindCatalog).ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: код %d, тело %s", w.Code, w.Body.String())
	}
	var up struct {
		Data storage.Attachment `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &up); err != nil {
		t.Fatalf("upload json: %v", err)
	}
	if up.Data.Filename != "договор.pdf" || up.Data.SizeBytes != int64(len("PDFDATA")) {
		t.Fatalf("метаданные вложения неожиданные: %+v", up.Data)
	}
	aid := up.Data.ID

	// --- list ---
	r = withUser(reqWithEntity("GET", "/api/v2/catalog/Товар/"+ownerID.String()+"/attachments", nil, collParams, nil), user)
	w = httptest.NewRecorder()
	h.listAttachmentsV2(metadata.KindCatalog).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list: код %d, тело %s", w.Code, w.Body.String())
	}
	var list struct {
		Data []storage.Attachment `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("list json: %v", err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != aid {
		t.Fatalf("ожидалось 1 вложение, получено %+v", list.Data)
	}

	// --- download ---
	aidParams := map[string]string{"aid": aid.String()}
	r = withUser(reqWithEntity("GET", "/api/v2/attachments/"+aid.String(), nil, aidParams, nil), user)
	w = httptest.NewRecorder()
	h.downloadAttachmentV2().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("download: код %d, тело %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "PDFDATA" {
		t.Fatalf("download: тело %q, ожидалось PDFDATA", w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd == "" {
		t.Fatal("download: пустой Content-Disposition")
	}

	// --- delete ---
	r = withUser(reqWithEntity("DELETE", "/api/v2/attachments/"+aid.String(), nil, aidParams, nil), user)
	w = httptest.NewRecorder()
	h.deleteAttachmentV2().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: код %d, тело %s", w.Code, w.Body.String())
	}

	// после удаления скачивание даёт 404
	r = withUser(reqWithEntity("GET", "/api/v2/attachments/"+aid.String(), nil, aidParams, nil), user)
	w = httptest.NewRecorder()
	h.downloadAttachmentV2().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("download после delete: код %d, ожидался 404", w.Code)
	}
}

// RBAC: без права write загрузка запрещена (403), без read — список запрещён.
func TestAPI_V2Attachments_RBACDenies(t *testing.T) {
	h, entity := attachTestHandler(t)
	reader := apiUser("reader", auth.Permission{Catalogs: map[string][]string{"Товар": {"read"}}})
	ownerID := seedAttachOwner(t, h, entity)
	collParams := map[string]string{"name": "Товар", "id": ownerID.String()}

	body, ctype := multipartFile(t, "file", "f.pdf", "application/pdf", []byte("X"))
	r := withUser(reqWithEntity("POST", "/api/v2/catalog/Товар/"+ownerID.String()+"/attachments", body, collParams, map[string]string{"Content-Type": ctype}), reader)
	w := httptest.NewRecorder()
	h.uploadAttachmentV2(metadata.KindCatalog).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("upload без write: код %d, ожидался 403", w.Code)
	}

	noone := apiUser("noone", auth.Permission{Catalogs: map[string][]string{"Другое": {"read"}}})
	r = withUser(reqWithEntity("GET", "/api/v2/catalog/Товар/"+ownerID.String()+"/attachments", nil, collParams, nil), noone)
	w = httptest.NewRecorder()
	h.listAttachmentsV2(metadata.KindCatalog).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("list без read: код %d, ожидался 403", w.Code)
	}
}

// allowed_types: файл с запрещённым расширением отклоняется 415, даже у
// пользователя с правом записи.
func TestAPI_V2Attachments_AllowedTypesRejected(t *testing.T) {
	h, entity := attachTestHandler(t)
	h.allowedAttachmentTypes = []string{"pdf", "png"}
	user := apiUser("editor", auth.Permission{Catalogs: map[string][]string{"Товар": {"read", "write"}}})
	ownerID := seedAttachOwner(t, h, entity)
	collParams := map[string]string{"name": "Товар", "id": ownerID.String()}

	body, ctype := multipartFile(t, "file", "notes.txt", "text/plain", []byte("hi"))
	r := withUser(reqWithEntity("POST", "/api/v2/catalog/Товар/"+ownerID.String()+"/attachments", body, collParams, map[string]string{"Content-Type": ctype}), user)
	w := httptest.NewRecorder()
	h.uploadAttachmentV2(metadata.KindCatalog).ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("upload .txt при allowed=[pdf,png]: код %d, ожидался 415 (%s)", w.Code, w.Body.String())
	}

	// разрешённое расширение проходит
	body, ctype = multipartFile(t, "file", "скан.PNG", "image/png", []byte("PNG"))
	r = withUser(reqWithEntity("POST", "/api/v2/catalog/Товар/"+ownerID.String()+"/attachments", body, collParams, map[string]string{"Content-Type": ctype}), user)
	w = httptest.NewRecorder()
	h.uploadAttachmentV2(metadata.KindCatalog).ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload .PNG при allowed=[pdf,png]: код %d, ожидался 201 (%s)", w.Code, w.Body.String())
	}
}

// Интеграция роутинга: маршруты вложений реально смонтированы в группе /api/v2
// (h.mountV2 → mountV2Attachments) и запрос доходит до хендлера через chi, а не
// упирается в 404 роутера. Ловит опечатки в путях; сама Bearer/session-мидлвара
// проверяется отдельно в auth (TestAPITokenOrSessionMiddleware).
func TestAPI_V2Attachments_RoutesMounted(t *testing.T) {
	h, entity := attachTestHandler(t)
	router := chi.NewRouter()
	h.mountV2(router)
	user := apiUser("editor", auth.Permission{Catalogs: map[string][]string{"Товар": {"read"}}})
	ownerID := seedAttachOwner(t, h, entity)

	req := httptest.NewRequest("GET", "/api/v2/catalog/Товар/"+ownerID.String()+"/attachments", nil)
	req = withUser(req, user)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("маршрут списка вложений: код %d, ожидался 200 (route не смонтирован?) — %s", w.Code, w.Body.String())
	}

	// download-маршрут по aid тоже смонтирован (несуществующий id → 404 от
	// хендлера, но не от роутера: тело — JSON-ошибка, а не chi «404 page not found»).
	req = httptest.NewRequest("GET", "/api/v2/attachments/"+uuid.New().String(), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "attachment not found") {
		t.Fatalf("маршрут скачивания: код %d, тело %q — ожидался JSON-404 хендлера", w.Code, w.Body.String())
	}
}

// IDOR: скачивание чужого вложения запрещено тому, у кого нет прав на владельца.
func TestAPI_V2Attachments_DownloadIDOR(t *testing.T) {
	h, _ := attachTestHandler(t)
	att, err := h.store.UploadAttachment(context.Background(), string(metadata.KindCatalog), "Товар", uuid.New(),
		"secret.pdf", "application/pdf", "", bytes.NewReader([]byte("SECRET")), 1<<20)
	if err != nil {
		t.Fatalf("seed UploadAttachment: %v", err)
	}
	stranger := apiUser("stranger", auth.Permission{Catalogs: map[string][]string{"Другое": {"read"}}})
	aidParams := map[string]string{"aid": att.ID.String()}

	r := withUser(reqWithEntity("GET", "/api/v2/attachments/"+att.ID.String(), nil, aidParams, nil), stranger)
	w := httptest.NewRecorder()
	h.downloadAttachmentV2().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("download чужого вложения: код %d, ожидался 403 (%s)", w.Code, w.Body.String())
	}
}

func TestAPI_V2Attachments_RejectsMissingOwner(t *testing.T) {
	h, _ := attachTestHandler(t)
	user := apiUser("editor", auth.Permission{Catalogs: map[string][]string{"Товар": {"read", "write"}}})
	ownerID := uuid.New()
	params := map[string]string{"name": "Товар", "id": ownerID.String()}
	body, ctype := multipartFile(t, "file", "f.pdf", "application/pdf", []byte("PDF"))
	r := withUser(reqWithEntity("POST", "/api/v2/catalog/Товар/"+ownerID.String()+"/attachments", body, params,
		map[string]string{"Content-Type": ctype}), user)
	w := httptest.NewRecorder()
	h.uploadAttachmentV2(metadata.KindCatalog).ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing owner: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPI_V2Attachments_WriteDoesNotImplyDownload(t *testing.T) {
	h, entity := attachTestHandler(t)
	ownerID := seedAttachOwner(t, h, entity)
	att, err := h.store.UploadAttachment(context.Background(), string(metadata.KindCatalog), entity.Name, ownerID,
		"secret.pdf", "application/pdf", "", bytes.NewReader([]byte("SECRET")), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	writer := apiUser("writer", auth.Permission{Catalogs: map[string][]string{"Товар": {"write"}}})
	params := map[string]string{"aid": att.ID.String()}
	r := withUser(reqWithEntity("GET", "/api/v2/attachments/"+att.ID.String(), nil, params, nil), writer)
	w := httptest.NewRecorder()
	h.downloadAttachmentV2().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("write-only download: status=%d body=%s", w.Code, w.Body.String())
	}
}
