package ui

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func rowOwnerUser(login, entity string, ops ...string) *auth.User {
	policies := auth.RowPolicies{}
	for _, op := range ops {
		policies[op] = auth.RowPolicy{Field: "Owner", Op: "eq", Value: auth.RowValue{User: "login"}}
	}
	return &auth.User{Login: login, Roles: []*auth.Role{{
		Permissions: auth.Permission{
			Catalogs: map[string][]string{entity: ops},
			RowAccess: auth.RowAccess{Catalogs: map[string]auth.RowPolicies{
				entity: policies,
			}},
		},
	}}}
}

func ownerCatalog(name string, extra ...metadata.Field) *metadata.Entity {
	fields := []metadata.Field{{Name: "Owner", Type: metadata.FieldTypeString}}
	fields = append(fields, extra...)
	return &metadata.Entity{Name: name, Kind: metadata.KindCatalog, Fields: fields}
}

func seedOwnerRow(t *testing.T, ctx context.Context, s *Server, entity *metadata.Entity, owner string, fields map[string]any) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if fields == nil {
		fields = map[string]any{}
	}
	fields["Owner"] = owner
	if err := s.store.Upsert(ctx, entity.Name, id, fields, entity); err != nil {
		t.Fatalf("Upsert(%s): %v", entity.Name, err)
	}
	return id
}

func TestAttachmentDownload_RowAccessByOwnerRow(t *testing.T) {
	entity := ownerCatalog("Контрагенты")
	s, ctx := newSubmitTestServer(t, []*metadata.Entity{entity})
	s.store.SetFilesDir(t.TempDir())
	if err := s.store.EnsureAttachmentTable(ctx); err != nil {
		t.Fatalf("EnsureAttachmentTable: %v", err)
	}
	visibleID := seedOwnerRow(t, ctx, s, entity, "u", nil)
	hiddenID := seedOwnerRow(t, ctx, s, entity, "other", nil)
	visible, err := s.store.UploadAttachment(ctx, "catalog", entity.Name, visibleID, "visible.txt", "text/plain", "", bytes.NewReader([]byte("visible")), 1<<20)
	if err != nil {
		t.Fatalf("UploadAttachment(visible): %v", err)
	}
	hidden, err := s.store.UploadAttachment(ctx, "catalog", entity.Name, hiddenID, "hidden.txt", "text/plain", "", bytes.NewReader([]byte("hidden")), 1<<20)
	if err != nil {
		t.Fatalf("UploadAttachment(hidden): %v", err)
	}
	user := rowOwnerUser("u", entity.Name, "read", "write")

	if rec := driveAttachment(t, s.attachmentDownload, http.MethodGet, visible.ID.String(), user); rec.Code != http.StatusOK {
		t.Fatalf("visible attachment code=%d, want %d", rec.Code, http.StatusOK)
	}
	if rec := driveAttachment(t, s.attachmentDownload, http.MethodGet, hidden.ID.String(), user); rec.Code != http.StatusForbidden {
		t.Fatalf("hidden attachment code=%d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestImageServe_RowAccessByReferencingRow(t *testing.T) {
	entity := ownerCatalog("Фото", metadata.Field{Name: "Картинка", Type: metadata.FieldTypeImage})
	s, ctx := newSubmitTestServer(t, []*metadata.Entity{entity})
	s.store.SetFilesDir(t.TempDir())
	if err := s.store.EnsureBlobTable(ctx); err != nil {
		t.Fatalf("EnsureBlobTable: %v", err)
	}
	png := []byte("\x89PNG\r\n\x1a\n img")
	visible, err := s.store.PutBlob(ctx, "image/png", bytes.NewReader(png), 1<<20, storage.BlobOwner{Kind: "catalog", Entity: entity.Name})
	if err != nil {
		t.Fatalf("PutBlob(visible): %v", err)
	}
	hidden, err := s.store.PutBlob(ctx, "image/png", bytes.NewReader(png), 1<<20, storage.BlobOwner{Kind: "catalog", Entity: entity.Name})
	if err != nil {
		t.Fatalf("PutBlob(hidden): %v", err)
	}
	seedOwnerRow(t, ctx, s, entity, "u", map[string]any{"Картинка": visible.ID.String()})
	seedOwnerRow(t, ctx, s, entity, "other", map[string]any{"Картинка": hidden.ID.String()})
	user := rowOwnerUser("u", entity.Name, "read")

	if rec := serveBlob(t, s, visible.ID.String(), user); rec.Code != http.StatusOK {
		t.Fatalf("visible image code=%d, want %d", rec.Code, http.StatusOK)
	}
	if rec := serveBlob(t, s, hidden.ID.String(), user); rec.Code != http.StatusForbidden {
		t.Fatalf("hidden image code=%d, want %d", rec.Code, http.StatusForbidden)
	}
}
