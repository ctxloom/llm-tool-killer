// Package cmd is the Windows cmd.exe (batch) frontend. The intended
// implementation is a small custom lexer for cmd's command grammar (& && || |
// ( ) with ^-escaping and %VAR% expansion), since no AST parser exists for it.
//
// This is currently a stub: it reports the command as opaque and returns
// ErrNotImplemented so callers apply their on_parse_error pass-through policy.
package cmd

import (
	"context"

	"github.com/abbitt/llm-tool-killer/internal/frontend"
	"github.com/abbitt/llm-tool-killer/internal/ir"
)

// Frontend is the (stub) cmd.exe frontend.
type Frontend struct{}

// New returns a cmd Frontend.
func New() *Frontend { return &Frontend{} }

// Shells reports the dialects handled by this frontend.
func (f *Frontend) Shells() []ir.Shell { return []ir.Shell{ir.ShellCmd} }

// Parse is not yet implemented; it returns an opaque, unparsed script.
func (f *Frontend) Parse(_ context.Context, shell ir.Shell, _ string) (*ir.Script, error) {
	return &ir.Script{Shell: shell, Flags: ir.OpacityFlags{Unparsed: true}}, frontend.ErrNotImplemented
}
