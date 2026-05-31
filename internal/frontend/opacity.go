package frontend

import (
	"slices"
	"strings"

	"github.com/benjaminabbitt/llm-tool-killer/internal/ir"
)

// WrapperSpec describes programs that run another command behind a flag — shell
// wrappers like {bash, [-c]}, {cmd, [/c, /k]}, {pwsh, [-command]}. Programs and
// flags are compared case-insensitively, so callers pass them lowercased.
type WrapperSpec struct {
	Programs []string
	Flags    []string
}

// ApplyOpacity sets opacity flags for a single command: HasEval when its program
// is one of evalPrograms, and Wrapper when it matches any WrapperSpec (the
// program plus one of that spec's flags). All comparisons are case-insensitive;
// evalPrograms and the WrapperSpec fields must be lowercase.
//
// Each shell frontend supplies its own program/flag tables; the detection logic
// lives here so it is written once.
func ApplyOpacity(flags *ir.OpacityFlags, sc ir.SimpleCommand, evalPrograms []string, wrappers ...WrapperSpec) {
	prog := strings.ToLower(sc.Program())
	if slices.Contains(evalPrograms, prog) {
		flags.HasEval = true
	}
	for _, w := range wrappers {
		if !slices.Contains(w.Programs, prog) {
			continue
		}
		for _, a := range sc.Args() {
			if slices.Contains(w.Flags, strings.ToLower(a)) {
				flags.Wrapper = true
				break
			}
		}
	}
}
