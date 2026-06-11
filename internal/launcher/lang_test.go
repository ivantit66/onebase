package launcher

import (
	"net/http/httptest"
	"testing"

	"github.com/ivantit66/onebase/internal/i18n"
)

// resolveLang передавал baseLang="ru" в i18n.Resolve, и Accept-Language
// никогда не учитывался — лаунчер всегда был русским (issue #49 п.1).
func TestResolveLangHonorsAcceptLanguage(t *testing.T) {
	saved := launcherBundle
	defer func() { launcherBundle = saved }()
	b, err := i18n.Load(i18n.EmbeddedLocales, "")
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	launcherBundle = b

	cases := []struct{ accept, want string }{
		{"de-DE,de;q=0.9,en;q=0.8", "de"},
		{"en-US,en;q=0.9", "en"},
		{"", "ru"},
		{"tlh", "ru"}, // нет словаря — фолбэк
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept-Language", c.accept)
		}
		if got := resolveLang(r); got != c.want {
			t.Errorf("Accept-Language=%q: got %q, want %q", c.accept, got, c.want)
		}
	}
}
