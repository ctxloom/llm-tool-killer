package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/benjaminabbitt/llm-tool-killer/internal/engine"
)

// confirmByRepeat implements "run it again to permit": the first denial is
// remembered, and an identical re-run within the window is allowed.
func TestConfirmByRepeat(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), ".ltk", "state.json")
	denied := engine.Response{Allow: false, Reason: "use just test"}
	const cmd = "go test ./..."

	// First attempt: still denied, but the reason now tells the agent how to
	// proceed, and the override is armed on disk.
	first := confirmByRepeat(denied, cmd, stateFile, 30*time.Second)
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
	second := confirmByRepeat(denied, cmd, stateFile, 30*time.Second)
	if !second.Allow {
		t.Error("repeating the exact command within the window must be allowed")
	}

	// The override is single-use: a third attempt re-arms and denies again.
	third := confirmByRepeat(denied, cmd, stateFile, 30*time.Second)
	if third.Allow {
		t.Error("override is single-use; the next attempt must be denied again")
	}
}

// A different command does not consume another command's armed override.
func TestConfirmByRepeatIsPerCommand(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	denied := engine.Response{Allow: false, Reason: "nope"}

	confirmByRepeat(denied, "go test", stateFile, 30*time.Second) // arm "go test"
	other := confirmByRepeat(denied, "git push --force", stateFile, 30*time.Second)
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

	confirmByRepeat(denied, "go test", stateFile, time.Nanosecond)
	time.Sleep(time.Millisecond)
	again := confirmByRepeat(denied, "go test", stateFile, time.Nanosecond)
	if again.Allow {
		t.Error("an elapsed window must not allow the repeat")
	}
}
