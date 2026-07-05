package ui

import (
	"net/http/httptest"
	"testing"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

func TestBuildFlatNav_NonAdminReadsDocumentFromCommaPermission(t *testing.T) {
	doc := &metadata.Entity{Name: "ВходящееПисьмо", Kind: metadata.KindDocument}
	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Entities: []*metadata.Entity{doc}})
	s := &Server{reg: reg}

	user := &auth.User{Roles: []*auth.Role{{
		Permissions: auth.Permission{
			Documents: map[string][]string{"ВходящееПисьмо": {"read,write"}},
		},
	}}}
	req := httptest.NewRequest("GET", "/ui/", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))

	nav := s.buildFlatNav(req)
	for _, group := range nav {
		for _, item := range group.Items {
			if item.Label == "ВходящееПисьмо" && item.URL == "/ui/document/ВходящееПисьмо" {
				return
			}
		}
	}
	t.Fatalf("sidebar must include readable document for non-admin role, got %#v", nav)
}
