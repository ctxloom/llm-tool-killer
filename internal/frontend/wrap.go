package frontend

import (
	"context"
	"path"
	"strings"

	"github.com/benjaminabbitt/llm-tool-killer/internal/ir"
	"github.com/benjaminabbitt/llm-tool-killer/internal/shellenv"
)

// maxWrapDepth bounds how deep ExpandWrappers re-parses nested wrappers
// (e.g. `bash -c "bash -c '…'"`), guarding against pathological input.
const maxWrapDepth = 4

// wrapperRule identifies a command that runs another command given inline — a
// trivial way to sneak a denied command past a rule. ExpandWrappers re-parses
// that inner command so rules match the real thing.
type wrapperRule struct {
	programs []string // argv[0] basename (lowercased)
	flag     string   // flag whose argument(s) are the inner command; "" = eval-style (all args)
	joinRest bool     // true: inner = all args after the flag joined; false: the single arg after it
}

// wrapperRules covers the common shell wrappers. Inner-command shell is derived
// from the program name via innerShell.
var wrapperRules = []wrapperRule{
	{programs: []string{"sh", "bash", "zsh", "dash", "ksh", "mksh"}, flag: "-c", joinRest: false},
	{programs: []string{"eval"}, flag: "", joinRest: true},
	{programs: []string{"cmd", "cmd.exe"}, flag: "/c", joinRest: true},
	{programs: []string{"cmd", "cmd.exe"}, flag: "/k", joinRest: true},
	{programs: []string{"pwsh", "powershell", "pwsh.exe", "powershell.exe"}, flag: "-command", joinRest: false},
}

// ExpandWrappers re-parses the inner command of any trivial shell wrapper it
// finds in s (`bash -c "…"`, `eval "…"`, `cmd /c "…"`, `pwsh -Command "…"`) and
// appends it to that command's Nested, so the evaluator (which already walks
// Nested) matches the real command. It recurses into nested scripts up to a
// small depth cap. It mutates s in place.
func (r *Registry) ExpandWrappers(ctx context.Context, s *ir.Script) {
	r.expandWrappers(ctx, s, 0)
}

func (r *Registry) expandWrappers(ctx context.Context, s *ir.Script, depth int) {
	if s == nil || depth >= maxWrapDepth {
		return
	}
	for pi := range s.Pipelines {
		cmds := s.Pipelines[pi].Commands
		for ci := range cmds {
			sc := &cmds[ci]
			if inner, shell, ok := wrappedCommand(sc.Argv, s.Shell); ok && inner != "" {
				if nested, err := r.Parse(ctx, shell, inner); err == nil && nested != nil {
					sc.Nested = append(sc.Nested, nested)
				}
			}
			// Recurse into already-present nested scripts (substitutions) and any
			// just-added wrapper bodies.
			for _, ns := range sc.Nested {
				r.expandWrappers(ctx, ns, depth+1)
			}
		}
	}
}

// wrappedCommand returns the inner command string and the shell it is written
// in, if argv is a recognized wrapper invocation.
func wrappedCommand(argv []string, outer ir.Shell) (string, ir.Shell, bool) {
	if len(argv) == 0 {
		return "", "", false
	}
	prog := strings.ToLower(path.Base(argv[0]))
	args := argv[1:]
	for _, rule := range wrapperRules {
		if !containsFold(rule.programs, prog) {
			continue
		}
		inner, ok := rule.extract(args)
		if !ok {
			continue
		}
		return inner, innerShell(prog, outer), true
	}
	return "", "", false
}

// extract pulls the inner command string out of a wrapper's arguments.
func (rule wrapperRule) extract(args []string) (string, bool) {
	if rule.flag == "" { // eval-style: the command is all the arguments
		if len(args) == 0 {
			return "", false
		}
		return strings.Join(args, " "), true
	}
	for i, a := range args {
		if !strings.EqualFold(a, rule.flag) {
			continue
		}
		rest := args[i+1:]
		if len(rest) == 0 {
			return "", false
		}
		if rule.joinRest {
			return strings.Join(rest, " "), true
		}
		return rest[0], true
	}
	return "", false
}

// innerShell maps a wrapper program to the shell its inner command is written
// in. It reuses shellenv's basename→Shell mapping (the same logic that resolves
// $SHELL), so the dialect table lives in one place; eval and anything shellenv
// doesn't recognize run in the surrounding shell.
func innerShell(prog string, outer ir.Shell) ir.Shell {
	if s := shellenv.ShellFromPath(prog); s != "" {
		return s
	}
	return outer
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
