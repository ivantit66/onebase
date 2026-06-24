package mcp

import "testing"

func TestToolsIncludeRefactorPreviewAndGateWrite(t *testing.T) {
	s := &Server{cfg: Config{Project: "."}, exe: "onebase"}
	names := toolNames(s.tools())
	if !names["refactor_preview"] {
		t.Fatalf("refactor_preview tool missing: %+v", names)
	}
	if !names["config_versions"] || !names["config_diff"] {
		t.Fatalf("config version read-only tools missing: %+v", names)
	}
	if names["refactor_write"] {
		t.Fatalf("refactor_write must not be exposed without AllowWrite")
	}
	if names["config_rollback"] {
		t.Fatalf("config_rollback must not be exposed without AllowWrite")
	}

	s.cfg.AllowRefactorWrite = true
	names = toolNames(s.tools())
	if !names["refactor_write"] {
		t.Fatalf("refactor_write tool missing with AllowRefactorWrite: %+v", names)
	}
	if names["fmt_write"] || names["procrun"] || names["config_rollback"] {
		t.Fatalf("specific refactor permission must not expose other mutating tools: %+v", names)
	}

	s.cfg = Config{Project: ".", AllowConfigRollback: true}
	names = toolNames(s.tools())
	if !names["config_rollback"] {
		t.Fatalf("config_rollback tool missing with AllowConfigRollback: %+v", names)
	}
	if names["refactor_write"] || names["fmt_write"] || names["procrun"] {
		t.Fatalf("specific rollback permission must not expose other mutating tools: %+v", names)
	}

	s.cfg = Config{Project: ".", AllowWrite: true}
	names = toolNames(s.tools())
	if !names["refactor_write"] || !names["config_rollback"] || !names["fmt_write"] || !names["procrun"] {
		t.Fatalf("all mutating tools must be exposed with AllowWrite: %+v", names)
	}
}

func TestToolArgsEnforceSpecificWritePermissions(t *testing.T) {
	s := &Server{cfg: Config{Project: "."}, exe: "onebase"}
	if _, err := s.toolArgs("refactor_write", map[string]any{"type": "rename-object", "from": "A", "to": "B"}); err == nil {
		t.Fatal("refactor_write must require AllowRefactorWrite or AllowWrite")
	}
	s.cfg.AllowRefactorWrite = true
	if args, err := s.toolArgs("refactor_write", map[string]any{"type": "rename-object", "from": "A", "to": "B"}); err != nil {
		t.Fatalf("refactor_write with AllowRefactorWrite returned error: %v", err)
	} else if !containsArg(args, "--write") {
		t.Fatalf("refactor_write args must include --write: %#v", args)
	}

	s.cfg = Config{Project: "."}
	if _, err := s.toolArgs("fmt_write", map[string]any{}); err == nil {
		t.Fatal("fmt_write must require AllowFmtWrite or AllowWrite")
	}
	s.cfg.AllowFmtWrite = true
	if _, err := s.toolArgs("fmt_write", map[string]any{}); err != nil {
		t.Fatalf("fmt_write with AllowFmtWrite returned error: %v", err)
	}
}

func toolNames(tools []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, t := range tools {
		if name, ok := t["name"].(string); ok {
			out[name] = true
		}
	}
	return out
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
