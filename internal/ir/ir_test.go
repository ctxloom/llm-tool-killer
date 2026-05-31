package ir

import (
	"reflect"
	"testing"
)

// buildNested mirrors `a && b | c $(d)` with d embedded in c's argv.
func buildNested() *Script {
	return &Script{
		Shell: ShellBash,
		Pipelines: []Pipeline{
			{Connector: ConnNone, Commands: []SimpleCommand{{Argv: []string{"a"}}}},
			{
				Connector: ConnAnd,
				Commands: []SimpleCommand{
					{Argv: []string{"b"}},
					{
						Argv: []string{"c"},
						Nested: []*Script{
							{Pipelines: []Pipeline{{Commands: []SimpleCommand{{Argv: []string{"d"}}}}}},
						},
					},
				},
			},
		},
	}
}

func TestCommandsPreOrderIncludesNested(t *testing.T) {
	got := buildNested().Commands()
	var progs []string
	for _, c := range got {
		progs = append(progs, c.Program())
	}
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(progs, want) {
		t.Fatalf("Commands() programs = %v, want %v", progs, want)
	}
}

func TestWalkEarlyStop(t *testing.T) {
	var visited []string
	completed := buildNested().Walk(func(c SimpleCommand) bool {
		visited = append(visited, c.Program())
		return c.Program() != "b" // stop right after visiting b
	})
	if completed {
		t.Fatal("Walk reported completion, want early stop")
	}
	want := []string{"a", "b"}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("visited = %v, want %v", visited, want)
	}
}

func TestProgramAndArgs(t *testing.T) {
	c := SimpleCommand{Argv: []string{"go", "test", "./..."}}
	if c.Program() != "go" {
		t.Errorf("Program() = %q, want go", c.Program())
	}
	if !reflect.DeepEqual(c.Args(), []string{"test", "./..."}) {
		t.Errorf("Args() = %v", c.Args())
	}
	empty := SimpleCommand{}
	if empty.Program() != "" || empty.Args() != nil {
		t.Errorf("empty command: Program=%q Args=%v", empty.Program(), empty.Args())
	}
}

func TestOpacityFlags(t *testing.T) {
	if (OpacityFlags{}).Any() {
		t.Error("zero flags should report Any()=false")
	}
	merged := OpacityFlags{HasEval: true}.Merge(OpacityFlags{Wrapper: true})
	if !merged.HasEval || !merged.Wrapper {
		t.Errorf("Merge lost a flag: %+v", merged)
	}
	if !merged.Any() {
		t.Error("merged flags should report Any()=true")
	}
}

// nil receiver must be safe.
func TestWalkNil(t *testing.T) {
	var s *Script
	if !s.Walk(func(SimpleCommand) bool { return true }) {
		t.Error("nil Walk should report completion")
	}
	if s.Commands() != nil {
		t.Error("nil Commands should be nil")
	}
}
