package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

const hookCmd = "ltk evaluate --config .ltk.yaml"

func decodeSettingsJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, b)
	}
	return m
}

func preToolUse(t *testing.T, m map[string]any) []any {
	t.Helper()
	hooks, ok := m["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks missing/not object: %v", m["hooks"])
	}
	pre, ok := hooks["PreToolUse"].([]any)
	if !ok {
		t.Fatalf("PreToolUse missing/not array: %v", hooks["PreToolUse"])
	}
	return pre
}

func TestClaudeHookCommand(t *testing.T) {
	if got := (ClaudeCode{}).HookCommand("ltk", ".ltk.yaml"); got != hookCmd {
		t.Errorf("HookCommand = %q", got)
	}
	if got := (ClaudeCode{}).HookCommand("ltk", ""); got != "ltk evaluate" {
		t.Errorf("HookCommand without config = %q", got)
	}
}

func TestClaudeInstallIntoEmpty(t *testing.T) {
	out, err := ClaudeCode{}.Install(nil, hookCmd)
	if err != nil {
		t.Fatal(err)
	}
	pre := preToolUse(t, decodeSettingsJSON(t, out))
	if len(pre) != 1 {
		t.Fatalf("want 1 entry, got %d", len(pre))
	}
	if entry := pre[0].(map[string]any); entry["matcher"] != claudeMatcher {
		t.Errorf("matcher = %v", entry["matcher"])
	}
	if !strings.Contains(string(out), hookCmd) {
		t.Error("command missing from output")
	}
}

func TestClaudeInstallPreservesOtherSettings(t *testing.T) {
	existing := `{
      "model": "opus",
      "hooks": {
        "PreToolUse": [{"matcher": "Edit", "hooks": [{"type": "command", "command": "other"}]}],
        "PostToolUse": [{"matcher": "Bash", "hooks": []}]
      }
    }`
	out, err := ClaudeCode{}.Install([]byte(existing), hookCmd)
	if err != nil {
		t.Fatal(err)
	}
	m := decodeSettingsJSON(t, out)
	if m["model"] != "opus" {
		t.Error("unrelated setting dropped")
	}
	if _, ok := m["hooks"].(map[string]any)["PostToolUse"]; !ok {
		t.Error("sibling PostToolUse dropped")
	}
	if len(preToolUse(t, m)) != 2 {
		t.Errorf("want 2 PreToolUse entries, got %d", len(preToolUse(t, m)))
	}
}

func TestClaudeInstallIdempotent(t *testing.T) {
	first, _ := ClaudeCode{}.Install(nil, hookCmd)
	second, err := ClaudeCode{}.Install(first, hookCmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(preToolUse(t, decodeSettingsJSON(t, second))) != 1 {
		t.Error("re-install added a duplicate")
	}
}

func TestClaudeUninstallIsInverse(t *testing.T) {
	// install then uninstall should leave other entries intact and ours gone.
	existing := `{"hooks":{"PreToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"other"}]}]}}`
	installed, err := ClaudeCode{}.Install([]byte(existing), hookCmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(preToolUse(t, decodeSettingsJSON(t, installed))) != 2 {
		t.Fatal("install should have added our entry")
	}
	removed, err := ClaudeCode{}.Uninstall(installed, hookCmd)
	if err != nil {
		t.Fatal(err)
	}
	pre := preToolUse(t, decodeSettingsJSON(t, removed))
	if len(pre) != 1 {
		t.Fatalf("want 1 entry after uninstall, got %d", len(pre))
	}
	if strings.Contains(string(removed), hookCmd) {
		t.Error("our hook command should be gone")
	}
}

func TestClaudeUninstallNoHooksKeyIsNoop(t *testing.T) {
	out, err := ClaudeCode{}.Uninstall([]byte(`{"model":"opus"}`), hookCmd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "opus") {
		t.Error("uninstall should preserve unrelated settings")
	}
}

func TestClaudeInstallRejectsMalformed(t *testing.T) {
	if _, err := (ClaudeCode{}).Install([]byte(`{"hooks":"nope"}`), hookCmd); err == nil {
		t.Error("expected error when hooks is not an object")
	}
}
