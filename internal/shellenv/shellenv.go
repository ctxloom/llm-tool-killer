// Package shellenv resolves the host's default shell from the environment. It
// lives in its own package (depended on by app today, by engine adapters later)
// so the dependency never forms an app↔engine cycle.
package shellenv

import (
	"path/filepath"
	"strings"

	"github.com/abbitt/llm-tool-killer/internal/ir"
)

// ShellFromPath maps a shell executable path (e.g. the value of $SHELL,
// "/usr/bin/zsh") to a known Shell, or "" if it isn't one we parse.
func ShellFromPath(shellPath string) ir.Shell {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(shellPath)))
	// Drop a trailing version suffix like "bash5" is uncommon; handle the
	// common interpreter names directly.
	switch name {
	case "bash":
		return ir.ShellBash
	case "zsh":
		return ir.ShellZsh
	case "sh", "dash", "ash", "busybox":
		return ir.ShellSh
	case "ksh", "ksh93", "mksh", "loksh", "oksh", "pdksh":
		return ir.ShellMksh
	case "pwsh", "powershell":
		return ir.ShellPwsh
	default:
		return ""
	}
}

// FromEnv resolves the default shell from a $SHELL value, returning "" when
// it is empty or unrecognized.
func FromEnv(shellValue string) ir.Shell {
	if shellValue == "" {
		return ""
	}
	return ShellFromPath(shellValue)
}
