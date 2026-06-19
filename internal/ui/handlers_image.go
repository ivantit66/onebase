package ui

// HTTP-обработчики поля типа image: загрузка картинки в blob-хранилище и
// отдача по UUID. Поле сущности хранит только ссылку (UUID); сам бинарник
// лежит на диске или в БД (см. storage blob backend, режим ui.file_storage).

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/storage"
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
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, s.tr(lang, "Нет файла в форме"), 400)
		return
	}
	defer file.Close()

	// Тип определяем по СОДЕРЖИМОМУ файла, а не по Content-Type формы (он
	// подделывается): читаем первые 512 байт для http.DetectContentType и
	// «возвращаем» их в поток через MultiReader. Это отсекает обычный SVG/HTML
	// (он распознаётся как text/*), но НЕ является единственным барьером:
	// GIF-полиглот (правильная сигнатура GIF89a + произвольный «хвост») всё ещё
	// классифицируется как image/gif. Фактическую защиту от XSS даёт сторона
	// отдачи imageServe — nosniff + честный Content-Type + sandbox-CSP, поэтому
	// эти заголовки трогать нельзя.
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	head = head[:n]
	mimeType, ok := allowedImageMime(head)
	if !ok {
		http.Error(w, s.tr(lang, "Можно загрузить только изображение"), 400)
		return
	}
	body := io.MultiReader(bytes.NewReader(head), file)

	// Владелец бинарника = сущность, в контексте которой идёт загрузка. imageServe
	// по нему проверяет право чтения при отдаче (защита от IDOR).
	owner := storage.BlobOwner{Kind: string(entity.Kind), Entity: entity.Name}
	b, err := s.store.PutBlob(r.Context(), mimeType, body, maxSize, owner)
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

	// Авторизация (защита от IDOR): если у блоба есть владелец-сущность, отдаём
	// только тем, у кого есть право чтения (или записи — чтобы превью сразу после
	// загрузки работало у загрузчика). can() возвращает true для nil-пользователя,
	// поэтому в открытом деплое без пользователей доступ остаётся свободным.
	// Легаси-блобы без владельца уже защищены auth-middleware (аноним до сюда
	// не доходит, если пользователи заведены) — отдельная проверка не нужна.
	if b.OwnerEntity != "" {
		if !s.can(r, b.OwnerKind, b.OwnerEntity, "read") && !s.can(r, b.OwnerKind, b.OwnerEntity, "write") {
			http.Error(w, s.tr(s.resolveLang(r), "Нет доступа"), http.StatusForbidden)
			return
		}
	}

	// Content-Type отдаём как есть только для растровых типов; всё прочее
	// (например text/html, сохранённый через СохранитьКартинку с произвольным
	// mime) выдаём как application/octet-stream — вместе с nosniff и sandbox это
	// исключает интерпретацию как HTML при прямом открытии /image/{id}.
	if strings.HasPrefix(b.Mime, "image/") {
		w.Header().Set("Content-Type", b.Mime)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if b.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(b.Size, 10))
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	// Бинарник отдаётся inline и в режиме sandbox: даже если в хранилище есть
	// SVG со скриптом (загруженный до ужесточения проверки типа), при прямом
	// открытии /image/{id} он будет инертен. На отрисовку через <img> не влияет.
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	io.Copy(w, rc)
}

// allowedImageMime определяет тип картинки по её первым байтам (server-side, без
// доверия заголовку формы) — первая линия фильтра загрузки. Отсекает обычный SVG
// (распознаётся как text/xml) и HTML, но это НЕ гарантия безопасности: контент с
// валидной растровой сигнатурой и произвольным «хвостом» (GIF-полиглот) пройдёт.
// Защиту от XSS обеспечивает отдача imageServe (nosniff + sandbox-CSP), а не этот
// фильтр — см. TestImageServe_SecurityHeaders.
func allowedImageMime(head []byte) (string, bool) {
	mime := http.DetectContentType(head)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	return mime, strings.HasPrefix(mime, "image/")
}
