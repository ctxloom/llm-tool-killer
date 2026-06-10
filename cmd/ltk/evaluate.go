package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/ctxloom/llm-tool-killer/internal/app"
	"github.com/ctxloom/llm-tool-killer/internal/engine"
	"github.com/ctxloom/llm-tool-killer/internal/ir"
	"github.com/ctxloom/llm-tool-killer/internal/rules"
	"github.com/ctxloom/llm-tool-killer/internal/scm"
	"github.com/ctxloom/llm-tool-killer/internal/shellenv"
	"github.com/ctxloom/llm-tool-killer/internal/state"
)

// configSearch lists the default config locations, in order. The .ltk/ layout
// (config + override state together) is preferred; the flat .ltk.yaml is kept
// for back-compat.
var configSearch = []string{
	defaultConfigPath, // .ltk/config.yaml
	legacyConfig,      // .ltk.yaml
	"llm-tool-killer.yaml",
	".llm-tool-killer.yaml",
	".config/llm-tool-killer.yaml",
}

func newEvaluateCmd() *cobra.Command {
	var cfgPath, shellName, engineName string
	c := &cobra.Command{
		Use:   "evaluate",
		Short: "Evaluate a hook payload from stdin and emit an allow/deny decision",
		Long: `Evaluate reads a hook payload (JSON) on stdin and emits an allow/deny decision.

It handles both kinds of payload the editing agent sends:

  • a shell command (tool_input.command) — parsed and matched against command
    rules, in the shell the tool implies (or --shell to force one).
  • a file edit (tool_input.file_path) — matched against file rules (match.path):
    globs, directory subtrees (a trailing slash, e.g. vendor/), and the
    "@submodules" sentinel, which is resolved against this repo's .gitmodules so
    one rule blocks edits inside every submodule.

A denial is written in the engine's format (for Claude Code, a permissionDecision
on stdout, exit 0); an allow writes nothing. Intended to be run by the hook, not
by hand.`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runEvaluate(engineName, cfgPath, ir.Shell(shellName))
		},
	}
	c.Flags().StringVar(&cfgPath, "config", "", "path to rules YAML (default: search cwd)")
	c.Flags().StringVar(&shellName, "shell", "", "force a shell dialect, overriding the engine's tool-derived shell")
	c.Flags().StringVar(&engineName, "engine", "claude-code", "hook engine adapter")
	return c
}

// runEvaluate is the process edge around evaluate: it feeds it stdin, writes
// the resulting streams, and exits non-zero when the engine's protocol demands
// it (a zero exit is a normal return, so deferred cleanup runs).
func runEvaluate(engineName, cfgPath string, forceShell ir.Shell) error {
	out, err := evaluate(engineName, cfgPath, forceShell, os.Stdin)
	if err != nil {
		return err
	}
	if len(out.Stdout) > 0 {
		if _, err := os.Stdout.Write(out.Stdout); err != nil {
			return fmt.Errorf("write decision: %w", err)
		}
	}
	if len(out.Stderr) > 0 {
		os.Stderr.Write(out.Stderr)
	}
	if out.ExitCode != 0 {
		os.Exit(out.ExitCode)
	}
	return nil
}

// evaluate runs the full decision path — decode the hook payload, parse the
// command, match the rules, encode the engine's wire output — with no process
// concerns, so it is testable end to end.
func evaluate(engineName, cfgPath string, forceShell ir.Shell, stdin io.Reader) (engine.Output, error) {
	adapter, err := engine.Get(engineName)
	if err != nil {
		return engine.Output{}, err
	}
	cfg, resolved, err := loadConfig(cfgPath)
	if err != nil {
		return engine.Output{}, err
	}
	// Resolve the `@submodules` path sentinel against this repo's .gitmodules, so
	// a rule can block edits inside every submodule without naming them.
	if wd, err := os.Getwd(); err == nil {
		cfg.ExpandSubmodules(scm.SubmodulePaths(afero.NewOsFs(), wd))
	}
	input, err := io.ReadAll(stdin)
	if err != nil {
		return engine.Output{}, fmt.Errorf("read stdin: %w", err)
	}
	req, err := adapter.Decode(input)
	if err != nil {
		return engine.Output{}, fmt.Errorf("decode hook input: %w", err)
	}

	a := app.New(cfg)
	a.ForceShell = forceShell
	a.HostShell = shellenv.FromEnv(os.Getenv("SHELL"))
	resp := a.Decide(context.Background(), req)

	// A denial may be lifted by "confirm by repeating" only when the rule that
	// fired allows it (inviolate rules report Confirmable=false and never reach
	// here, so repeating them never helps).
	if !resp.Allow && resp.Confirmable && resp.ConfirmWindowSeconds > 0 {
		// The override is keyed on what the agent repeats: the command, or the
		// file path for a file-edit rule.
		key := req.Command
		if req.FilePath != "" {
			key = "edit:" + req.FilePath
		}
		resp = confirmByRepeat(resp, key, statePath(resolved),
			time.Duration(resp.ConfirmDelaySeconds)*time.Second,
			time.Duration(resp.ConfirmWindowSeconds)*time.Second)
	}

	out, err := adapter.Encode(resp)
	if err != nil {
		return engine.Output{}, fmt.Errorf("encode decision: %w", err)
	}
	return out, nil
}

// confirmByRepeat applies the "run it again to permit" override (state.ConfirmByRepeat)
// using the wall clock, and notes on stderr when a repeat was honored. The logic
// lives in internal/state so the acceptance suite can exercise it too.
func confirmByRepeat(resp engine.Response, command, stateFile string, delay, window time.Duration) engine.Response {
	out, overridden := state.ConfirmByRepeat(afero.NewOsFs(), resp, command, stateFile, time.Now(), delay, window)
	if overridden {
		fmt.Fprintln(os.Stderr, progName+": command repeated within the override window — allowing.")
	}
	return out
}

// statePath puts the override state next to the resolved config when that
// config lives in a .ltk directory; otherwise (legacy flat configs, custom
// paths, or no config at all) it falls back to .ltk/state.json in the cwd.
// Runtime state always lives inside a .ltk directory, never loose in the
// project root, so .gitignore's ".ltk/state.json" entry covers it.
func statePath(configPath string) string {
	if dir := filepath.Dir(configPath); filepath.Base(dir) == configDir {
		return filepath.Join(dir, stateBase)
	}
	return filepath.Join(configDir, stateBase)
}

// loadConfig loads the given path, or searches the default locations, returning
// the resolved path (empty when falling back to the built-in allow-all config).
func loadConfig(path string) (*rules.Config, string, error) {
	if path != "" {
		c, err := rules.Load(path)
		return c, path, err
	}
	for _, candidate := range configSearch {
		if _, err := os.Stat(candidate); err == nil {
			c, err := rules.Load(candidate)
			return c, candidate, err
		}
	}
	c, err := rules.Parse([]byte("version: 1\nrules: []\n"))
	return c, "", err
}
