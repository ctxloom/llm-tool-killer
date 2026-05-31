package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeCodeDecode(t *testing.T) {
	payload := `{
		"hook_event_name": "PreToolUse",
		"tool_name": "Bash",
		"tool_input": {"command": "go test ./...", "description": "run tests"}
	}`
	req, err := ClaudeCode{}.Decode([]byte(payload))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if req.ToolName != "Bash" || req.Command != "go test ./..." {
		t.Fatalf("req = %+v", req)
	}
}

func TestClaudeCodeEncodeAllowIsSilent(t *testing.T) {
	out, err := ClaudeCode{}.Encode(Response{Allow: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Stdout) != 0 || len(out.Stderr) != 0 || out.ExitCode != 0 {
		t.Errorf("allow should be a zero Output, got %+v", out)
	}
}

func TestClaudeCodeEncodeDeny(t *testing.T) {
	out, err := ClaudeCode{}.Encode(Response{
		Allow:   false,
		Reason:  "Use just test.",
		Suggest: "just test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 0 {
		t.Errorf("Claude Code deny uses exit 0 + JSON, got exit %d", out.ExitCode)
	}
	var decoded ccOutput
	if err := json.Unmarshal(out.Stdout, &decoded); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	h := decoded.HookSpecificOutput
	if h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" {
		t.Fatalf("hook output = %+v", h)
	}
	if !strings.Contains(h.PermissionDecisionReason, "just test") {
		t.Errorf("reason missing suggestion: %q", h.PermissionDecisionReason)
	}
}

func TestGet(t *testing.T) {
	if _, err := Get("claude-code"); err != nil {
		t.Errorf("claude-code should resolve: %v", err)
	}
	if _, err := Get("nope"); err == nil {
		t.Error("unknown engine should error")
	}
}

func TestMessage(t *testing.T) {
	cases := []struct {
		resp Response
		want string
	}{
		{Response{Reason: "no"}, "no"},
		{Response{Suggest: "just test"}, "Use instead: just test"},
		{Response{Reason: "no", Suggest: "just test"}, "no\n\nUse instead: just test"},
	}
	for _, c := range cases {
		if got := c.resp.Message(); got != c.want {
			t.Errorf("Message(%+v) = %q, want %q", c.resp, got, c.want)
		}
	}
}
