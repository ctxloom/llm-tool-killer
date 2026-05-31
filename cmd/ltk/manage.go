package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/benjaminabbitt/llm-tool-killer/internal/engine"
	"github.com/benjaminabbitt/llm-tool-killer/internal/rules"
)

func newManageCmd() *cobra.Command {
	m := &cobra.Command{
		Use:   "manage",
		Short: "Install or remove the pre-tool hook in your LLM agent's config",
	}
	m.AddCommand(newInstallCmd(), newUninstallCmd())
	return m
}

// manageFlags are shared by install and uninstall.
type manageFlags struct {
	engineName   string
	settingsPath string
	bin          string
	configPath   string
	global       bool
	printOnly    bool
}

func (f *manageFlags) bind(c *cobra.Command) {
	c.Flags().StringVar(&f.engineName, "engine", "", "force an engine (default: auto-detect the most relevant)")
	c.Flags().StringVar(&f.settingsPath, "settings", "", "explicit settings file path (overrides the engine default)")
	c.Flags().StringVar(&f.bin, "bin", "ltk", "the ltk invocation the hook should run")
	c.Flags().StringVar(&f.configPath, "config", ".ltk.yaml", "rules file the hook should use (\"\" to omit)")
	c.Flags().BoolVar(&f.global, "global", false, "target the user-level config instead of the project")
	c.Flags().BoolVar(&f.printOnly, "print", false, "print the resulting config to stdout instead of writing")
}

// resolve picks the engine and its settings path.
func (f *manageFlags) resolve() (engine.Engine, string, error) {
	var (
		eng engine.Engine
		err error
	)
	if f.engineName != "" {
		if eng, err = engine.Get(f.engineName); err != nil {
			return nil, "", err
		}
	} else {
		var ok bool
		if eng, ok = engine.Detect("."); !ok {
			return nil, "", errors.New("no LLM agent config detected here; pass --engine to choose one")
		}
	}
	path := f.settingsPath
	if path == "" {
		if path, err = eng.SettingsPath(".", f.global); err != nil {
			return nil, "", err
		}
	}
	return eng, path, nil
}

func newInstallCmd() *cobra.Command {
	f := &manageFlags{}
	c := &cobra.Command{
		Use:   "install",
		Short: "Add the pre-tool hook to the most relevant LLM config",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			eng, path, err := f.resolve()
			if err != nil {
				return err
			}
			command := eng.HookCommand(f.bin, f.configPath)
			if f.configPath != "" {
				if err := scaffoldConfig(f.configPath); err != nil {
					return err
				}
			}
			existing, err := readIfExists(path)
			if err != nil {
				return err
			}
			merged, err := eng.Install(existing, command)
			if err != nil {
				return err
			}
			if f.printOnly {
				_, err := os.Stdout.Write(merged)
				return err
			}
			if err := writeFile(path, merged); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ltk: installed hook for %s\n  settings: %s\n  command:  %s\n", eng.Name(), path, command)
			return nil
		},
	}
	f.bind(c)
	return c
}

func newUninstallCmd() *cobra.Command {
	f := &manageFlags{}
	c := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the pre-tool hook from the LLM config",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			eng, path, err := f.resolve()
			if err != nil {
				return err
			}
			command := eng.HookCommand(f.bin, f.configPath)
			existing, err := readIfExists(path)
			if err != nil {
				return err
			}
			if existing == nil {
				fmt.Fprintf(os.Stderr, "ltk: nothing to uninstall (%s not found)\n", path)
				return nil
			}
			updated, err := eng.Uninstall(existing, command)
			if err != nil {
				return err
			}
			if f.printOnly {
				_, err := os.Stdout.Write(updated)
				return err
			}
			if err := writeFile(path, updated); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ltk: removed hook for %s from %s\n", eng.Name(), path)
			return nil
		},
	}
	f.bind(c)
	return c
}

// scaffoldConfig writes a starter rules file if one does not already exist.
func scaffoldConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // leave the user's rules alone
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.WriteFile(path, []byte(rules.StarterConfig), 0o644); err != nil {
		return fmt.Errorf("write starter config %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "ltk: wrote starter rules file %s (edit it to taste)\n", path)
	return nil
}

func readIfExists(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return b, nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
