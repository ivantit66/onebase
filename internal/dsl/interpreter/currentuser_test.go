package interpreter

import (
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
)

// ТекущийПользователь() / ИмяПользователя() инжектятся как
// builtins. Здесь моделируем инъекцию так же, как buildDSLVars: через
// extraVars. Проверяем что DSL может прочитать имя и поля.
func runUserFunc(t *testing.T, code string, extra map[string]any) any {
	t.Helper()
	l := lexer.New(code, "<test>")
	p := parser.New(l)
	prog, err := p.ParseProgram()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	i := New()
	var result any
	if err := i.RunWithResult(prog.Procedures[0], &MapThis{M: map[string]any{}}, &result, extra); err != nil {
		t.Fatalf("run: %v", err)
	}
	return result
}

func TestCurrentUser_UserName(t *testing.T) {
	extra := map[string]any{
		"ИмяПользователя": BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
			return "ivanov", nil
		}),
	}
	code := `Функция Тест()
  Возврат ИмяПользователя();
КонецФункции`
	if got := runUserFunc(t, code, extra); got != "ivanov" {
		t.Errorf("expected ivanov, got %v", got)
	}
}

func TestCurrentUser_FieldAccess(t *testing.T) {
	userObj := &MapThis{M: map[string]any{
		"Имя": "ivanov", "ПолноеИмя": "Иван Иванов", "Админ": true,
	}}
	extra := map[string]any{
		"ТекущийПользователь": BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
			return userObj, nil
		}),
	}
	code := `Функция Тест()
  П = ТекущийПользователь();
  Возврат П.ПолноеИмя;
КонецФункции`
	if got := runUserFunc(t, code, extra); got != "Иван Иванов" {
		t.Errorf("expected «Иван Иванов», got %v", got)
	}
}

// Сценарий из замечания: хелпер ищет настройку по имени пользователя
// с фолбэком на «Общие».
func TestCurrentUser_DefaultSettingScenario(t *testing.T) {
	extra := map[string]any{
		"ИмяПользователя": BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
			return "", nil // фоновое задание — нет пользователя
		}),
	}
	code := `Функция Тест()
  Имя = ИмяПользователя();
  Если Имя = "" Тогда
    Возврат "Общие";
  Иначе
    Возврат Имя;
  КонецЕсли;
КонецФункции`
	if got := runUserFunc(t, code, extra); got != "Общие" {
		t.Errorf("ожидался фолбэк «Общие», got %v", got)
	}
}
