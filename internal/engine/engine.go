// Package engine adapts the various LLM hook protocols (Claude Code, Gemini,
// ...) to a single internal Request/Response shape. The core never speaks a
// specific engine's wire format; an Adapter does the translation at the edge.
package engine

import (
	"fmt"
	"strings"

	"github.com/benjaminabbitt/llm-tool-killer/internal/ir"
)

// Request is the engine-neutral view of a tool invocation to be checked.
type Request struct {
	ToolName string
	Command  string
	// Shell is the dialect to parse with. Empty means the engine did not say;
	// the caller fills in a default.
	Shell ir.Shell
}

// Response is the engine-neutral decision.
type Response struct {
	Allow   bool
	Reason  string
	Suggest string
	// Confirmable reports whether a denial may be lifted by repeating the exact
	// command within ConfirmWindowSeconds (the "confirm by repeating" override).
	// An inviolate rule yields Confirmable=false, so repeating never helps.
	Confirmable          bool
	ConfirmWindowSeconds int
}

// Message renders the reason and suggestion into a single human-facing string.
func (r Response) Message() string {
	switch {
	case r.Suggest == "":
		return r.Reason
	case r.Reason == "":
		return "Use instead: " + r.Suggest
	default:
		return r.Reason + "\n\nUse instead: " + r.Suggest
	}
}

// Output is the full result an adapter produces for one decision: what to write
// to each stream and the process exit code. This covers every engine's protocol
// without changing the interface:
//
//   - Claude Code / Codex: deny → JSON on Stdout, ExitCode 0; allow → empty.
//   - Gemini CLI: deny → reason on Stderr, ExitCode 2; allow → empty.
//
// A zero Output (no bytes, exit 0) means "write nothing" — pass-through.
type Output struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Adapter translates one hook engine's stdin/stdout protocol. It is what the
// `evaluate` (runtime) path needs.
type Adapter interface {
	// Name is the engine identifier (e.g. "claude-code").
	Name() string
	// Decode parses the engine's hook payload from stdin bytes.
	Decode(input []byte) (Request, error)
	// Encode renders the decision into the engine's wire output (streams + exit
	// code).
	Encode(resp Response) (Output, error)
}

// Engine is everything specific to one hook host: the runtime Adapter plus the
// management surface (where its hook config lives and how to add/remove the
// hook). Engine-specific details — settings paths, the on-disk hook format —
// live entirely in the engine's own module, so adding an engine never touches
// the `manage` command.
type Engine interface {
	Adapter
	// Detect scores how relevant this engine is to the project rooted at dir
	// (e.g. presence of .claude/). 0 means "not present here". The manage
	// command installs into the highest-scoring engine.
	Detect(dir string) int
	// SettingsPath returns the engine's hook-config file for the given scope.
	SettingsPath(dir string, global bool) (string, error)
	// HookCommand builds the command line the hook should run.
	HookCommand(bin, configPath string) string
	// Install merges a hook running command into the engine's settings bytes
	// (empty in → fresh config). Idempotent; returns the new settings bytes.
	Install(settings []byte, command string) ([]byte, error)
	// Uninstall removes any hook running command from the settings bytes.
	Uninstall(settings []byte, command string) ([]byte, error)
}

// engines is the registry of known engines.
func engines() []Engine {
	return []Engine{ClaudeCode{}}
}

// Get returns the engine for a name.
func Get(name string) (Engine, error) {
	want := strings.ToLower(name)
	for _, e := range engines() {
		if e.Name() == want || strings.HasPrefix(e.Name(), want) {
			return e, nil
		}
	}
	switch want {
	case "claude-code", "claudecode", "claude":
		return ClaudeCode{}, nil
	default:
		return nil, fmt.Errorf("unknown engine %q", name)
	}
}

// Detect returns the most relevant engine for the project at dir, or ok=false
// if none is detected there.
func Detect(dir string) (Engine, bool) {
	var best Engine
	bestScore := 0
	for _, e := range engines() {
		if s := e.Detect(dir); s > bestScore {
			best, bestScore = e, s
		}
	}
	return best, best != nil
}
