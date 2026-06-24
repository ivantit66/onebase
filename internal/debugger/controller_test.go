package debugger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testArray []any

func (a testArray) Iterate() []any { return []any(a) }

func TestBreakpointLifecycleNormalizesFiles(t *testing.T) {
	dc := NewDebugController()
	s := dc.StartSession(`C:\cfg\заказ.posting.os`)
	require.NotNil(t, s)
	assert.Same(t, s, dc.GetSession(s.ID))

	bp := s.SetBreakpoint(`/tmp/заказ.posting.os`, 12, "Amount > 0")
	assert.Equal(t, "post-Заказ", bp.File)
	assert.Equal(t, 1, bp.MapLen)
	assert.Equal(t, 1, bp.EntryLen)

	got := s.CheckBreakpoint("post-Заказ", 12)
	require.NotNil(t, got)
	assert.Equal(t, 1, got.HitCount)
	assert.True(t, s.HasBreakpointsForFile("заказ.posting.os"))
	assert.Len(t, s.GetBreakpointsForFile("заказ.posting.os"), 1)

	toggled := s.ToggleBreakpoint("заказ.posting.os", 12)
	require.NotNil(t, toggled)
	assert.False(t, toggled.Enabled)
	assert.Nil(t, s.CheckBreakpoint("заказ.posting.os", 12))

	assert.True(t, s.RemoveBreakpoint("заказ.posting.os", 12))
	assert.False(t, s.RemoveBreakpoint("заказ.posting.os", 12))
	assert.Empty(t, s.GetBreakpoints())

	dc.RemoveSession(s.ID)
	assert.Nil(t, dc.GetSession(s.ID))
	assert.Equal(t, StateStopped, s.State)
}

func TestCallStackSteppingAndSnapshot(t *testing.T) {
	s := NewDebugController().StartSession("demo.proc.os")
	s.PushFrame("Outer", 10)
	s.PushFrame("Inner", 20)
	assert.Equal(t, 2, s.StackDepth())
	assert.Equal(t, []StackFrame{{Procedure: "Outer", Line: 10}, {Procedure: "Inner", Line: 20}}, s.GetCallStack())
	s.PopFrame()
	assert.Equal(t, 1, s.StackDepth())

	s.mu.Lock()
	s.State = StatePaused
	s.currentLoc = &Location{File: "demo.proc.os", Line: 21}
	s.lastDepth = 2
	s.vars = map[string]any{
		"Name":            "short",
		"Ok":              true,
		"Items":           testArray{"a", float64(2)},
		"__debug_session": "hidden",
	}
	s.pauseReason = "breakpoint"
	s.mu.Unlock()

	s.Step(StepOver)
	assert.False(t, s.ShouldStep("other.proc.os", 1))
	assert.False(t, s.ShouldStep("demo.proc.os", 3))
	assert.True(t, s.ShouldStep("demo.proc.os", 2))

	s.Step(StepInto)
	assert.True(t, s.ShouldStep("demo.proc.os", 99))

	s.Step(StepOut)
	assert.True(t, s.ShouldStep("demo.proc.os", 1))

	snap := s.Snapshot()
	assert.Equal(t, StateRunning, snap.State)
	assert.Equal(t, "breakpoint", snap.PauseReason)
	assert.Len(t, snap.Stack, 1)
	assert.NotEmpty(t, snap.Variables)
	for _, v := range snap.Variables {
		assert.NotEqual(t, "__debug_session", v.Name)
	}
}

func TestFormatValueGetTypeNameAndParseUserValue(t *testing.T) {
	assert.Equal(t, "Неопределено", FormatValue(nil))
	assert.Equal(t, "Истина", FormatValue(true))
	assert.Equal(t, "42", FormatValue(float64(42)))
	assert.Equal(t, "3.14", FormatValue(float64(3.14159)))
	assert.Equal(t, "Массив[2]{0: a, 1: 2}", FormatValue(testArray{"a", float64(2)}))
	assert.Equal(t, "Строка", GetTypeName("x"))
	assert.Equal(t, "Число", GetTypeName(10))
	assert.Equal(t, "Булево", GetTypeName(false))

	n, err := ParseUserValue("10.5", "Число")
	require.NoError(t, err)
	assert.Equal(t, 10.5, n)
	b, err := ParseUserValue("Истина", "Булево")
	require.NoError(t, err)
	assert.Equal(t, true, b)
	s, err := ParseUserValue("abc", "Строка")
	require.NoError(t, err)
	assert.Equal(t, "abc", s)
	_, err = ParseUserValue("maybe", "Булево")
	assert.Error(t, err)
	_, err = ParseUserValue("x", "Дата")
	assert.Error(t, err)
}

func TestGlobalDebugControllerLifecycle(t *testing.T) {
	g := NewGlobalDebugController()
	assert.False(t, g.IsEnabled())
	assert.Nil(t, g.Session())

	first := g.Enable()
	assert.True(t, g.IsEnabled())
	assert.Same(t, first, g.Session())

	second := g.Enable()
	assert.Equal(t, StateStopped, first.State)
	assert.Same(t, second, g.Session())

	g.SetSession(nil)
	assert.False(t, g.IsEnabled())
	assert.Nil(t, g.Session())
	assert.Equal(t, StateStopped, second.State)

	third := NewDebugController().StartSession("module.os")
	g.SetSession(third)
	assert.True(t, g.IsEnabled())
	assert.Same(t, third, g.Session())
	g.Disable()
	assert.False(t, g.IsEnabled())
	assert.Nil(t, g.Session())
	assert.Equal(t, StateStopped, third.State)
}
