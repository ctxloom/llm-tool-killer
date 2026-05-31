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
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runEvaluate(engineName, cfgPath, ir.Shell(shellName))
		},
	}
	c.Flags().StringVar(&cfgPath, "config", "", "path to rules YAML (default: search cwd)")
	c.Flags().StringVar(&shellName, "shell", "", "force a shell dialect, overriding the engine's tool-derived shell")
	c.Flags().StringVar(&engineName, "engine", "claude-code", "hook engine adapter")
	return c
}

func runEvaluate(engineName, cfgPath string, forceShell ir.Shell) error {
	adapter, err := engine.Get(engineName)
	if err != nil {
		return err
	}
	cfg, resolved, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	req, err := adapter.Decode(input)
	if err != nil {
		return fmt.Errorf("decode hook input: %w", err)
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
			time.Duration(resp.ConfirmWindowSeconds)*time.Second)
	}

	out, err := adapter.Encode(resp)
	if err != nil {
		return fmt.Errorf("encode decision: %w", err)
	}
	if len(out.Stdout) > 0 {
		os.Stdout.Write(out.Stdout)
	}
	if len(out.Stderr) > 0 {
		os.Stderr.Write(out.Stderr)
	}
	os.Exit(out.ExitCode)
	return nil
}

// confirmByRepeat applies the "run it again to permit" override (state.ConfirmByRepeat)
// using the wall clock, and notes on stderr when a repeat was honored. The logic
// lives in internal/state so the acceptance suite can exercise it too.
func confirmByRepeat(resp engine.Response, command, stateFile string, window time.Duration) engine.Response {
	out, overridden := state.ConfirmByRepeat(afero.NewOsFs(), resp, command, stateFile, time.Now(), window)
	if overridden {
		fmt.Fprintln(os.Stderr, progName+": command repeated within the override window — allowing.")
	}
	return out
}

// statePath puts the override state next to the resolved config (e.g. in .ltk/),
// or under .ltk/ in the cwd when the config location is unknown.
func statePath(configPath string) string {
	if configPath == "" {
		return filepath.Join(configDir, stateBase)
	}
	return filepath.Join(filepath.Dir(configPath), stateBase)
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
