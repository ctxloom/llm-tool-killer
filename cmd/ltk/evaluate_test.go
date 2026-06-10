package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ctxloom/llm-tool-killer/internal/engine"
)

// evaluate is the full decision path (decode → parse → rules → encode) minus
// process concerns (stdin, stream writes, exit), so it is testable end to end.
func TestEvaluateDeniesAndAllows(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "rules.yaml")
	cfg := `version: 1
rules:
  - id: no-force-push
    match: { command: [git, push, --force] }
    message: "no force pushes"
    suggest: "git push --force-with-lease"
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := func(command string) string {
		return `{"tool_name":"Bash","tool_input":{"command":` + strconv.Quote(command) + `}}`
	}

	t.Run("denied command produces a deny decision on stdout", func(t *testing.T) {
		out, err := evaluate("claude-code", cfgPath, "", strings.NewReader(payload("git push --force")))
		if err != nil {
			t.Fatal(err)
		}
		if out.ExitCode != 0 {
			t.Errorf("claude-code denials exit 0, got %d", out.ExitCode)
		}
		if !strings.Contains(string(out.Stdout), `"deny"`) || !strings.Contains(string(out.Stdout), "no force pushes") {
			t.Errorf("stdout should carry the deny decision and reason, got %s", out.Stdout)
		}
	})

	t.Run("allowed command writes nothing", func(t *testing.T) {
		out, err := evaluate("claude-code", cfgPath, "", strings.NewReader(payload("git status")))
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Stdout) != 0 || len(out.Stderr) != 0 || out.ExitCode != 0 {
			t.Errorf("allow must be a zero Output (pass-through), got %+v", out)
		}
	})

	t.Run("unreadable payload is an error", func(t *testing.T) {
		if _, err := evaluate("claude-code", cfgPath, "", strings.NewReader("not json")); err == nil {
			t.Error("malformed payload should surface as an error")
		}
	})
}

// Override state lives inside a .ltk directory: next to the config when the
// config is already in one, otherwise .ltk/state.json in the cwd — never loose
// in the repo root (legacy flat configs like .ltk.yaml used to cause that).
func TestStatePath(t *testing.T) {
	tests := []struct {
		name, config, want string
	}{
		{"ltk dir layout", filepath.Join(".ltk", "config.yaml"), filepath.Join(".ltk", "state.json")},
		{"absolute ltk dir", filepath.Join("/home/u/proj", ".ltk", "config.yaml"), filepath.Join("/home/u/proj", ".ltk", "state.json")},
		{"legacy flat config", ".ltk.yaml", filepath.Join(".ltk", "state.json")},
		{"named flat config", "llm-tool-killer.yaml", filepath.Join(".ltk", "state.json")},
		{"nested non-ltk config", filepath.Join(".config", "llm-tool-killer.yaml"), filepath.Join(".ltk", "state.json")},
		{"no config resolved", "", filepath.Join(".ltk", "state.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statePath(tt.config); got != tt.want {
				t.Errorf("statePath(%q) = %q, want %q", tt.config, got, tt.want)
			}
		})
	}
}

// confirmByRepeat implements "run it again to permit": the first denial is
// remembered, and an identical re-run within the window is allowed.
func TestConfirmByRepeat(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), ".ltk", "state.json")
	denied := engine.Response{Allow: false, Reason: "use just test"}
	const cmd = "go test ./..."

	// First attempt: still denied, but the reason now tells the agent how to
	// proceed, and the override is armed on disk.
	first := confirmByRepeat(denied, cmd, stateFile, 0, 30*time.Second)
	if first.Allow {
		t.Fatal("first attempt must still be denied")
	}
	if !strings.Contains(first.Reason, "again") {
		t.Errorf("denial reason should explain the repeat override, got %q", first.Reason)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file should have been written: %v", err)
	}

	// Second attempt, same command: allowed.
	second := confirmByRepeat(denied, cmd, stateFile, 0, 30*time.Second)
	if !second.Allow {
		t.Error("repeating the exact command within the window must be allowed")
	}

	// The override is single-use: a third attempt re-arms and denies again.
	third := confirmByRepeat(denied, cmd, stateFile, 0, 30*time.Second)
	if third.Allow {
		t.Error("override is single-use; the next attempt must be denied again")
	}
}

// A different command does not consume another command's armed override.
func TestConfirmByRepeatIsPerCommand(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	denied := engine.Response{Allow: false, Reason: "nope"}

	confirmByRepeat(denied, "go test", stateFile, 0, 30*time.Second) // arm "go test"
	other := confirmByRepeat(denied, "git push --force", stateFile, 0, 30*time.Second)
	if other.Allow {
		t.Error("a different command must not be allowed by another's armed override")
	}
}

// A zero/expired window never confirms: every attempt re-arms and denies. (The
// caller only invokes confirmByRepeat when the window is > 0, but the boundary
// of an immediately-elapsed window should still deny.)
func TestConfirmByRepeatTinyWindowDenies(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	denied := engine.Response{Allow: false, Reason: "nope"}

	confirmByRepeat(denied, "go test", stateFile, 0, time.Nanosecond)
	time.Sleep(time.Millisecond)
	again := confirmByRepeat(denied, "go test", stateFile, 0, time.Nanosecond)
	if again.Allow {
		t.Error("an elapsed window must not allow the repeat")
	}
}
