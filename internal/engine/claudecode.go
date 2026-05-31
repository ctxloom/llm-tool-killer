package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ctxloom/llm-tool-killer/internal/ir"
)

// ClaudeCode adapts the Claude Code PreToolUse hook protocol.
//
// Input (stdin) is the hook payload; we read tool_name and tool_input.command.
// Output (stdout) for a denial is a hookSpecificOutput object with
// permissionDecision "deny". An allow writes nothing and exits 0, letting the
// normal permission flow proceed.
type ClaudeCode struct{}

// Name returns the engine identifier.
func (ClaudeCode) Name() string { return "claude-code" }

type ccInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

// Decode extracts the command from a PreToolUse payload and resolves the shell
// from the tool name. The tool the LLM chose is the authoritative shell signal:
// Claude Code's Bash tool runs in Git Bash (bash) on every platform, and its
// opt-in PowerShell tool spawns pwsh directly.
func (ClaudeCode) Decode(input []byte) (Request, error) {
	var in ccInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Request{}, err
	}
	return Request{
		ToolName: in.ToolName,
		Command:  in.ToolInput.Command,
		Shell:    ccShellForTool(in.ToolName),
	}, nil
}

// ccShellForTool maps a Claude Code tool name to a shell hint. It only returns
// a value when the tool authoritatively names the dialect:
//
//   - PowerShell tool → pwsh (unambiguous).
//   - Bash tool → "" (deferred). The Bash tool runs in the user's system
//     default shell, which may be bash OR zsh; the adapter can't tell from the
//     payload. Returning "" lets the resolver apply defaults.shell (e.g. zsh)
//     before falling back to bash, instead of wrongly asserting bash here.
//   - any other / unknown tool → "".
func ccShellForTool(tool string) ir.Shell {
	switch {
	case strings.Contains(strings.ToLower(tool), "powershell"),
		strings.Contains(strings.ToLower(tool), "pwsh"):
		return ir.ShellPwsh
	default:
		return ""
	}
}

type ccOutput struct {
	HookSpecificOutput ccHookOutput `json:"hookSpecificOutput"`
}

type ccHookOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// Encode renders a denial as a PreToolUse permission decision on stdout with
// exit 0. An allow produces a zero Output: no bytes, exit 0, which lets Claude
// Code's normal permission flow proceed (it does NOT auto-approve).
func (ClaudeCode) Encode(resp Response) (Output, error) {
	if resp.Allow {
		return Output{}, nil
	}
	body, err := json.Marshal(ccOutput{HookSpecificOutput: ccHookOutput{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: resp.Message(),
	}})
	if err != nil {
		return Output{}, err
	}
	return Output{Stdout: body, ExitCode: 0}, nil
}

// --- management surface (Claude Code specific) ---

// claudeMatcher is the PreToolUse matcher: the shell-bound tools.
const claudeMatcher = "Bash|PowerShell"

// Claude Code settings.json keys, used when merging/removing the hook in the
// untyped JSON document. Kept as named constants so the merge and remove paths
// (and the read-back checks) can never disagree on a key spelling.
const (
	keyHooks      = "hooks"
	keyPreToolUse = "PreToolUse"
	keyMatcher    = "matcher"
	keyType       = "type"
	keyCommand    = "command"
	typeCommand   = "command" // value of the "type" field for a command hook
)

// Detect scores Claude Code's relevance by the presence of a .claude directory.
func (ClaudeCode) Detect(dir string) int {
	if fi, err := os.Stat(filepath.Join(dir, ".claude")); err == nil && fi.IsDir() {
		return 2
	}
	return 0
}

// SettingsPath is .claude/settings.json (project) or ~/.claude/settings.json.
func (ClaudeCode) SettingsPath(dir string, global bool) (string, error) {
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	}
	return filepath.Join(dir, ".claude", "settings.json"), nil
}

// HookCommand runs the evaluate subcommand, optionally with a rules file.
func (ClaudeCode) HookCommand(bin, configPath string) string {
	cmd := bin + " evaluate"
	if configPath != "" {
		cmd += " --config " + configPath
	}
	return cmd
}

// Install merges a PreToolUse hook running command into the settings JSON.
func (ClaudeCode) Install(settings []byte, command string) ([]byte, error) {
	return claudeMergeHook(settings, claudeMatcher, command)
}

// Uninstall removes any PreToolUse hook running command from the settings JSON.
func (ClaudeCode) Uninstall(settings []byte, command string) ([]byte, error) {
	return claudeRemoveHook(settings, command)
}

// claudeMergeHook adds a PreToolUse command hook to a Claude settings document
// without disturbing other settings. Idempotent.
func claudeMergeHook(existing []byte, matcher, command string) ([]byte, error) {
	settings, err := decodeSettings(existing)
	if err != nil {
		return nil, err
	}
	hooks, err := childMap(settings, keyHooks)
	if err != nil {
		return nil, err
	}
	pre, err := childSlice(hooks, keyPreToolUse)
	if err != nil {
		return nil, err
	}
	if !preContainsCommand(pre, command) {
		pre = append(pre, map[string]any{
			keyMatcher: matcher,
			keyHooks: []any{
				map[string]any{keyType: typeCommand, keyCommand: command},
			},
		})
	}
	hooks[keyPreToolUse] = pre
	settings[keyHooks] = hooks
	return renderJSON(settings)
}

// claudeRemoveHook removes PreToolUse inner hooks running command, pruning
// entries/keys that become empty. Idempotent.
func claudeRemoveHook(existing []byte, command string) ([]byte, error) {
	settings, err := decodeSettings(existing)
	if err != nil {
		return nil, err
	}
	hooks, ok := settings[keyHooks].(map[string]any)
	if !ok {
		return renderJSON(settings)
	}
	pre, ok := hooks[keyPreToolUse].([]any)
	if !ok {
		return renderJSON(settings)
	}
	kept := removeCommandEntries(pre, command)
	if len(kept) == 0 {
		delete(hooks, keyPreToolUse)
	} else {
		hooks[keyPreToolUse] = kept
	}
	if len(hooks) == 0 {
		delete(settings, keyHooks)
	} else {
		settings[keyHooks] = hooks
	}
	return renderJSON(settings)
}

// removeCommandEntries drops inner hooks running command from each PreToolUse
// entry, and drops any entry left with no hooks.
func removeCommandEntries(pre []any, command string) []any {
	var kept []any
	for _, e := range pre {
		em, ok := e.(map[string]any)
		if !ok {
			kept = append(kept, e)
			continue
		}
		hs, ok := em[keyHooks].([]any)
		if !ok {
			kept = append(kept, e)
			continue
		}
		keptHooks := filterOutCommand(hs, command)
		if len(keptHooks) == 0 {
			continue // entry had only our hook → drop it
		}
		em[keyHooks] = keptHooks
		kept = append(kept, em)
	}
	return kept
}

// filterOutCommand returns the hooks whose command is not command.
func filterOutCommand(hooks []any, command string) []any {
	var kept []any
	for _, h := range hooks {
		if hm, ok := h.(map[string]any); ok {
			if c, _ := hm[keyCommand].(string); c == command {
				continue
			}
		}
		kept = append(kept, h)
	}
	return kept
}

func decodeSettings(existing []byte) (map[string]any, error) {
	settings := map[string]any{}
	if trimmed := bytes.TrimSpace(existing); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &settings); err != nil {
			return nil, fmt.Errorf("parse existing settings: %w", err)
		}
	}
	return settings, nil
}

func renderJSON(m map[string]any) ([]byte, error) {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func childMap(m map[string]any, key string) (map[string]any, error) {
	switch v := m[key].(type) {
	case nil:
		nm := map[string]any{}
		m[key] = nm
		return nm, nil
	case map[string]any:
		return v, nil
	default:
		return nil, fmt.Errorf("settings field %q is not an object", key)
	}
}

func childSlice(m map[string]any, key string) ([]any, error) {
	switch v := m[key].(type) {
	case nil:
		return []any{}, nil
	case []any:
		return v, nil
	default:
		return nil, fmt.Errorf("settings field %q is not an array", key)
	}
}

func preContainsCommand(pre []any, command string) bool {
	for _, e := range pre {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		hs, ok := em[keyHooks].([]any)
		if !ok {
			continue
		}
		for _, h := range hs {
			if hm, ok := h.(map[string]any); ok {
				if c, _ := hm[keyCommand].(string); c == command {
					return true
				}
			}
		}
	}
	return false
}
