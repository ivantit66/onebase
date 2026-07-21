package langref

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
)

// TestNumerators_Described — DX (issue #358 / PR #364): корневой объект
// Нумераторы должен быть виден в автодополнении/AI-справке конфигуратора и
// зарегистрирован в реестре имён, иначе Нумераторы.СледующийНомер —
// невидимая возможность (работает в рантайме, но не подсказывается).
// Образец — ПредопределённыеЗначения: тоже корневой объект, описан KindFunc.
func TestNumerators_Described(t *testing.T) {
	d, ok := ByName("нумераторы")
	if !ok {
		t.Fatal(`нет дескриптора для Нумераторы (ByName("нумераторы"))`)
	}
	if d.Display != "Нумераторы" {
		t.Errorf("Display = %q, хочу %q", d.Display, "Нумераторы")
	}
	if !strings.Contains(d.Signature, "СледующийНомер") {
		t.Errorf("Signature %q не упоминает СледующийНомер", d.Signature)
	}
	if _, ok := ByName("Numerators"); !ok {
		t.Error("нет англ. алиаса Numerators")
	}

	known := interpreter.KnownBuiltinNames()
	if _, ok := known["нумераторы"]; !ok {
		t.Error(`"нумераторы" отсутствует в KnownBuiltinNames`)
	}
	if _, ok := known["numerators"]; !ok {
		t.Error(`"numerators" отсутствует в KnownBuiltinNames`)
	}
}
