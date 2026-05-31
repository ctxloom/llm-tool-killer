package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/abbitt/llm-tool-killer/internal/app"
	"github.com/abbitt/llm-tool-killer/internal/engine"
	"github.com/abbitt/llm-tool-killer/internal/ir"
	"github.com/abbitt/llm-tool-killer/internal/rules"
	"github.com/abbitt/llm-tool-killer/internal/shellenv"
)

// configSearch lists the default config locations, in order.
var configSearch = []string{
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
	cfg, err := loadConfig(cfgPath)
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

// loadConfig loads the given path, or searches default locations. If no config
// is found and none was requested, it returns an empty allow-all config.
func loadConfig(path string) (*rules.Config, error) {
	if path != "" {
		return rules.Load(path)
	}
	for _, candidate := range configSearch {
		if _, err := os.Stat(candidate); err == nil {
			return rules.Load(candidate)
		}
	}
	return rules.Parse([]byte("version: 1\nrules: []\n"))
}
