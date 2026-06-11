package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	claudecli "github.com/ctxloom/claude"

	"github.com/ctxloom/llm-tool-killer/internal/ir"
)

// ClaudeCode adapts the Claude Code PreToolUse hook protocol. The wire types
// — stdin payload, decision JSON — live in the shared github.com/ctxloom/claude
// module (the org's single source of truth for the contract); this adapter
// only maps them onto ltk's Request/Response.
//
// A denial is a hookSpecificOutput object with permissionDecision "deny" on
// stdout. An allow writes nothing and exits 0, letting the normal permission
// flow proceed.
type ClaudeCode struct{}

// Name returns the engine identifier.
func (ClaudeCode) Name() string { return "claude-code" }

// Decode extracts the command from a PreToolUse payload and resolves the shell
// from the tool name. The tool the LLM chose is the authoritative shell signal:
// Claude Code's Bash tool runs in Git Bash (bash) on every platform, and its
// opt-in PowerShell tool spawns pwsh directly.
func (ClaudeCode) Decode(input []byte) (Request, error) {
	p, err := claudecli.DecodeHookPayload(input)
	if err != nil {
		return Request{}, err
	}
	return Request{
		ToolName: p.ToolName,
		Command:  p.ToolInput.Command,
		Shell:    ccShellForTool(p.ToolName),
		FilePath: p.ToolInput.FilePath,
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

// Encode renders a denial as a PreToolUse permission decision on stdout with
// exit 0. An allow produces a zero Output: no bytes, exit 0, which lets Claude
// Code's normal permission flow proceed (it does NOT auto-approve).
func (ClaudeCode) Encode(resp Response) (Output, error) {
	if resp.Allow {
		return Output{}, nil
	}
	body, err := claudecli.EncodeDeny(resp.Message())
	if err != nil {
		return Output{}, err
	}
	return Output{Stdout: body, ExitCode: 0}, nil
}

// --- management surface (Claude Code specific) ---

// claudeMatcher is the PreToolUse matcher: the shell-bound tools (command rules)
// plus the file-editing tools (path rules).
const claudeMatcher = "Bash|PowerShell|Edit|Write|MultiEdit|NotebookEdit"

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
// The path conventions come from the claude module (the org's single source
// of truth for Claude Code's on-disk layout).
func (ClaudeCode) SettingsPath(dir string, global bool) (string, error) {
	if global {
		return claudecli.GlobalSettingsPath()
	}
	return claudecli.ProjectSettingsPath(dir), nil
}

// HookCommand runs the evaluate subcommand, optionally with a rules file.
func (ClaudeCode) HookCommand(bin, configPath string) string {
	cmd := bin + " evaluate"
	if configPath != "" {
		cmd += " --config " + quotePathIfNeeded(configPath)
	}
	return cmd
}

// quotePathIfNeeded double-quotes a path containing whitespace so it survives
// the hook host's shell split. Double quotes (not single) so env references
// like ${CLAUDE_PROJECT_DIR} keep expanding; plain paths pass through
// unquoted, keeping existing installed hook commands byte-stable.
func quotePathIfNeeded(p string) string {
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}

// Install merges a PreToolUse hook running command into the settings JSON.
func (ClaudeCode) Install(settings []byte, command string) ([]byte, error) {
	return mergePreToolUseHook(settings, claudeMatcher, command)
}

// Uninstall removes any PreToolUse hook running command from the settings JSON.
func (ClaudeCode) Uninstall(settings []byte, command string) ([]byte, error) {
	return removePreToolUseHook(settings, command)
}

// mergePreToolUseHook adds a PreToolUse command hook to a settings document
// without disturbing other settings. Idempotent. Shared across engines:
// Antigravity adopted Claude Code's nested registration shape
// (hooks.PreToolUse[].matcher + hooks[].{type,command}) verbatim, so one merge
// covers both — only the matcher vocabulary differs per engine.
func mergePreToolUseHook(existing []byte, matcher, command string) ([]byte, error) {
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

// removePreToolUseHook removes PreToolUse inner hooks running command, pruning
// entries/keys that become empty. Idempotent.
func removePreToolUseHook(existing []byte, command string) ([]byte, error) {
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
