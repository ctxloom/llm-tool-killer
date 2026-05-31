package pwsh

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/ctxloom/llm-tool-killer/internal/ir"
)

// fake builds a Frontend whose runner returns canned parser JSON.
func fake(json string) *Frontend {
	return &Frontend{run: func(context.Context, string) ([]byte, error) {
		return []byte(json), nil
	}}
}

func parse(t *testing.T, f *Frontend, src string) *ir.Script {
	t.Helper()
	s, err := f.Parse(context.Background(), ir.ShellPwsh, src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

func TestLowerSimpleCommand(t *testing.T) {
	f := fake(`{"commands":[{"argv":[{"k":"lit","v":"go"},{"k":"lit","v":"test"}]}],"hasErrors":false}`)
	s := parse(t, f, "go test")
	if len(s.Pipelines) != 1 {
		t.Fatalf("want 1 command, got %d", len(s.Pipelines))
	}
	c := s.Pipelines[0].Commands[0]
	if c.Program() != "go" || len(c.Argv) != 2 || c.Argv[1] != "test" {
		t.Errorf("argv = %v", c.Argv)
	}
}

func TestLowerFlattensNestedCommands(t *testing.T) {
	// FindAll returns every CommandAst flat; e.g. Write-Host $(Remove-Item x).
	f := fake(`{"commands":[
		{"argv":[{"k":"lit","v":"Write-Host"},{"k":"dyn","v":"$(Remove-Item x)"}]},
		{"argv":[{"k":"lit","v":"Remove-Item"},{"k":"lit","v":"x"}]}
	],"hasErrors":false}`)
	s := parse(t, f, "Write-Host $(Remove-Item x)")
	var progs []string
	for _, c := range s.Commands() {
		progs = append(progs, c.Program())
	}
	if len(progs) != 2 || progs[1] != "Remove-Item" {
		t.Errorf("programs = %v, want [Write-Host Remove-Item]", progs)
	}
}

func TestRunnerErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	f := &Frontend{run: func(context.Context, string) ([]byte, error) { return nil, want }}
	s, err := f.Parse(context.Background(), ir.ShellPwsh, "x")
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want boom", err)
	}
	if s == nil {
		t.Error("on runner error want a non-nil script")
	}
}

func TestShells(t *testing.T) {
	if got := New().Shells(); len(got) != 1 || got[0] != ir.ShellPwsh {
		t.Errorf("Shells() = %v", got)
	}
}

// TestIntegrationRealParser exercises the actual PowerShell parser when present.
func TestIntegrationRealParser(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		if _, err := exec.LookPath("powershell"); err != nil {
			t.Skip("no PowerShell on PATH")
		}
	}
	s, err := New().Parse(context.Background(), ir.ShellPwsh, "Get-ChildItem -Path . -Recurse")
	if err != nil {
		t.Fatalf("real parse: %v", err)
	}
	cmds := s.Commands()
	if len(cmds) == 0 || cmds[0].Program() != "Get-ChildItem" {
		t.Fatalf("commands = %+v", cmds)
	}
}
