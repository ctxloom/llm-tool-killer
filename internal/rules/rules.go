// Package rules holds the YAML rule schema, its loader, and the evaluator that
// matches a parsed Script against the rules. The evaluator depends only on the
// IR, so it is shell-agnostic.
package rules

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/abbitt/llm-tool-killer/internal/ir"
)

// Action is the outcome a rule (or default policy) selects.
type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
)

// Config is the top-level YAML document.
type Config struct {
	Version  int      `yaml:"version"`
	Defaults Defaults `yaml:"defaults"`
	Rules    []Rule   `yaml:"rules"`
}

// Defaults control behavior when no rule fires.
type Defaults struct {
	// Shell is the fallback dialect used when neither a --shell override nor the
	// engine adapter could determine one. Empty means "no default" (the resolver
	// then sniffs, finally falling back to bash).
	Shell ir.Shell `yaml:"shell"`
	// OnOpaque decides what to do when a command contains constructs that
	// cannot be statically analyzed (eval, $(...), bash -c, unparsed). Default
	// allow keeps cooperative behavior; set to deny to harden.
	OnOpaque Action `yaml:"on_opaque"`
	// OnParseError decides what to do when the frontend could not parse the
	// command at all. Default allow.
	OnParseError Action `yaml:"on_parse_error"`
}

// Rule is a single match → action mapping.
type Rule struct {
	ID      string `yaml:"id"`
	Match   Match  `yaml:"match"`
	Action  Action `yaml:"action"` // defaults to deny
	Message string `yaml:"message"`
	Suggest string `yaml:"suggest"`
}

func (r Rule) action() Action {
	if r.Action == "" {
		return ActionDeny
	}
	return r.Action
}

// CommandPattern is an argv prefix to match. In YAML it may be written as a
// scalar string (shell-split on whitespace: "sh -c" → ["sh", "-c"]) or as a
// list, taken verbatim (["go", "test"]).
type CommandPattern []string

// UnmarshalYAML accepts either a scalar string or a sequence of strings.
func (c *CommandPattern) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*c = strings.Fields(node.Value)
	case yaml.SequenceNode:
		var toks []string
		if err := node.Decode(&toks); err != nil {
			return err
		}
		*c = toks
	default:
		return fmt.Errorf("match.command: expected a string or list of strings")
	}
	return nil
}

// Match is the set of conditions a command must satisfy. All present conditions
// must hold (AND). An entirely empty Match matches nothing, to avoid accidental
// catch-all denials.
type Match struct {
	// Command matches the program plus arguments. The first token is the
	// program: it matches argv[0] exactly or by basename, so absolute paths
	// still match. Remaining tokens are classified per the command's shell into:
	//
	//   - positional args (e.g. subcommands like `test`, `commit`): order
	//     matters; they must appear as an ordered prefix of the command's
	//     non-option arguments.
	//   - options (any flag: `-c`, `-x`, `--no-cache`, and `/c` under cmd): no
	//     implied order; each must appear somewhere in the command's arguments,
	//     and they never consume a positional slot.
	//
	// Trailing arguments are always allowed. Examples: `go` matches any `go …`;
	// `[go, test]` matches `go test …` (and `go --mod=mod test …`); `sh -c`
	// matches `sh -c …` and `sh -e -c …`; `[git, push, --force, --no-verify]`
	// matches those flags in any order after `push`.
	Command CommandPattern `yaml:"command"`
	// ArgsAny / ArgsAll are program-agnostic refinements: tokens that must
	// appear somewhere in the arguments (beyond the Command prefix).
	ArgsAny []string `yaml:"args_any"` // at least one present in args
	ArgsAll []string `yaml:"args_all"` // all present in args
	// Shells restricts the rule to these shells.
	Shells []ir.Shell `yaml:"shells"`
}

func (m Match) hasConstraint() bool {
	return len(m.Command) > 0 || len(m.ArgsAny) > 0 ||
		len(m.ArgsAll) > 0 || len(m.Shells) > 0
}

func (m Match) matches(shell ir.Shell, c ir.SimpleCommand) bool {
	if !m.hasConstraint() {
		return false
	}
	if len(m.Shells) > 0 && !slices.Contains(m.Shells, shell) {
		return false
	}
	if len(m.Command) > 0 && !matchCommand(m.Command, c.Argv, shell) {
		return false
	}
	args := c.Args()
	for _, a := range m.ArgsAll {
		if !slices.Contains(args, a) {
			return false
		}
	}
	if len(m.ArgsAny) > 0 &&
		!slices.ContainsFunc(m.ArgsAny, func(a string) bool { return slices.Contains(args, a) }) {
		return false
	}
	return true
}

// matchCommand reports whether a command pattern matches a command's argv,
// using the command's shell to classify flags. See Match.Command for the model.
//
//   - pattern[0] (the program) matches argv[0] exactly or by basename.
//   - positional pattern tokens (non-options) must be an ordered prefix of the
//     command's non-option arguments.
//   - option pattern tokens (flags) must each appear somewhere in args, in any
//     order.
func matchCommand(pattern, argv []string, shell ir.Shell) bool {
	if len(pattern) == 0 || len(argv) == 0 {
		return false
	}
	if argv[0] != pattern[0] && path.Base(argv[0]) != pattern[0] {
		return false
	}

	var positionals, options []string
	for _, tok := range pattern[1:] {
		if isOption(tok, shell) {
			options = append(options, tok)
		} else {
			positionals = append(positionals, tok)
		}
	}

	args := argv[1:]
	for _, opt := range options {
		if !slices.Contains(args, opt) {
			return false
		}
	}

	operands := make([]string, 0, len(args))
	for _, a := range args {
		if !isOption(a, shell) {
			operands = append(operands, a)
		}
	}
	if len(positionals) > len(operands) {
		return false
	}
	for i, w := range positionals {
		if operands[i] != w {
			return false
		}
	}
	return true
}

// isOption reports whether a token is an option (a flag) for the given shell, as
// opposed to a positional argument (a POSIX "operand"). The classification is
// shell-aware so rules stay portable:
//
//   - POSIX family (sh/bash/zsh/mksh) and PowerShell: options start with "-"
//     (e.g. -c, -x, --no-cache, -Recurse). A lone "-" is positional (stdin).
//   - cmd.exe: options conventionally start with "/" (e.g. /c, /s); "-" is also
//     accepted. (Note "/" is the switch char in cmd, whereas in POSIX it begins
//     a path, which is why this must be shell-aware.)
func isOption(tok string, shell ir.Shell) bool {
	if tok == "" || tok == "-" {
		return false
	}
	if shell == ir.ShellCmd && strings.HasPrefix(tok, "/") {
		return true
	}
	return strings.HasPrefix(tok, "-")
}

// Parse decodes and validates a config from YAML bytes. Unknown fields are
// rejected so that typos in a rule file surface as errors instead of being
// silently ignored.
func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.normalizeAndValidate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Load reads and parses a config file.
func Load(pathname string) (*Config, error) {
	data, err := os.ReadFile(pathname)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

func (c *Config) normalizeAndValidate() error {
	if c.Defaults.OnOpaque == "" {
		c.Defaults.OnOpaque = ActionAllow
	}
	if c.Defaults.OnParseError == "" {
		c.Defaults.OnParseError = ActionAllow
	}
	if err := validAction(c.Defaults.OnOpaque); err != nil {
		return fmt.Errorf("defaults.on_opaque: %w", err)
	}
	if err := validAction(c.Defaults.OnParseError); err != nil {
		return fmt.Errorf("defaults.on_parse_error: %w", err)
	}
	if c.Defaults.Shell != "" && !c.Defaults.Shell.Valid() {
		return fmt.Errorf("defaults.shell: unknown shell %q", c.Defaults.Shell)
	}
	seen := make(map[string]bool, len(c.Rules))
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.ID == "" {
			return fmt.Errorf("rule #%d: missing id", i)
		}
		if seen[r.ID] {
			return fmt.Errorf("duplicate rule id %q", r.ID)
		}
		seen[r.ID] = true
		if err := validAction(r.action()); err != nil {
			return fmt.Errorf("rule %q: %w", r.ID, err)
		}
		if !r.Match.hasConstraint() {
			return fmt.Errorf("rule %q: match has no conditions", r.ID)
		}
		for _, tok := range r.Match.Command {
			if tok == "" {
				return fmt.Errorf("rule %q: empty token in match.command", r.ID)
			}
		}
		for _, sh := range r.Match.Shells {
			if !sh.Valid() {
				return fmt.Errorf("rule %q: unknown shell %q in match.shells", r.ID, sh)
			}
		}
	}
	return nil
}

func validAction(a Action) error {
	switch a {
	case ActionAllow, ActionDeny:
		return nil
	default:
		return fmt.Errorf("invalid action %q (want allow or deny)", a)
	}
}

func opaqueDesc(f ir.OpacityFlags) string {
	var parts []string
	if f.HasEval {
		parts = append(parts, "eval")
	}
	if f.Wrapper {
		parts = append(parts, "shell -c wrapper")
	}
	if f.DynamicExpansion {
		parts = append(parts, "dynamic expansion")
	}
	if f.Unparsed {
		parts = append(parts, "unparsed construct")
	}
	return strings.Join(parts, ", ")
}
