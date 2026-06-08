package runtime_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

// Обе формы адресации субконто в DSL должны попасть в строку движения плоскими
// ключами: краткая Дв.СубконтоN → субконто<N>, именованная Дв.Субконто.<Имя> →
// субконто_<имя>. Это путь, который потом разбирает storage.WriteAccountMovements.
func TestMovements_SubcontoAddressing(t *testing.T) {
	src := `Процедура ОбработкаПроведения()
  Дв = Движения.БухУчёт.Добавить();
  Дв.СчётДт = "41";
  Дв.СчётКт = "60";
  Дв.Субконто1 = "Поставщик-А";
  Дв.Субконто.Номенклатура = "Товар-X";
КонецПроцедуры`

	l := lexer.New(src, "test.os")
	prog, err := parser.New(l).ParseProgram()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	mc := runtime.NewMovementsCollector("ПоступлениеТоваров", uuid.New())
	obj := runtime.NewObject("ПоступлениеТоваров", metadata.KindDocument)

	interp := interpreter.New()
	if err := interp.Run(prog.Procedures[0], obj, map[string]any{"Движения": mc}); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Интерпретатор приводит имя регистра из Движения.БухУчёт к нижнему регистру.
	rows := mc.All()["бухучёт"]
	if len(rows) != 1 {
		t.Fatalf("ожидалась 1 строка движения, получили %d", len(rows))
	}
	row := rows[0]

	if got := row["счётдт"]; got != "41" {
		t.Errorf("счётдт: ожидался 41, получили %v", got)
	}
	if got := row["субконто1"]; got != "Поставщик-А" {
		t.Errorf("субконто1 (краткая форма): ожидался Поставщик-А, получили %v", got)
	}
	if got := row["субконто_номенклатура"]; got != "Товар-X" {
		t.Errorf("субконто_номенклатура (именованная форма): ожидался Товар-X, получили %v", got)
	}
}
