package rules

import (
	"testing"

	"github.com/benjaminabbitt/llm-tool-killer/internal/ir"
)

// mode controls whether a rule fires: disable → inert, enable/confirm → fires.
func TestRuleModeMatching(t *testing.T) {
	mk := func(mode Mode) *Config {
		return &Config{Rules: []Rule{{
			ID:      "no-go-test",
			Match:   Match{Command: CommandPattern{"go", "test"}},
			Mode:    mode,
			Message: "use just test",
		}}}
	}
	script := cmd(ir.ShellBash, "go", "test")

	cases := []struct {
		name    string
		mode    Mode
		allowed bool
	}{
		{"absent defaults to enable (fires)", "", false},
		{"enable fires", ModeEnable, false},
		{"confirm fires", ModeConfirm, false},
		{"disable is inert", ModeDisable, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if d := Evaluate(mk(tc.mode), script); d.Allowed != tc.allowed {
				t.Fatalf("allowed=%v want %v", d.Allowed, tc.allowed)
			}
		})
	}
}

// mode decodes from YAML, defaulting to enable when absent.
func TestModeParsesFromYAML(t *testing.T) {
	cfg, err := Parse([]byte(`version: 1
rules:
  - id: off-rule
    match: { command: [go, test] }
    mode: disable
  - id: default-rule
    match: { command: [git, push, --force] }
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rules[0].isEnabled() {
		t.Error("mode:disable should disable rule 0")
	}
	if cfg.Rules[1].mode() != ModeEnable {
		t.Error("absent mode should default to enable")
	}
}

func TestInvalidModeRejected(t *testing.T) {
	_, err := Parse([]byte("version: 1\nrules:\n  - id: x\n    mode: sometimes\n    match: { command: go }\n"))
	if err == nil {
		t.Error("an unknown mode should be a validation error")
	}
}

// confirmPolicy: only confirm-mode rules are repeatable, and only with a window.
func TestConfirmPolicy(t *testing.T) {
	cases := []struct {
		name       string
		rule       Rule
		defs       Defaults
		repeatable bool
		window     int
	}{
		{"enable is inviolate", Rule{Mode: ModeEnable}, Defaults{RepeatWindowSeconds: 30}, false, 0},
		{"disable is not repeatable", Rule{Mode: ModeDisable}, Defaults{RepeatWindowSeconds: 30}, false, 0},
		{"confirm uses global window", Rule{Mode: ModeConfirm}, Defaults{RepeatWindowSeconds: 30}, true, 30},
		{"confirm overrides window", Rule{Mode: ModeConfirm, WindowSeconds: 5}, Defaults{RepeatWindowSeconds: 30}, true, 5},
		{"confirm with no window is inert", Rule{Mode: ModeConfirm}, Defaults{}, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep, win := tc.rule.confirmPolicy(tc.defs)
			if rep != tc.repeatable || win != tc.window {
				t.Fatalf("got (repeatable=%v, window=%d), want (%v, %d)", rep, win, tc.repeatable, tc.window)
			}
		})
	}
}
