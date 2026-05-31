package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/benjaminabbitt/llm-tool-killer/internal/app"
	"github.com/benjaminabbitt/llm-tool-killer/internal/engine"
	"github.com/benjaminabbitt/llm-tool-killer/internal/ir"
	"github.com/benjaminabbitt/llm-tool-killer/internal/rules"
	"github.com/benjaminabbitt/llm-tool-killer/internal/shellenv"
	"github.com/benjaminabbitt/llm-tool-killer/internal/state"
)

// configSearch lists the default config locations, in order. The .ltk/ layout
// (config + override state together) is preferred; the flat .ltk.yaml is kept
// for back-compat.
var configSearch = []string{
	".ltk/config.yaml",
	".ltk.yaml",
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

	if !resp.Allow && cfg.Defaults.RepeatWindowSeconds > 0 {
		resp = confirmByRepeat(resp, req.Command, statePath(resolved),
			time.Duration(cfg.Defaults.RepeatWindowSeconds)*time.Second)
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

// confirmByRepeat implements the "run it again to permit" override: a denied
// command is remembered for window, and the next identical command within that
// window is allowed instead. State persists in stateFile across hook calls.
// It is an escape hatch, not a control (see internal/state).
func confirmByRepeat(resp engine.Response, command, stateFile string, window time.Duration) engine.Response {
	now := time.Now()
	key := strings.TrimSpace(command)
	st := state.Open(stateFile)
	if st.Armed(key, now) {
		st.Clear(key)
		_ = st.Save(now)
		fmt.Fprintln(os.Stderr, "ltk: command repeated within the override window — allowing.")
		return engine.Response{Allow: true}
	}
	st.Arm(key, now, window)
	_ = st.Save(now)
	resp.Reason = strings.TrimRight(resp.Reason, "\n") +
		fmt.Sprintf("\n\nIf you really mean it, run the exact same command again within %ds to proceed.", int(window.Seconds()))
	return resp
}

// statePath puts the override state next to the resolved config (e.g. in .ltk/),
// or under .ltk/ in the cwd when the config location is unknown.
func statePath(configPath string) string {
	if configPath == "" {
		return filepath.Join(".ltk", "state.json")
	}
	return filepath.Join(filepath.Dir(configPath), "state.json")
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
