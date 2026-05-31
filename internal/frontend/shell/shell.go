// Package shell implements the Frontend for the POSIX shell family — sh, bash,
// zsh, and mksh — by lowering the mvdan.cc/sh AST into the Command-Graph IR.
//
// Lowering is intentionally lossy: it preserves the command graph (pipelines,
// sequencing, nesting) and the words of each command, but collapses control
// flow. Compound commands (if/for/while/case/functions) are flattened to the
// commands they contain so rules still match programs buried inside them.
package shell

import (
	"context"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/abbitt/llm-tool-killer/internal/ir"
)

// Frontend lowers POSIX-family shell into IR.
type Frontend struct{}

// New returns a shell Frontend.
func New() *Frontend { return &Frontend{} }

// Shells reports the dialects handled by this frontend.
func (f *Frontend) Shells() []ir.Shell {
	return []ir.Shell{ir.ShellSh, ir.ShellBash, ir.ShellZsh, ir.ShellMksh}
}

func variantFor(sh ir.Shell) syntax.LangVariant {
	switch sh {
	case ir.ShellSh:
		return syntax.LangPOSIX
	case ir.ShellMksh:
		return syntax.LangMirBSDKorn
	case ir.ShellZsh:
		return syntax.LangZsh
	default: // bash and anything unexpected
		return syntax.LangBash
	}
}

// Parse lowers src into a Script.
func (f *Frontend) Parse(_ context.Context, shell ir.Shell, src string) (*ir.Script, error) {
	parser := syntax.NewParser(syntax.Variant(variantFor(shell)))
	file, err := parser.Parse(strings.NewReader(src), "")
	if err != nil {
		return &ir.Script{Shell: shell, Flags: ir.OpacityFlags{Unparsed: true}}, err
	}
	l := &lowerer{shell: shell, printer: syntax.NewPrinter()}
	script := &ir.Script{Shell: shell}
	script.Pipelines = l.lowerStmts(file.Stmts)
	script.Flags = l.flags
	return script, nil
}

// lowerer accumulates opacity flags as it walks the AST.
type lowerer struct {
	shell   ir.Shell
	printer *syntax.Printer
	flags   ir.OpacityFlags
}

func (l *lowerer) lowerStmts(stmts []*syntax.Stmt) []ir.Pipeline {
	var out []ir.Pipeline
	for i, st := range stmts {
		conn := ir.ConnSeq
		if i == 0 {
			conn = ir.ConnNone
		}
		out = append(out, l.lowerStmt(st, conn)...)
	}
	return out
}

func (l *lowerer) lowerStmt(st *syntax.Stmt, conn ir.Connector) []ir.Pipeline {
	if st == nil {
		return nil
	}
	return l.lowerCmd(st.Cmd, st, conn)
}

func (l *lowerer) lowerCmd(cmd syntax.Command, st *syntax.Stmt, conn ir.Connector) []ir.Pipeline {
	switch c := cmd.(type) {
	case *syntax.CallExpr:
		return []ir.Pipeline{{
			Connector:  conn,
			Background: st != nil && st.Background,
			Negated:    st != nil && st.Negated,
			Commands:   []ir.SimpleCommand{l.lowerCall(c, st)},
		}}

	case *syntax.BinaryCmd:
		switch c.Op {
		case syntax.Pipe, syntax.PipeAll:
			return []ir.Pipeline{{
				Connector: conn,
				Negated:   st != nil && st.Negated,
				Commands:  l.flattenPipe(c),
			}}
		case syntax.AndStmt:
			return append(l.lowerStmt(c.X, conn), l.lowerStmt(c.Y, ir.ConnAnd)...)
		case syntax.OrStmt:
			return append(l.lowerStmt(c.X, conn), l.lowerStmt(c.Y, ir.ConnOr)...)
		default:
			return append(l.lowerStmt(c.X, conn), l.lowerStmt(c.Y, ir.ConnSeq)...)
		}

	case *syntax.Block:
		return l.lowerStmts(c.Stmts)

	case *syntax.Subshell:
		return l.lowerStmts(c.Stmts)

	default:
		// Compound command (if/for/while/case/function/...) or a construct we
		// don't model. Walk it to surface every simple command it contains so
		// rules still match, then stop descending into each call we capture —
		// lowerCall already pulls in any nested substitutions.
		var out []ir.Pipeline
		syntax.Walk(cmd, func(n syntax.Node) bool {
			call, ok := n.(*syntax.CallExpr)
			if !ok {
				return true
			}
			out = append(out, ir.Pipeline{
				Connector: ir.ConnSeq,
				Commands:  []ir.SimpleCommand{l.lowerCall(call, nil)},
			})
			return false
		})
		if len(out) == 0 {
			l.flags.Unparsed = true
		}
		return out
	}
}

// flattenPipe collects every command in a "|"/"|&" chain in order.
func (l *lowerer) flattenPipe(c *syntax.BinaryCmd) []ir.SimpleCommand {
	return append(l.pipeSide(c.X), l.pipeSide(c.Y)...)
}

func (l *lowerer) pipeSide(st *syntax.Stmt) []ir.SimpleCommand {
	if st == nil {
		return nil
	}
	if bc, ok := st.Cmd.(*syntax.BinaryCmd); ok && (bc.Op == syntax.Pipe || bc.Op == syntax.PipeAll) {
		return l.flattenPipe(bc)
	}
	var cmds []ir.SimpleCommand
	for _, p := range l.lowerStmt(st, ir.ConnNone) {
		cmds = append(cmds, p.Commands...)
	}
	return cmds
}

func (l *lowerer) lowerCall(c *syntax.CallExpr, st *syntax.Stmt) ir.SimpleCommand {
	sc := ir.SimpleCommand{}
	for _, a := range c.Assigns {
		name := ""
		if a.Name != nil {
			name = a.Name.Value
		}
		sc.Assignments = append(sc.Assignments, ir.Assignment{Name: name, Value: l.word(&sc, a.Value)})
	}
	for _, w := range c.Args {
		sc.Argv = append(sc.Argv, l.word(&sc, w))
	}
	if st != nil {
		for _, r := range st.Redirs {
			sc.Redirects = append(sc.Redirects, ir.Redirect{
				Op:     r.Op.String(),
				Target: l.word(&sc, r.Word),
			})
		}
	}
	sc.Raw = l.render(c)
	l.detectOpacity(sc)
	return sc
}

// detectOpacity sets argv-derived flags (eval, sh/bash -c wrappers).
func (l *lowerer) detectOpacity(sc ir.SimpleCommand) {
	switch sc.Program() {
	case "eval":
		l.flags.HasEval = true
	case "sh", "bash", "zsh", "dash", "ksh", "mksh":
		if slices.Contains(sc.Args(), "-c") {
			l.flags.Wrapper = true
		}
	}
}

// word resolves a word to a best-effort literal string. Dynamic parts ($VAR,
// arithmetic) resolve to "" and set DynamicExpansion; command/process
// substitutions additionally lower their bodies into sc.Nested.
func (l *lowerer) word(sc *ir.SimpleCommand, w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range w.Parts {
		b.WriteString(l.wordPart(sc, part))
	}
	return b.String()
}

func (l *lowerer) wordPart(sc *ir.SimpleCommand, part syntax.WordPart) string {
	switch p := part.(type) {
	case *syntax.Lit:
		return p.Value
	case *syntax.SglQuoted:
		return p.Value
	case *syntax.DblQuoted:
		var b strings.Builder
		for _, ip := range p.Parts {
			b.WriteString(l.wordPart(sc, ip))
		}
		return b.String()
	case *syntax.ParamExp:
		l.flags.DynamicExpansion = true
		return ""
	case *syntax.CmdSubst:
		l.flags.DynamicExpansion = true
		sc.Nested = append(sc.Nested, &ir.Script{Shell: l.shell, Pipelines: l.lowerStmts(p.Stmts)})
		return ""
	case *syntax.ProcSubst:
		l.flags.DynamicExpansion = true
		sc.Nested = append(sc.Nested, &ir.Script{Shell: l.shell, Pipelines: l.lowerStmts(p.Stmts)})
		return ""
	case *syntax.ArithmExp:
		l.flags.DynamicExpansion = true
		return ""
	default:
		l.flags.Unparsed = true
		return ""
	}
}

func (l *lowerer) render(n syntax.Node) string {
	var b strings.Builder
	if err := l.printer.Print(&b, n); err != nil {
		return ""
	}
	return strings.TrimSpace(b.String())
}
