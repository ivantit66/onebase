package ui

import "testing"

// allowedImageMime определяет тип по СОДЕРЖИМОМУ (а не по заголовку формы,
// который подделывается) и пропускает только настоящие растровые изображения.
// SVG и HTML обязаны отсекаться — иначе image/svg+xml со скриптом даёт stored
// XSS при прямом открытии /image/{id} (nosniff тут не спасает: тип честный).
func TestAllowedImageMime(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	gif := []byte("GIF89a\x00\x00\x00\x00")
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0, 0, 0}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	html := []byte("<!doctype html><script>alert(1)</script>")

	cases := []struct {
		name  string
		head  []byte
		allow bool
	}{
		{"png", png, true},
		{"gif", gif, true},
		{"jpeg", jpeg, true},
		{"svg", svg, false},
		{"html", html, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mime, ok := allowedImageMime(c.head)
			if ok != c.allow {
				t.Fatalf("allowedImageMime(%s) = (%q,%v), ожидалось allow=%v", c.name, mime, ok, c.allow)
			}
		})
	}
}
