package richtext

import (
	"strings"
	"testing"
)

// 1x1 прозрачный PNG в base64 — валидная data-URI картинка для теста.
const pngDataURI = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func TestSanitize_StripsScript(t *testing.T) {
	got := Sanitize(`<p>hi</p><script>alert(1)</script>`)
	if strings.Contains(strings.ToLower(got), "script") {
		t.Errorf("script not stripped: %q", got)
	}
	if !strings.Contains(got, "<p>hi</p>") {
		t.Errorf("paragraph lost: %q", got)
	}
}

func TestSanitize_StripsImgOnerrorAndExternalSrc(t *testing.T) {
	got := Sanitize(`<img src="https://evil.example/x.png" onerror="alert(1)">`)
	low := strings.ToLower(got)
	if strings.Contains(low, "onerror") {
		t.Errorf("onerror not stripped: %q", got)
	}
	if strings.Contains(low, "evil.example") {
		t.Errorf("external img src not stripped: %q", got)
	}
}

func TestSanitize_StripsJavascriptHref(t *testing.T) {
	got := Sanitize(`<a href="javascript:alert(1)">click</a>`)
	if strings.Contains(strings.ToLower(got), "javascript:") {
		t.Errorf("javascript: href not neutralized: %q", got)
	}
	// текст ссылки должен остаться
	if !strings.Contains(got, "click") {
		t.Errorf("link text lost: %q", got)
	}
}

func TestSanitize_KeepsSafeHref(t *testing.T) {
	got := Sanitize(`<a href="https://example.com">x</a>`)
	if !strings.Contains(got, `href="https://example.com"`) {
		t.Errorf("safe href lost: %q", got)
	}
}

func TestSanitize_KeepsDataURIImage(t *testing.T) {
	got := Sanitize(`<img src="` + pngDataURI + `" alt="pic">`)
	if !strings.Contains(got, "data:image/png;base64,") {
		t.Errorf("data-URI image not kept: %q", got)
	}
	if !strings.Contains(got, `alt="pic"`) {
		t.Errorf("alt lost: %q", got)
	}
}

func TestSanitize_RejectsDataURIScript(t *testing.T) {
	// data:text/html — не картинка, должна быть вырезана
	got := Sanitize(`<img src="data:text/html;base64,PHNjcmlwdD4=">`)
	if strings.Contains(strings.ToLower(got), "data:text/html") {
		t.Errorf("non-image data-URI not stripped: %q", got)
	}
}

func TestSanitize_RejectsSVGDataURI(t *testing.T) {
	// SVG data-URI исполняем (может нести <script>) — должен быть вырезан,
	// несмотря на то что встроенный bluemonday AllowDataURIImages его пропустил бы.
	in := `<img src="data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPjxzY3JpcHQ+YWxlcnQoMSk8L3NjcmlwdD48L3N2Zz4=">`
	got := Sanitize(in)
	if strings.Contains(got, "svg") || strings.Contains(got, "src=") {
		t.Fatalf("svg data-URI должен быть вырезан, получено: %q", got)
	}
}

func TestSanitize_KeepsFormatting(t *testing.T) {
	in := `<p>a</p><b>b</b><strong>c</strong><i>d</i><em>e</em><u>f</u><ul><li>g</li></ul><h1>h</h1><blockquote>q</blockquote>`
	got := Sanitize(in)
	for _, tag := range []string{"<p>", "<b>", "<strong>", "<i>", "<em>", "<u>", "<ul>", "<li>", "<h1>", "<blockquote>"} {
		if !strings.Contains(got, tag) {
			t.Errorf("formatting tag %s lost: %q", tag, got)
		}
	}
}

func TestSanitize_StripsStyleAttr(t *testing.T) {
	got := Sanitize(`<div style="position:absolute;background:url(javascript:alert(1))">x</div>`)
	if strings.Contains(strings.ToLower(got), "style") {
		t.Errorf("style attribute not stripped: %q", got)
	}
	if !strings.Contains(got, "<div>x</div>") {
		t.Errorf("div content lost: %q", got)
	}
}

func TestSanitize_StripsOnclickGlobal(t *testing.T) {
	got := Sanitize(`<p onclick="alert(1)">hi</p>`)
	if strings.Contains(strings.ToLower(got), "onclick") {
		t.Errorf("onclick not stripped: %q", got)
	}
}

func TestPlaintext_StripsTags(t *testing.T) {
	got := Plaintext(`<p>Hello</p><b>world</b><img src="` + pngDataURI + `">`)
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("tags remain: %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "world") {
		t.Errorf("text lost: %q", got)
	}
	if strings.Contains(got, "base64") {
		t.Errorf("img data leaked into plaintext: %q", got)
	}
}

func TestPlaintext_CollapsesWhitespace(t *testing.T) {
	got := Plaintext("<p>a</p>\n\n   <p>b</p>")
	if got != "a b" {
		t.Errorf("whitespace not collapsed: %q", got)
	}
}

func TestPlaintext_DropsScriptText(t *testing.T) {
	got := Plaintext(`<p>visible</p><script>secret()</script>`)
	if strings.Contains(got, "secret") {
		t.Errorf("script text leaked: %q", got)
	}
}

func TestMaxBytes(t *testing.T) {
	if MaxBytes != 4*1024*1024 {
		t.Errorf("MaxBytes = %d, want 4MiB", MaxBytes)
	}
}
