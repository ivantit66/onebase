package interpreter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

// runProcErr прогоняет первую процедуру и возвращает ошибку выполнения (если есть).
func runProcErr(t *testing.T, src string) error {
	t.Helper()
	prog, err := parser.New(lexer.New(src, "test.os")).ParseProgram()
	require.NoError(t, err)
	require.NotEmpty(t, prog.Procedures)
	obj := runtime.NewObject("Test", metadata.KindDocument)
	var result any
	return interpreter.New().RunWithResult(prog.Procedures[0], obj, &result)
}

// Чтение по точке у Соответствия (частая ошибка с результатом ПрочитатьJSON)
// раньше тихо возвращало Неопределено; теперь — понятная ошибка с подсказкой
// про Получить(). См. поведение 1С: Соответствие.Ключ — ошибка, а не nil.
func TestMapDotRead_RaisesWithHint(t *testing.T) {
	src := `Процедура Тест()
		м = ПрочитатьJSON("{""ИНН"":""7701234567""}");
		Возврат м.ИНН;
	КонецПроцедуры`
	err := runProcErr(t, src)
	require.Error(t, err)
	assert.ErrorContains(t, err, "Соответствие не поддерживает чтение по точке")
	assert.ErrorContains(t, err, `Получить("ИНН")`)
}

// Запись по точке у Соответствия раньше тихо терялась; теперь — ошибка с
// подсказкой про Вставить().
func TestMapDotWrite_RaisesWithHint(t *testing.T) {
	src := `Процедура Тест()
		м = Новый Соответствие;
		м.ИНН = "7701234567";
	КонецПроцедуры`
	err := runProcErr(t, src)
	require.Error(t, err)
	assert.ErrorContains(t, err, "Соответствие не поддерживает запись по точке")
	assert.ErrorContains(t, err, `Вставить("ИНН"`)
}

// Позитивный контроль: штатный доступ через Получить/Вставить работает как и
// раньше — правка не задела метод-вызовы.
func TestMapMethodAccess_StillWorks(t *testing.T) {
	src := `Процедура Тест()
		м = Новый Соответствие;
		м.Вставить("ИНН", "7701234567");
		Возврат м.Получить("ИНН");
	КонецПроцедуры`
	err := runProcErr(t, src)
	require.NoError(t, err)
}
