package debugger

import (
	"encoding/json"
	"fmt"
	"time"
)

// DebugState represents the current state of a debug session
type DebugState int

const (
	StateRunning DebugState = iota
	StatePaused
	StateStopped
)

func (s DebugState) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s DebugState) String() string {
	switch s {
	case StateRunning:
		return "running"
	case StatePaused:
		return "paused"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Location represents a specific position in source code
type Location struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Col       int    `json:"col"`
	Procedure string `json:"procedure,omitempty"`
}

// Breakpoint represents a breakpoint in source code
type Breakpoint struct {
	ID        string    `json:"id"`
	File      string    `json:"file"`
	Line      int       `json:"line"`
	Enabled   bool      `json:"enabled"`
	Condition string    `json:"condition,omitempty"`
	HitCount  int       `json:"hit_count"`
	CreatedAt time.Time `json:"created_at"`

	// Diagnostic fields (not part of the breakpoint data)
	MapLen   int `json:"map_len"`
	EntryLen int `json:"entry_len"`
}

// StackFrame represents a frame in the call stack
type StackFrame struct {
	Module    string `json:"module"`
	Procedure string `json:"procedure"`
	Line      int    `json:"line"`
}

// EvaluateResult represents the result of evaluating an expression
type EvaluateResult struct {
	Value   any    `json:"value"`
	Type    string `json:"type"`
	IsError bool   `json:"is_error"`
	Error   string `json:"error,omitempty"`
}

// StepMode defines how stepping should behave
type StepMode int

const (
	StepNone StepMode = iota
	StepOver
	StepInto
	StepOut
)

// StatusSnapshot is the JSON response for GET /debug/status
type StatusSnapshot struct {
	State       DebugState      `json:"state"`
	Location    *Location       `json:"location,omitempty"`
	Variables   []VarEntry      `json:"variables,omitempty"`
	Stack       []StackFrame    `json:"stack,omitempty"`
	Breakpoints []Breakpoint    `json:"breakpoints,omitempty"`
	PauseReason string          `json:"pause_reason,omitempty"` // "breakpoint" or "step"
	Error       string          `json:"error,omitempty"`

	// Diagnostics — filled when debug is enabled
	DiagLastFile string   `json:"diag_last_file,omitempty"`
	DiagLastLine int      `json:"diag_last_line,omitempty"`
	DiagBPKeys   []string `json:"diag_bp_keys"`
	DiagBPCount  int      `json:"diag_bp_count"`
	DiagMessages []string `json:"diag_messages,omitempty"`
}

// VarEntry represents a single variable in the debug view
type VarEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

// typeNamer is implemented by DSL collection types to return a user-friendly type name.
type typeNamer interface{ TypeName() string }

// FormatValue formats a value for display in the debugger
func FormatValue(v any) string {
	if v == nil {
		return "Неопределено"
	}
	switch val := v.(type) {
	case string:
		if len(val) > 100 {
			return val[:100] + "..."
		}
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%g", val)
		}
		return fmt.Sprintf("%.2f", val)
	case int, int32, int64:
		return fmt.Sprintf("%d", val)
	case bool:
		if val {
			return "Истина"
		}
		return "Ложь"
	default:
		s := fmt.Sprintf("%v", v)
		if len(s) > 100 {
			return s[:100] + "..."
		}
		return s
	}
}

// GetTypeName returns the DSL type name for a Go value
func GetTypeName(v any) string {
	if v == nil {
		return "Неопределено"
	}
	if tn, ok := v.(typeNamer); ok {
		return tn.TypeName()
	}
	switch v.(type) {
	case bool:
		return "Булево"
	case float64, float32, int, int32, int64:
		return "Число"
	case string:
		return "Строка"
	default:
		return fmt.Sprintf("%T", v)
	}
}
