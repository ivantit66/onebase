package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/processor"
	"github.com/ivantit66/onebase/internal/runtime"
)

// runProcessor использует внешний VarsBuilder: переменные из него (например
// Справочники) доступны обработке задания; без builder'а — базовый набор.
func TestRunProcessor_UsesVarsBuilder(t *testing.T) {
	db, _ := openSchedulerTestDB(t)

	src := `Процедура Выполнить()
  Сообщить(МаркерВнешнегоОкружения());
КонецПроцедуры`
	l := lexer.New(src, "тест.proc.os")
	p := parser.New(l)
	prog, err := p.ParseProgram()
	require.NoError(t, err)

	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Programs: map[string]*ast.Program{"ТестЗадание": prog}})
	reg.LoadProcessors([]*processor.Processor{{Name: "ТестЗадание"}})

	interp := interpreter.New()
	interp.LookupProc = reg.GetModuleProc

	sched := New(db, reg, interp)
	marker := interpreter.BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
		return "из-внешнего-окружения", nil
	})
	sched.SetVarsBuilder(func(ctx context.Context, mc *runtime.MovementsCollector) map[string]any {
		return map[string]any{"МаркерВнешнегоОкружения": marker}
	})

	out, err := sched.runProcessor(context.Background(), &metadata.ScheduledJob{
		Name: "Тест", Processor: "ТестЗадание",
	})
	require.NoError(t, err)
	assert.Equal(t, "из-внешнего-окружения", out)
}
