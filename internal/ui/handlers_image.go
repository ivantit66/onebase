package ui

// HTTP-обработчики поля типа image: загрузка картинки в blob-хранилище и
// отдача по UUID. Поле сущности хранит только ссылку (UUID); сам бинарник
// лежит на диске или в БД (см. storage blob backend, режим ui.file_storage).

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// imageUpload принимает картинку (multipart-поле "file") в контексте сущности,
// сохраняет её в blob-хранилище и возвращает JSON {"ref":"<uuid>"}. Ссылку
// форма кладёт в скрытое поле и сохраняет вместе с записью (поле типа image).
func (s *Server) imageUpload(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "write") {
		return
	}

	maxSize := s.maxFileSizeBytes
	if maxSize == 0 {
		maxSize = 50 * 1024 * 1024
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSize+1024)

	lang := s.resolveLang(r)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, s.tr(lang, "Ошибка разбора формы")+": "+s.errText(r, err), 400)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, s.tr(lang, "Нет файла в форме"), 400)
		return
	}
	defer file.Close()

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if !strings.HasPrefix(mimeType, "image/") {
		http.Error(w, s.tr(lang, "Можно загрузить только изображение"), 400)
		return
	}

	b, err := s.store.PutBlob(r.Context(), mimeType, file, maxSize)
	if err != nil {
		http.Error(w, s.errText(r, err), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"ref": b.ID.String()})
}

// imageServe отдаёт бинарник по UUID (значение поля image). Бинарник
// адресуется неизменяемым UUID, поэтому помечается долгоживущим кэшем.
func (s *Server) imageServe(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	b, rc, err := s.store.OpenBlob(r.Context(), id)
	if err != nil {
		http.Error(w, s.tr(s.resolveLang(r), "Файл не найден"), 404)
		return
	}
	defer rc.Close()

	if b.Mime != "" {
		w.Header().Set("Content-Type", b.Mime)
	}
	if b.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(b.Size, 10))
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	io.Copy(w, rc)
}
