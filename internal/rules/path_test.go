package rules

import "testing"

func cfgWith(rule Rule) *Config { return &Config{Rules: []Rule{rule}} }

// A path rule blocks file edits to matching files and ignores command calls.
func TestEvaluatePathBasics(t *testing.T) {
	cfg := cfgWith(Rule{
		ID:      "no-hand-edit-version",
		Match:   Match{Path: []string{"VERSION"}},
		Message: "bump via versionator",
	})

	deny := []string{"VERSION", "/proj/VERSION", "sub/dir/VERSION"}
	for _, p := range deny {
		if EvaluatePath(cfg, p).Allowed {
			t.Errorf("editing %q should be denied", p)
		}
	}
	allow := []string{"VERSION.md", "src/version.go", "README.md", ""}
	for _, p := range allow {
		if !EvaluatePath(cfg, p).Allowed {
			t.Errorf("editing %q should be allowed", p)
		}
	}
}

// Glob patterns match basename and full path.
func TestEvaluatePathGlobs(t *testing.T) {
	cfg := cfgWith(Rule{
		ID:      "no-lockfiles",
		Match:   Match{Path: []string{"*.lock", "dist/*"}},
		Message: "don't hand-edit generated files",
	})
	for _, p := range []string{"go.lock", "a/b/c.lock", "dist/ltk"} {
		if EvaluatePath(cfg, p).Allowed {
			t.Errorf("%q should be denied", p)
		}
	}
	// `*` does not cross `/` (Go path.Match), so dist/* matches one level only.
	for _, p := range []string{"lockfile", "src/dist.go", "dist/sub/x"} {
		if !EvaluatePath(cfg, p).Allowed {
			t.Errorf("%q should be allowed", p)
		}
	}
}

// Path rules carry mode/confirm/suggest like command rules.
func TestEvaluatePathModeAndConfirm(t *testing.T) {
	// confirm mode is repeatable when a window is configured.
	cfg := &Config{
		Defaults: Defaults{RepeatWindowSeconds: 30},
		Rules: []Rule{{
			ID:      "confirmable",
			Match:   Match{Path: []string{"VERSION"}},
			Mode:    ModeConfirm,
			Suggest: "just bump",
		}},
	}
	d := EvaluatePath(cfg, "VERSION")
	if d.Allowed || !d.Confirmable || d.ConfirmWindowSeconds != 30 || d.Suggest != "just bump" {
		t.Fatalf("confirm path rule: %+v", d)
	}

	// disable mode is inert.
	cfg.Rules[0].Mode = ModeDisable
	if !EvaluatePath(cfg, "VERSION").Allowed {
		t.Error("mode:disable path rule should not fire")
	}
}

// Command rules and path rules don't cross over.
func TestPathAndCommandRulesDoNotCross(t *testing.T) {
	cfg := &Config{Rules: []Rule{
		{ID: "cmd", Match: Match{Command: CommandPattern{"git", "tag"}}, Message: "x"},
		{ID: "path", Match: Match{Path: []string{"VERSION"}}, Message: "y"},
	}}
	// command eval ignores the path rule; path eval ignores the command rule.
	if EvaluatePath(cfg, "VERSION").Rule.ID != "path" {
		t.Error("EvaluatePath should fire the path rule")
	}
	// A non-VERSION edit is allowed even though a command rule exists.
	if !EvaluatePath(cfg, "main.go").Allowed {
		t.Error("unrelated file edit should be allowed")
	}
}

func TestPathRuleRejectsMixedMatch(t *testing.T) {
	_, err := Parse([]byte("version: 1\nrules:\n  - id: bad\n    match: { path: [VERSION], command: [git, tag] }\n    message: m\n"))
	if err == nil {
		t.Error("a match mixing path and command should be a validation error")
	}
}

func TestPathRuleParses(t *testing.T) {
	cfg, err := Parse([]byte("version: 1\nrules:\n  - id: v\n    match: { path: [VERSION] }\n    message: use versionator\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Rules[0].Match.isPathRule() {
		t.Error("path rule should report isPathRule")
	}
}
