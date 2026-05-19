package ui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/ivantit66/onebase/internal/debugger"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
)

// ── JSON helpers ─────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("empty body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// corsMiddleware allows cross-origin requests from the configurator (launcher server).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Global Debug Handlers ────────────────────────────────────────

// debugGlobalEnable handles POST /debug/global/enable
func (s *Server) debugGlobalEnable(w http.ResponseWriter, r *http.Request) {
	log.Printf("[DEBUG] enable called, globalDebug=%p interp=%p", s.globalDebug, s.interp)
	sess := s.globalDebug.Enable()
	s.interp.DebugHook = sess
	log.Printf("[DEBUG] enable done, session=%p enabled=%v", sess, s.globalDebug.IsEnabled())
	writeJSON(w, 200, map[string]any{
		"status":       "enabled",
		"session":      "global",
		"dbg_ptr":      fmt.Sprintf("%p", s.globalDebug),
		"sess_ptr":     fmt.Sprintf("%p", sess),
		"interp_ptr":   fmt.Sprintf("%p", s.interp),
	})
}

// debugGlobalDisable handles POST /debug/global/disable
func (s *Server) debugGlobalDisable(w http.ResponseWriter, r *http.Request) {
	s.globalDebug.Disable()
	s.interp.DebugHook = nil
	writeJSON(w, 200, map[string]string{"status": "disabled"})
}

// debugGlobalStatus handles GET /debug/global/status
func (s *Server) debugGlobalStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	sess := s.globalDebug.Session()
	log.Printf("[DEBUG] status called, globalDebug=%p session=%p", s.globalDebug, sess)
	if sess == nil {
		writeJSON(w, 200, map[string]any{"state": "disabled", "dbg_ptr": fmt.Sprintf("%p", s.globalDebug)})
		return
	}
	writeJSON(w, 200, sess.Snapshot())
}

// debugGlobalBreakpoint handles POST /debug/global/breakpoint
func (s *Server) debugGlobalBreakpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Action string `json:"action"` // "set", "remove", "toggle"
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	sess := s.globalDebug.Session()
	if sess == nil {
		writeJSON(w, 400, map[string]string{"error": "debug not enabled"})
		return
	}

	switch req.Action {
	case "set":
		bp := sess.SetBreakpoint(req.File, req.Line, "")
		snap := sess.Snapshot()
		writeJSON(w, 200, map[string]any{
			"id": bp.ID, "file": bp.File, "line": bp.Line, "enabled": bp.Enabled,
			"bp_count": snap.DiagBPCount, "bp_keys": snap.DiagBPKeys,
		})
	case "remove":
		sess.RemoveBreakpoint(req.File, req.Line)
		writeJSON(w, 200, map[string]string{"status": "removed"})
	case "toggle":
		existing := sess.CheckBreakpoint(req.File, req.Line)
		if existing != nil {
			sess.RemoveBreakpoint(req.File, req.Line)
			writeJSON(w, 200, map[string]string{"status": "removed"})
		} else {
			bp := sess.SetBreakpoint(req.File, req.Line, "")
			snap := sess.Snapshot()
			writeJSON(w, 200, map[string]any{
				"id": bp.ID, "file": bp.File, "line": bp.Line, "enabled": bp.Enabled,
				"bp_count": snap.DiagBPCount, "bp_keys": snap.DiagBPKeys,
			})
		}
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// debugGlobalContinue handles POST /debug/global/continue
func (s *Server) debugGlobalContinue(w http.ResponseWriter, r *http.Request) {
	sess := s.globalDebug.Session()
	if sess == nil {
		writeJSON(w, 400, map[string]string{"error": "debug not enabled"})
		return
	}
	sess.Continue()
	writeJSON(w, 200, map[string]string{"status": "continued"})
}

// debugGlobalStep handles POST /debug/global/step
func (s *Server) debugGlobalStep(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"` // "into", "over", "out"
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	sess := s.globalDebug.Session()
	if sess == nil {
		writeJSON(w, 400, map[string]string{"error": "debug not enabled"})
		return
	}

	var mode debugger.StepMode
	switch req.Mode {
	case "into":
		mode = debugger.StepInto
	case "over":
		mode = debugger.StepOver
	case "out":
		mode = debugger.StepOut
	default:
		mode = debugger.StepOver
	}

	sess.Step(mode)
	writeJSON(w, 200, map[string]string{"status": "stepped"})
}

// debugGlobalStop handles POST /debug/global/stop — force stop current execution
func (s *Server) debugGlobalStop(w http.ResponseWriter, r *http.Request) {
	s.globalDebug.Disable() // Disable stops the session internally
	s.interp.DebugHook = nil
	writeJSON(w, 200, map[string]string{"status": "stopped"})
}

// debugGlobalEvaluate handles POST /debug/global/evaluate
func (s *Server) debugGlobalEvaluate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Expr string `json:"expr"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if req.Expr == "" {
		writeJSON(w, 400, map[string]string{"error": "empty expression"})
		return
	}

	// If a debug session is active, only evaluate when paused — otherwise the
	// expression refers to DSL locals (e.g. Запрос.Текст) that don't exist
	// outside the paused frame, and a standalone fallback would return nil,
	// making the watch panel flicker every poll between paused and running.
	sess := s.globalDebug.Session()
	if sess != nil {
		snap := sess.Snapshot()
		if snap.State != debugger.StatePaused {
			writeJSON(w, 200, debugger.EvaluateResult{
				IsError: true,
				Error:   "interpreter is not paused",
			})
			return
		}
		result := sess.Evaluate(req.Expr, func(expr string) (any, error) {
			return standaloneEval(s, expr)
		})
		// Convert Value to a string so JSON-marshaling never silently drops
		// unexported fields (e.g. *Map, *Array) — client always gets a readable string.
		result.Value = debugger.FormatValue(result.Value)
		writeJSON(w, 200, result)
		return
	}

	// No debug session — fall back to standalone evaluation (e.g. "2+2").
	val, err := standaloneEval(s, req.Expr)
	if err != nil {
		writeJSON(w, 200, debugger.EvaluateResult{
			IsError: true,
			Error:   err.Error(),
		})
		return
	}
	writeJSON(w, 200, debugger.EvaluateResult{
		Value: debugger.FormatValue(val),
		Type:  debugger.GetTypeName(val),
	})
}

// standaloneEval parses and evaluates a DSL expression
func standaloneEval(s *Server, expr string) (any, error) {
	l := lexer.New(expr, "<console>")
	p := parser.New(l)
	parsed, err := p.ParseExpr()
	if err != nil {
		return nil, err
	}

	tmpInterp := interpreter.New()
	tmpInterp.LookupProc = s.reg.GetModuleProc
	result := tmpInterp.EvalExpr(parsed, nil)
	return result, nil
}

