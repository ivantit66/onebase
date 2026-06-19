package langref

import (
	"strings"
	"testing"
)

// E1: у Page-дескрипторов раньше были слишком общие алиасы («Ссылка» у Кнопки,
// «Добавить» у Пункта), затенявшие канонические методы при ByName. После их
// удаления ByName должен резолвить эти имена в канонические дескрипторы.
func TestByName_PageAliasesDoNotShadow(t *testing.T) {
	if d, ok := ByName("Ссылка"); !ok || strings.ToLower(d.Name) != "ссылка" {
		t.Errorf("ByName(\"Ссылка\") → %q (ok=%v); ожидалась каноническая «ссылка», не «кнопка»", d.Name, ok)
	}
	if d, ok := ByName("Добавить"); !ok || strings.ToLower(d.Name) != "добавить" {
		t.Errorf("ByName(\"Добавить\") → %q (ok=%v); ожидалась каноническая «добавить», не «пункт»", d.Name, ok)
	}
}
