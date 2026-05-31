package shell

import (
	"context"
	"reflect"
	"testing"

	"github.com/abbitt/llm-tool-killer/internal/ir"
)

func parse(t *testing.T, sh ir.Shell, src string) *ir.Script {
	t.Helper()
	s, err := New().Parse(context.Background(), sh, src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	if s == nil {
		t.Fatalf("Parse(%q) returned nil script", src)
	}
	return s
}

// programs lists argv[0] of every command, nested included, in walk order.
func programs(s *ir.Script) []string {
	var out []string
	for _, c := range s.Commands() {
		out = append(out, c.Program())
	}
	return out
}

func TestSimpleCommand(t *testing.T) {
	s := parse(t, ir.ShellBash, "go test ./...")
	if len(s.Pipelines) != 1 || len(s.Pipelines[0].Commands) != 1 {
		t.Fatalf("shape = %d pipelines", len(s.Pipelines))
	}
	c := s.Pipelines[0].Commands[0]
	if !reflect.DeepEqual(c.Argv, []string{"go", "test", "./..."}) {
		t.Errorf("argv = %v", c.Argv)
	}
}

func TestAssignmentPrefix(t *testing.T) {
	s := parse(t, ir.ShellBash, "FOO=bar CGO_ENABLED=0 go build")
	c := s.Pipelines[0].Commands[0]
	want := []ir.Assignment{{Name: "FOO", Value: "bar"}, {Name: "CGO_ENABLED", Value: "0"}}
	if !reflect.DeepEqual(c.Assignments, want) {
		t.Errorf("assignments = %+v", c.Assignments)
	}
	if c.Program() != "go" {
		t.Errorf("program = %q, want go", c.Program())
	}
}

func TestConnectors(t *testing.T) {
	s := parse(t, ir.ShellBash, "a && b || c ; d")
	if got := programs(s); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("programs = %v", got)
	}
	var conns []ir.Connector
	for _, p := range s.Pipelines {
		conns = append(conns, p.Connector)
	}
	want := []ir.Connector{ir.ConnNone, ir.ConnAnd, ir.ConnOr, ir.ConnSeq}
	if !reflect.DeepEqual(conns, want) {
		t.Errorf("connectors = %v, want %v", conns, want)
	}
}

func TestPipeline(t *testing.T) {
	s := parse(t, ir.ShellBash, "cat x | grep y | wc -l")
	if len(s.Pipelines) != 1 {
		t.Fatalf("want 1 pipeline, got %d", len(s.Pipelines))
	}
	cmds := s.Pipelines[0].Commands
	if len(cmds) != 3 {
		t.Fatalf("want 3 piped commands, got %d", len(cmds))
	}
	got := []string{cmds[0].Program(), cmds[1].Program(), cmds[2].Program()}
	if !reflect.DeepEqual(got, []string{"cat", "grep", "wc"}) {
		t.Errorf("piped programs = %v", got)
	}
}

func TestCommandSubstitutionNested(t *testing.T) {
	s := parse(t, ir.ShellBash, "echo $(go build ./...)")
	if got := programs(s); !reflect.DeepEqual(got, []string{"echo", "go"}) {
		t.Fatalf("programs = %v, want [echo go]", got)
	}
	if !s.Flags.DynamicExpansion {
		t.Error("command substitution should set DynamicExpansion")
	}
}

func TestBacktickSubstitutionNested(t *testing.T) {
	s := parse(t, ir.ShellBash, "echo `go build`")
	if got := programs(s); !reflect.DeepEqual(got, []string{"echo", "go"}) {
		t.Fatalf("programs = %v, want [echo go]", got)
	}
	if !s.Flags.DynamicExpansion {
		t.Error("backtick substitution should set DynamicExpansion")
	}
}

func TestProcessSubstitutionNested(t *testing.T) {
	s := parse(t, ir.ShellBash, "diff <(go test) f")
	found := false
	for _, p := range programs(s) {
		if p == "go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected `go` inside process substitution, programs = %v", programs(s))
	}
}

func TestBackgroundAndSequence(t *testing.T) {
	s := parse(t, ir.ShellBash, "go test & echo done")
	if got := programs(s); !reflect.DeepEqual(got, []string{"go", "echo"}) {
		t.Errorf("programs = %v, want [go echo]", got)
	}
	if !s.Pipelines[0].Background {
		t.Error("first pipeline should be marked Background")
	}
}

func TestSubshell(t *testing.T) {
	s := parse(t, ir.ShellBash, "(cd build && go test)")
	if got := programs(s); !reflect.DeepEqual(got, []string{"cd", "go"}) {
		t.Errorf("programs = %v", got)
	}
}

func TestCompoundCommandFindsInnerCommands(t *testing.T) {
	s := parse(t, ir.ShellBash, "if true; then go test; fi")
	found := false
	for _, p := range programs(s) {
		if p == "go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected to find `go` inside if-clause, programs = %v", programs(s))
	}
}

func TestEvalFlag(t *testing.T) {
	s := parse(t, ir.ShellBash, `eval "go test"`)
	if !s.Flags.HasEval {
		t.Error("eval should set HasEval")
	}
}

func TestWrapperFlag(t *testing.T) {
	s := parse(t, ir.ShellBash, `bash -c "go test"`)
	if !s.Flags.Wrapper {
		t.Error("bash -c should set Wrapper")
	}
}

func TestDynamicExpansionFlag(t *testing.T) {
	s := parse(t, ir.ShellBash, "go $SUBCMD")
	if !s.Flags.DynamicExpansion {
		t.Error("$VAR should set DynamicExpansion")
	}
}

func TestRedirect(t *testing.T) {
	s := parse(t, ir.ShellBash, "go test > out.txt")
	c := s.Pipelines[0].Commands[0]
	if len(c.Redirects) != 1 || c.Redirects[0].Target != "out.txt" {
		t.Fatalf("redirects = %+v", c.Redirects)
	}
}

func TestShellsCovered(t *testing.T) {
	got := New().Shells()
	want := []ir.Shell{ir.ShellSh, ir.ShellBash, ir.ShellZsh, ir.ShellMksh}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Shells() = %v, want %v", got, want)
	}
}

func TestParseErrorReturnsFlaggedScript(t *testing.T) {
	s, err := New().Parse(context.Background(), ir.ShellBash, "for do done $(")
	if err == nil {
		t.Skip("input parsed without error; nothing to assert")
	}
	if s == nil || !s.Flags.Unparsed {
		t.Errorf("on parse error want non-nil script with Unparsed flag, got %+v", s)
	}
}
