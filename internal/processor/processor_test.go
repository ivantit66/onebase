package processor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParamDisplayLabel(t *testing.T) {
	p := Param{
		Name:   "Period",
		Label:  "Период",
		Labels: map[string]string{"en": "Period"},
	}

	assert.Equal(t, "Period", p.DisplayLabel("en"))
	assert.Equal(t, "Период", p.DisplayLabel("ru"))
	assert.Equal(t, "Period", (Param{Name: "Period"}).DisplayLabel("ru"))
}

func TestProcessorDisplayName(t *testing.T) {
	p := &Processor{
		Name:   "CloseMonth",
		Title:  "Закрытие месяца",
		Titles: map[string]string{"en": "Month-end close"},
	}

	assert.Equal(t, "Month-end close", p.DisplayName("en"))
	assert.Equal(t, "Закрытие месяца", p.DisplayName("ru"))
	assert.Equal(t, "CloseMonth", (&Processor{Name: "CloseMonth"}).DisplayName("ru"))
}

func TestManagedFormReturnsFirstManagedForm(t *testing.T) {
	plain := &metadata.FormModule{Name: "Plain"}
	managed := &metadata.FormModule{Name: "Managed", LayoutKind: metadata.FormLayoutManaged}
	p := &Processor{Forms: []*metadata.FormModule{nil, plain, managed}}

	assert.Same(t, managed, p.ManagedForm())
	assert.Nil(t, (&Processor{}).ManagedForm())
}

func TestParseBytesAndLoadDir(t *testing.T) {
	raw := []byte(`
name: CloseMonth
title: Закрытие месяца
params:
  - name: Period
    type: date
    label: Период
  - name: Mode
    type: choice
    options: [soft, hard]
`)
	p, err := ParseBytes(raw)
	require.NoError(t, err)
	assert.Equal(t, "CloseMonth", p.Name)
	require.Len(t, p.Params, 2)
	assert.Equal(t, []string{"soft", "hard"}, p.Params[1].Options)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "close.yaml"), raw, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("name: Ignored"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested.yaml"), 0o755))

	items, err := LoadDir(dir)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "CloseMonth", items[0].Name)

	missing, err := LoadDir(filepath.Join(dir, "missing"))
	require.NoError(t, err)
	assert.Nil(t, missing)
}
