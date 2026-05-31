package rules

import (
	"testing"

	"github.com/benjaminabbitt/llm-tool-killer/internal/ir"
)

// args_none lets a rule carve out read-only exceptions: the rule matches only
// when NONE of the listed tokens are present. e.g. block `git tag` but not the
// read-only `git tag --list`.
func TestArgsNoneExceptions(t *testing.T) {
	cfg := &Config{Rules: []Rule{{
		ID:      "no-git-tag",
		Match:   Match{Command: CommandPattern{"git", "tag"}, ArgsNone: []string{"--list", "-l", "-n"}},
		Message: "releases go through the pipeline",
	}}}

	deny := [][]string{
		{"git", "tag", "v1.2.3"}, // creating a tag → denied
		{"git", "tag"},           // bare → denied
	}
	for _, argv := range deny {
		if Evaluate(cfg, cmd(ir.ShellBash, argv...)).Allowed {
			t.Errorf("%v should be denied", argv)
		}
	}

	allow := [][]string{
		{"git", "tag", "--list"},       // read-only listing → exception
		{"git", "tag", "-l", "v1.*"},   // short form
		{"git", "tag", "-n", "--list"}, // any excepted token present
	}
	for _, argv := range allow {
		if !Evaluate(cfg, cmd(ir.ShellBash, argv...)).Allowed {
			t.Errorf("%v should be allowed (args_none exception)", argv)
		}
	}
}

// args_none on its own (no command) is a valid constraint, and bundled short
// flags are expanded before the exception check.
func TestArgsNoneWithBundledFlags(t *testing.T) {
	cfg := &Config{Rules: []Rule{{
		ID:      "rm-recursive",
		Match:   Match{Command: CommandPattern{"rm", "-r"}, ArgsNone: []string{"-i"}},
		Message: "no recursive delete",
	}}}
	// `rm -ri` bundles -r and -i; the -i exception must fire even though it's
	// clustered with -r.
	if !Evaluate(cfg, cmd(ir.ShellBash, "rm", "-ri", "dir")).Allowed {
		t.Error("rm -ri should be allowed: -i is an args_none exception (bundled)")
	}
	if Evaluate(cfg, cmd(ir.ShellBash, "rm", "-rf", "dir")).Allowed {
		t.Error("rm -rf should be denied: no exception token present")
	}
}

func TestArgsNoneParsesAndCountsAsConstraint(t *testing.T) {
	cfg, err := Parse([]byte("version: 1\nrules:\n  - id: x\n    match: { args_none: [--help] }\n    message: m\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Rules[0].Match.hasConstraint() {
		t.Error("args_none alone should be a valid match constraint")
	}
}
