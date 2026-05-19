package runtime

import (
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

// Замечание #17: *Object должен реализовывать тот же контракт, что и *Ref —
// GetRefUUID() и String(). Это позволяет storage.normalizeRegArg
// одинаково обрабатывать оба типа при записи движений регистра.
func TestObject_GetRefUUID(t *testing.T) {
	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	o := &Object{Type: "Контрагенты", Kind: metadata.KindCatalog, ID: id}
	if got := o.GetRefUUID(); got != "33333333-3333-3333-3333-333333333333" {
		t.Errorf("GetRefUUID = %q", got)
	}
}

func TestObject_GetRefUUID_Nil(t *testing.T) {
	var o *Object
	if got := o.GetRefUUID(); got != "" {
		t.Errorf("nil-Object.GetRefUUID = %q, ожидался пустой", got)
	}
}

// String() — display-имя по конвенции Наименование/Name/Номер/Number.
func TestObject_String_Naimenovanie(t *testing.T) {
	o := &Object{
		Type:   "Контрагенты",
		Kind:   metadata.KindCatalog,
		ID:     uuid.New(),
		Fields: map[string]any{"наименование": "ООО Ромашка"},
	}
	if got := o.String(); got != "ООО Ромашка" {
		t.Errorf("String() = %q, ожидалось «ООО Ромашка»", got)
	}
}

func TestObject_String_Number(t *testing.T) {
	o := &Object{
		Type:   "ПоступлениеТоваров",
		Kind:   metadata.KindDocument,
		ID:     uuid.New(),
		Fields: map[string]any{"номер": "ПОС-00042"},
	}
	if got := o.String(); got != "ПОС-00042" {
		t.Errorf("String() = %q", got)
	}
}

// Без конвенционных полей — fallback на Type:short-uuid.
func TestObject_String_Fallback(t *testing.T) {
	o := &Object{
		Type:   "Документ",
		Kind:   metadata.KindDocument,
		ID:     uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Fields: map[string]any{},
	}
	got := o.String()
	if got != "Документ:44444444" {
		t.Errorf("String() fallback = %q", got)
	}
}

func TestObject_String_Nil(t *testing.T) {
	var o *Object
	if got := o.String(); got != "" {
		t.Errorf("nil-Object.String() = %q", got)
	}
}

// Пустое Наименование не должно «выигрывать» — берётся следующее непустое.
func TestObject_String_SkipsEmpty(t *testing.T) {
	o := &Object{
		Type: "X",
		ID:   uuid.New(),
		Fields: map[string]any{
			"наименование": "  ",  // только пробелы
			"номер":        "Н-1",
		},
	}
	if got := o.String(); got != "Н-1" {
		t.Errorf("String() = %q, ожидалось «Н-1» (наименование пустое — взять следующее)", got)
	}
}
