package frontend

import (
	"context"
	"strings"
	"testing"

	"github.com/ctxloom/llm-tool-killer/internal/ir"
)

// fakeFrontend parses by recording the (shell, src) it was given and returning a
// single-command script whose program is the first whitespace token of src.
type fakeFrontend struct {
	shells []ir.Shell
	seen   []string // "shell|src" for each Parse call
}

func (f *fakeFrontend) Shells() []ir.Shell { return f.shells }

func (f *fakeFrontend) Parse(_ context.Context, shell ir.Shell, src string) (*ir.Script, error) {
	f.seen = append(f.seen, string(shell)+"|"+src)
	return &ir.Script{
		Shell:     shell,
		Pipelines: []ir.Pipeline{{Commands: []ir.SimpleCommand{{Argv: strings.Fields(src)}}}},
	}, nil
}

// cmdScript builds a one-command script with the given argv.
func cmdScript(shell ir.Shell, argv ...string) *ir.Script {
	return &ir.Script{Shell: shell, Pipelines: []ir.Pipeline{{Commands: []ir.SimpleCommand{{Argv: argv}}}}}
}

func newReg(f Frontend) *Registry {
	r := NewRegistry()
	r.Register(f)
	return r
}

func nestedPrograms(s *ir.Script) []string {
	var out []string
	for _, c := range s.Commands() {
		out = append(out, c.Program())
	}
	return out
}

func TestExpandWrappers_BashDashC(t *testing.T) {
	f := &fakeFrontend{shells: []ir.Shell{ir.ShellBash, ir.ShellSh, ir.ShellZsh, ir.ShellMksh}}
	r := newReg(f)
	// bash -c "go test" — the inner "go test" is a single argv token.
	s := cmdScript(ir.ShellBash, "bash", "-c", "go test")
	r.ExpandWrappers(context.Background(), s)
	if got := nestedPrograms(s); !contains(got, "go") {
		t.Fatalf("inner `go` not surfaced; programs=%v seen=%v", got, f.seen)
	}
}

func TestExpandWrappers_Eval(t *testing.T) {
	f := &fakeFrontend{shells: []ir.Shell{ir.ShellBash, ir.ShellSh, ir.ShellZsh, ir.ShellMksh}}
	r := newReg(f)
	s := cmdScript(ir.ShellBash, "eval", "git", "tag", "v1")
	r.ExpandWrappers(context.Background(), s)
	if got := nestedPrograms(s); !contains(got, "git") {
		t.Fatalf("eval inner not surfaced; programs=%v", got)
	}
}

func TestExpandWrappers_CmdSlashC(t *testing.T) {
	f := &fakeFrontend{shells: []ir.Shell{ir.ShellCmd}}
	r := newReg(f)
	s := cmdScript(ir.ShellCmd, "cmd", "/c", "git tag v1")
	r.ExpandWrappers(context.Background(), s)
	if got := nestedPrograms(s); !contains(got, "git") {
		t.Fatalf("cmd /c inner not surfaced; programs=%v", got)
	}
}

func TestExpandWrappers_NotAWrapper(t *testing.T) {
	f := &fakeFrontend{shells: []ir.Shell{ir.ShellBash, ir.ShellSh, ir.ShellZsh, ir.ShellMksh}}
	r := newReg(f)
	s := cmdScript(ir.ShellBash, "go", "build")
	r.ExpandWrappers(context.Background(), s)
	if len(f.seen) != 0 {
		t.Errorf("non-wrapper should not be re-parsed; seen=%v", f.seen)
	}
}

func TestExpandWrappers_DepthCap(t *testing.T) {
	// A frontend that always reports a nested wrapper would recurse forever
	// without the cap; ensure it terminates.
	f := &recursiveWrapper{}
	r := NewRegistry()
	r.Register(f)
	s := cmdScript(ir.ShellBash, "bash", "-c", "bash -c x")
	r.ExpandWrappers(context.Background(), s) // must return (depth-capped)
}

// recursiveWrapper always returns `bash -c x`, to exercise the depth cap.
type recursiveWrapper struct{}

func (recursiveWrapper) Shells() []ir.Shell {
	return []ir.Shell{ir.ShellBash, ir.ShellSh, ir.ShellZsh, ir.ShellMksh}
}
func (recursiveWrapper) Parse(_ context.Context, shell ir.Shell, _ string) (*ir.Script, error) {
	return cmdScript(shell, "bash", "-c", "bash -c x"), nil
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
