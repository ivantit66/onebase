package ui

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/storage"
)

// serveBlob вызывает imageServe для заданного blob от имени user (nil = аноним /
// открытый деплой без пользователей) и возвращает записанный ответ.
func serveBlob(t *testing.T, s *Server, id string, user *auth.User) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/ui/_image/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if user != nil {
		ctx = auth.ContextWithUser(ctx, user)
	}
	rec := httptest.NewRecorder()
	s.imageServe(rec, req.WithContext(ctx))
	return rec
}

func catalogUser(entity string, ops ...string) *auth.User {
	return &auth.User{Roles: []*auth.Role{{
		Permissions: auth.Permission{Catalogs: map[string][]string{entity: ops}},
	}}}
}

// TestImageServe_AuthByOwner: отдача картинки проверяет право чтения на
// сущность-владельца (защита от IDOR). Легаси-блобы без владельца отдаются
// любому прошедшему middleware (аутентифицированному либо в открытом деплое).
func TestImageServe_AuthByOwner(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	defer db.Close()
	if err := db.EnsureBlobTable(ctx); err != nil {
		t.Fatalf("EnsureBlobTable: %v", err)
	}
	png := []byte("\x89PNG\r\n\x1a\n картинка")

	owned, err := db.PutBlob(ctx, "image/png", bytes.NewReader(png), 1<<20,
		storage.BlobOwner{Kind: "catalog", Entity: "Контрагенты"})
	if err != nil {
		t.Fatalf("PutBlob(owned): %v", err)
	}
	legacy, err := db.PutBlob(ctx, "image/png", bytes.NewReader(png), 1<<20, storage.BlobOwner{})
	if err != nil {
		t.Fatalf("PutBlob(legacy): %v", err)
	}

	s := &Server{store: db}

	cases := []struct {
		name string
		id   string
		user *auth.User
		want int
	}{
		{"владелец, есть read", owned.ID.String(), catalogUser("Контрагенты", "read"), http.StatusOK},
		{"владелец, есть только write (превью загрузки)", owned.ID.String(), catalogUser("Контрагенты", "write"), http.StatusOK},
		{"владелец, нет прав", owned.ID.String(), catalogUser("Другое", "read"), http.StatusForbidden},
		{"владелец, без пользователей (открытый деплой)", owned.ID.String(), nil, http.StatusOK},
		{"легаси без владельца, аутентифицирован без прав", legacy.ID.String(), catalogUser("Другое", "read"), http.StatusOK},
		{"легаси без владельца, открытый деплой", legacy.ID.String(), nil, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := serveBlob(t, s, c.id, c.user)
			if rec.Code != c.want {
				t.Fatalf("код %d, ожидался %d", rec.Code, c.want)
			}
		})
	}
}
