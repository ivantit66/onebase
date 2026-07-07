package ui

// Рендер экрана активных сессий (план 78): несколько сессий одного логина
// отдельными строками, вид сессии, per-session kick по public_id и kick all.

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/auth"
)

func TestAdminSessionsTemplate_MultiSession(t *testing.T) {
	now := time.Now()
	sessions := []*auth.SessionInfo{
		{PublicID: "pid-ent", Kind: auth.SessionKindEnterprise, Login: "ivan", FullName: "Иван",
			CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), IP: "127.0.0.1:5555",
			UserAgent: "Mozilla/5.0 (Windows NT 10.0) Chrome/126.0 Safari/537.36"},
		{PublicID: "pid-cfg", Kind: auth.SessionKindConfigurator, Login: "ivan", FullName: "Иван",
			CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), IP: "127.0.0.1:6666",
			UserAgent: "Mozilla/5.0 Edg/126.0"},
		// Сессия, созданная до миграции плана 78: без public_id и меты.
		{Login: "legacy", FullName: "Старый", ExpiresAt: now.Add(time.Hour)},
	}

	var buf bytes.Buffer
	err := adminTmpl.ExecuteTemplate(&buf, "admin-sessions", map[string]any{"Sessions": sessionVMs(sessions), "Limit": 2})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	html := buf.String()

	// Форма политики лимита сессий (п. 1.6) с текущим значением.
	for _, want := range []string{`action="/ui/admin/sessions/limit"`, `name="limit" value="2"`} {
		if !strings.Contains(html, want) {
			t.Errorf("в HTML нет формы лимита: %q", want)
		}
	}

	// Обе строки одного логина и оба вида сессий.
	if strings.Count(html, "<strong>ivan</strong>") != 2 {
		t.Errorf("ожидались 2 строки логина ivan")
	}
	for _, want := range []string{"Предприятие", "Конфигуратор", "Chrome", "Edge", "127.0.0.1:5555"} {
		if !strings.Contains(html, want) {
			t.Errorf("в HTML нет %q", want)
		}
	}

	// Per-session kick по public_id (токенов в HTML нет по построению —
	// SessionInfo их не содержит).
	for _, want := range []string{`name="public_id" value="pid-ent"`, `name="public_id" value="pid-cfg"`, `action="/ui/admin/sessions/kick"`} {
		if !strings.Contains(html, want) {
			t.Errorf("в HTML нет %q", want)
		}
	}
	// Kick all по логину остаётся.
	if !strings.Contains(html, `action="/ui/admin/sessions/ivan/kick"`) {
		t.Error("нет действия «завершить все сессии пользователя»")
	}
	// У legacy-сессии нет public_id → нет per-session формы, но есть kick all.
	if strings.Contains(html, `name="public_id" value=""`) {
		t.Error("для сессии без public_id не должно быть per-session kick")
	}
	if !strings.Contains(html, `action="/ui/admin/sessions/legacy/kick"`) {
		t.Error("для legacy-сессии должен остаться kick all")
	}
}

func TestShortUserAgent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"Mozilla/5.0 (Windows NT 10.0) Chrome/126.0 Safari/537.36 Edg/126.0", "Edge"},
		{"Mozilla/5.0 (Windows NT 10.0) Chrome/126.0 Safari/537.36", "Chrome"},
		{"Mozilla/5.0 (X11; Linux) Firefox/127.0", "Firefox"},
		{"Mozilla/5.0 (Macintosh) Version/17.4 Safari/605.1.15", "Safari"},
		{"нестандартный агент", "нестандартный агент"},
	}
	for _, c := range cases {
		if got := shortUserAgent(c.in); got != c.want {
			t.Errorf("shortUserAgent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := strings.Repeat("яю", 40)
	if got := shortUserAgent(long); len([]rune(got)) != 41 {
		t.Errorf("длинный агент должен обрезаться до 40 рун + …, получено %d рун", len([]rune(got)))
	}
}
