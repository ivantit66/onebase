package ui

import (
	"os"
	"strings"
	"testing"
)

func TestRefPickerDoesNotInjectOptionLabelAsHTML(t *testing.T) {
	raw, err := os.ReadFile("templates.go")
	if err != nil {
		t.Fatalf("read templates.go: %v", err)
	}
	src := string(raw)

	for _, bad := range []string{
		`+ opts[i].label +`,
		`+opts[i].label+`,
		`+ opts[j].label +`,
		`+opts[j].label+`,
	} {
		if strings.Contains(src, bad) {
			t.Fatalf("ref picker must not concatenate option label into innerHTML: found %q", bad)
		}
	}
	for _, want := range []string{
		`item.textContent = opts[i].label`,
		`item.textContent=opts[j].label`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("ref picker should write option label through textContent: missing %q", want)
		}
	}
}
