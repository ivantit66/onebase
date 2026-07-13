package api

// REST API v2 — вложения к документам и справочникам (issue #315).
//
// Раньше вложения жили только под UI-маршрутами /ui/.../attachments в группе с
// session-only middleware, поэтому Bearer-токен интеграций к ним не проходил, и
// клиентам приходилось грузить файлы по приватной сессионной куке. Здесь те же
// операции вынесены в /api/v2 (группа APITokenOrSession → Bearer|cookie) с той
// же RBAC/RLS-проверкой владельца, что и в UI, но с JSON-ответами и кодами
// 201/204 вместо редиректов.

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// mountV2Attachments регистрирует маршруты вложений в группе /api/v2. Коллекция
// привязана к строке-владельцу ({name}/{id}), отдельный файл адресуется по aid.
func (h *handler) mountV2Attachments(r chi.Router) {
	r.Get("/catalog/{name}/{id}/attachments", h.listAttachmentsV2(metadata.KindCatalog))
	r.Post("/catalog/{name}/{id}/attachments", h.uploadAttachmentV2(metadata.KindCatalog))
	r.Get("/document/{name}/{id}/attachments", h.listAttachmentsV2(metadata.KindDocument))
	r.Post("/document/{name}/{id}/attachments", h.uploadAttachmentV2(metadata.KindDocument))
	r.Get("/attachments/{aid}", h.downloadAttachmentV2())
	r.Delete("/attachments/{aid}", h.deleteAttachmentV2())
}

// listAttachmentsV2 возвращает список вложений строки-владельца.
func (h *handler) listAttachmentsV2(kind metadata.Kind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entity, entityName, ok := h.entityFromV2Route(w, r, kind)
		if !ok {
			return
		}
		if !requireRESTPerm(w, r, kind, entityName, "read") {
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id", "", 0)
			return
		}
		if !h.rowAllowedID(r.Context(), entity, "read", id) {
			writeError(w, http.StatusForbidden, "forbidden", "", 0)
			return
		}
		atts, err := h.store.ListAttachments(r.Context(), string(entity.Kind), entity.Name, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "", 0)
			return
		}
		if atts == nil {
			atts = []storage.Attachment{}
		}
		writeJSONV2(w, http.StatusOK, restV2Envelope{Data: atts})
	}
}

// uploadAttachmentV2 принимает multipart-загрузку файла к строке-владельцу.
func (h *handler) uploadAttachmentV2(kind metadata.Kind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entity, entityName, ok := h.entityFromV2Route(w, r, kind)
		if !ok {
			return
		}
		if !requireRESTPerm(w, r, kind, entityName, "write") {
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id", "", 0)
			return
		}
		if !h.rowAllowedID(r.Context(), entity, "write", id) {
			writeError(w, http.StatusForbidden, "forbidden", "", 0)
			return
		}

		maxSize := h.maxFileSizeBytes
		if maxSize <= 0 {
			maxSize = 50 * 1024 * 1024
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxSize+1024)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "cannot parse multipart form: "+err.Error(), "", 0)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing file field", "", 0)
			return
		}
		defer file.Close()

		filename := storage.SanitizeAttachmentName(header.Filename)
		if !storage.AttachmentExtAllowed(h.allowedAttachmentTypes, filename) {
			writeError(w, http.StatusUnsupportedMediaType, "file type not allowed", "", 0)
			return
		}
		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		uploadedBy := ""
		if u := auth.UserFromContext(r.Context()); u != nil {
			uploadedBy = u.Login
		}

		att, err := h.store.UploadAttachment(r.Context(), string(entity.Kind), entity.Name, id,
			filename, mimeType, uploadedBy, file, maxSize)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "", 0)
			return
		}
		writeJSONV2(w, http.StatusCreated, restV2Envelope{Data: att})
	}
}

// downloadAttachmentV2 отдаёт бинарник вложения по его id.
func (h *handler) downloadAttachmentV2() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		aid, err := uuid.Parse(chi.URLParam(r, "aid"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id", "", 0)
			return
		}
		f, att, err := h.store.OpenAttachment(r.Context(), aid)
		if err != nil {
			writeError(w, http.StatusNotFound, "attachment not found", "", 0)
			return
		}
		defer f.Close()

		// Защита от IDOR: отдаём файл только при праве чтения (или записи — чтобы
		// предпросмотр у загрузчика работал сразу) на строку-владельца.
		if !h.rowAllowsOwnerID(r.Context(), att.OwnerKind, att.OwnerName, "read", att.OwnerID) &&
			!h.rowAllowsOwnerID(r.Context(), att.OwnerKind, att.OwnerName, "write", att.OwnerID) {
			writeError(w, http.StatusForbidden, "forbidden", "", 0)
			return
		}

		w.Header().Set("Content-Type", att.MimeType)
		w.Header().Set("Content-Disposition", attachmentContentDisposition(att.Filename))
		http.ServeContent(w, r, att.Filename, att.UploadedAt, f)
	}
}

// deleteAttachmentV2 удаляет вложение по его id.
func (h *handler) deleteAttachmentV2() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		aid, err := uuid.Parse(chi.URLParam(r, "aid"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id", "", 0)
			return
		}
		att, err := h.store.GetAttachment(r.Context(), aid)
		if err != nil {
			writeError(w, http.StatusNotFound, "attachment not found", "", 0)
			return
		}
		// Удалять вложение может только тот, у кого есть право записи на владельца.
		if !h.rowAllowsOwnerID(r.Context(), att.OwnerKind, att.OwnerName, "write", att.OwnerID) {
			writeError(w, http.StatusForbidden, "forbidden", "", 0)
			return
		}
		if err := h.store.DeleteAttachment(r.Context(), aid); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "", 0)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// attachmentOpenAPISchema описывает объект вложения для openapi.json.
func attachmentOpenAPISchema() map[string]any {
	str := map[string]any{"type": "string"}
	uuidStr := map[string]any{"type": "string", "format": "uuid"}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          uuidStr,
			"owner_kind":  str,
			"owner_name":  str,
			"owner_id":    uuidStr,
			"filename":    str,
			"mime_type":   str,
			"size_bytes":  map[string]any{"type": "integer", "format": "int64"},
			"uploaded_at": map[string]any{"type": "string", "format": "date-time"},
			"uploaded_by": str,
		},
	}
}

// attachmentContentDisposition собирает заголовок Content-Disposition по RFC 6266:
// ASCII-фолбэк в filename= для старых клиентов + полное UTF-8-имя в filename*=
// (иначе не-ASCII имена браузеры декодируют как latin-1 → кракозябры). Компактный
// аналог ui.contentDisposition для REST-пути отдачи вложений. Имя к этому моменту
// уже нормализовано storage.SanitizeAttachmentName (без путей и управляющих).
func attachmentContentDisposition(filename string) string {
	const attr = "!#$&+-.^_`|~"
	var ascii, enc strings.Builder
	for _, r := range filename {
		if r >= 0x20 && r < 0x80 && r != '"' && r != '\\' {
			ascii.WriteRune(r)
		} else {
			ascii.WriteByte('_')
		}
	}
	for _, c := range []byte(filename) { // побайтово: октеты UTF-8 percent-кодируются
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			strings.IndexByte(attr, c) >= 0 {
			enc.WriteByte(c)
		} else {
			fmt.Fprintf(&enc, "%%%02X", c)
		}
	}
	name := ascii.String()
	if name == "" {
		name = "file"
	}
	return "attachment; filename=\"" + name + "\"; filename*=UTF-8''" + enc.String()
}
