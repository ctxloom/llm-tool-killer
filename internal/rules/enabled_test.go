package rules

import (
	"testing"

	"github.com/benjaminabbitt/llm-tool-killer/internal/ir"
)

// A rule with enabled:false stays in the file but never matches; absent or
// enabled:true participate normally.
func TestRuleEnabledToggle(t *testing.T) {
	yes, no := true, false
	mk := func(enabled *bool) *Config {
		return &Config{Rules: []Rule{{
			ID:      "no-go-test",
			Match:   Match{Command: CommandPattern{"go", "test"}},
			Enabled: enabled,
			Message: "use just test",
		}}}
	}
	script := cmd(ir.ShellBash, "go", "test")

	cases := []struct {
		name    string
		enabled *bool
		allowed bool
	}{
		{"absent defaults to enabled", nil, false},
		{"enabled true", &yes, false},
		{"enabled false is respected", &no, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if d := Evaluate(mk(tc.enabled), script); d.Allowed != tc.allowed {
				t.Fatalf("allowed=%v want %v", d.Allowed, tc.allowed)
			}
		})
	}
}

// enabled decodes from YAML, defaulting to true when the key is absent.
func TestEnabledParsesFromYAML(t *testing.T) {
	cfg, err := Parse([]byte(`version: 1
rules:
  - id: off-rule
    match: { command: [go, test] }
    enabled: false
  - id: on-rule
    match: { command: [git, push, --force] }
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rules[0].isEnabled() {
		t.Error("enabled:false should disable rule 0")
	}
	if !cfg.Rules[1].isEnabled() {
		t.Error("absent enabled should default to true")
	}
}
