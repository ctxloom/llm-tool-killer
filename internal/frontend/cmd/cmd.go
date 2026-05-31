// Package cmd implements the Windows cmd.exe (batch) frontend. No AST parser
// exists for cmd, so this is a small hand-written lexer + parser covering the
// command grammar that matters for rule matching:
//
//   - operators: & (sequence), && (and), || (or), | (pipe)
//   - grouping: ( … ) (flattened — its commands are surfaced for matching)
//   - quoting: "…" (one token; quotes stripped)
//   - escaping: ^ (the next character is literal, incl. & | ( ) etc.)
//   - variables: %VAR% (and "%VAR%") mark the token dynamic
//   - redirections: >, >>, <, 2>, 2>&1 (consumed, kept off argv)
//
// Control-flow keywords (for/if/do/else) are surfaced as ordinary command words
// rather than interpreted; this is sufficient for matching real programs and is
// documented as a limitation.
package cmd

import (
	"context"
	"strings"

	"github.com/abbitt/llm-tool-killer/internal/ir"
)

// Frontend lowers cmd.exe command lines into the IR.
type Frontend struct{}

// New returns a cmd Frontend.
func New() *Frontend { return &Frontend{} }

// Shells reports the dialects handled by this frontend.
func (f *Frontend) Shells() []ir.Shell { return []ir.Shell{ir.ShellCmd} }

// Parse lowers src into a Script. It is best-effort and never errors; constructs
// it cannot model set the Unparsed opacity flag.
func (f *Frontend) Parse(_ context.Context, shell ir.Shell, src string) (*ir.Script, error) {
	p := &parser{toks: lex(src), shell: shell}
	pipelines := p.parseSequence()
	if p.pos < len(p.toks) {
		p.flags.Unparsed = true // leftover tokens (e.g. an unmatched ')')
	}
	return &ir.Script{Shell: shell, Pipelines: pipelines, Flags: p.flags}, nil
}

// --- lexer ---

type tkind uint8

const (
	tWord tkind = iota
	tAnd        // &&
	tOr         // ||
	tSeq        // & or newline
	tPipe       // |
	tLParen
	tRParen
	tRedir // text holds the operator
)

type tok struct {
	kind    tkind
	text    string
	dynamic bool // word contained %VAR% expansion
}

func lex(s string) []tok {
	var toks []tok
	var buf strings.Builder
	dynamic, hasWord := false, false

	flush := func() {
		if hasWord {
			toks = append(toks, tok{kind: tWord, text: buf.String(), dynamic: dynamic})
			buf.Reset()
			dynamic, hasWord = false, false
		}
	}
	add := func(b byte) { buf.WriteByte(b); hasWord = true }
	op := func(k tkind, t string) { toks = append(toks, tok{kind: k, text: t}) }

	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '^': // escape: next char is literal
			if i+1 < len(s) {
				add(s[i+1])
				i += 2
			} else {
				i++
			}
		case '"': // quoted segment until next quote or end
			hasWord = true
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '%' {
					if j := strings.IndexByte(s[i+1:], '%'); j >= 0 {
						buf.WriteString(s[i : i+j+2])
						dynamic = true
						i += j + 2
						continue
					}
				}
				buf.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++ // closing quote
			}
		case ' ', '\t', '\r':
			flush()
			i++
		case '\n':
			flush()
			op(tSeq, "\n")
			i++
		case '&':
			flush()
			if i+1 < len(s) && s[i+1] == '&' {
				op(tAnd, "&&")
				i += 2
			} else {
				op(tSeq, "&")
				i++
			}
		case '|':
			flush()
			if i+1 < len(s) && s[i+1] == '|' {
				op(tOr, "||")
				i += 2
			} else {
				op(tPipe, "|")
				i++
			}
		case '(':
			flush()
			op(tLParen, "(")
			i++
		case ')':
			flush()
			op(tRParen, ")")
			i++
		case '>', '<':
			// a pure-digit word immediately before is a file-descriptor prefix
			// (e.g. 2>): drop it from argv rather than emitting it.
			if hasWord && isDigits(buf.String()) && !dynamic {
				buf.Reset()
				hasWord = false
			} else {
				flush()
			}
			o := string(c)
			i++
			if c == '>' && i < len(s) && s[i] == '>' {
				o += ">"
				i++
			}
			if i < len(s) && s[i] == '&' {
				o += "&"
				i++
			}
			op(tRedir, o)
		case '%':
			if j := strings.IndexByte(s[i+1:], '%'); j >= 0 {
				buf.WriteString(s[i : i+j+2])
				hasWord, dynamic = true, true
				i += j + 2
			} else {
				add(c)
				i++
			}
		default:
			add(c)
			i++
		}
	}
	flush()
	return toks
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// --- parser ---

type parser struct {
	toks  []tok
	pos   int
	shell ir.Shell
	flags ir.OpacityFlags
}

func (p *parser) peek() (tok, bool) {
	if p.pos < len(p.toks) {
		return p.toks[p.pos], true
	}
	return tok{}, false
}

// parseSequence parses statements separated by & && || newline, until EOF or a
// ')'. The ')' is left for the caller (parsePipeline) to consume.
func (p *parser) parseSequence() []ir.Pipeline {
	var out []ir.Pipeline
	conn := ir.ConnNone
	for {
		t, ok := p.peek()
		if !ok || t.kind == tRParen {
			break
		}
		out = append(out, p.parsePipeline(conn)...)
		t, ok = p.peek()
		if !ok || t.kind == tRParen {
			break
		}
		switch t.kind {
		case tAnd:
			conn = ir.ConnAnd
		case tOr:
			conn = ir.ConnOr
		default:
			conn = ir.ConnSeq
		}
		p.pos++ // consume the separator
	}
	return out
}

// parsePipeline parses one pipeline (commands joined by |) or a ( … ) group,
// which is flattened into its inner pipelines.
func (p *parser) parsePipeline(conn ir.Connector) []ir.Pipeline {
	if t, ok := p.peek(); ok && t.kind == tLParen {
		p.pos++ // (
		inner := p.parseSequence()
		if t2, ok := p.peek(); ok && t2.kind == tRParen {
			p.pos++
		} else {
			p.flags.Unparsed = true
		}
		p.skipRedirs() // trailing redirs on the group, e.g. (...) > file
		return inner
	}

	var cmds []ir.SimpleCommand
	for {
		if sc, ok := p.parseSimpleCommand(); ok {
			cmds = append(cmds, sc)
		}
		if t, ok := p.peek(); ok && t.kind == tPipe {
			p.pos++
			continue
		}
		break
	}
	if len(cmds) == 0 {
		return nil
	}
	return []ir.Pipeline{{Connector: conn, Commands: cmds}}
}

func (p *parser) parseSimpleCommand() (ir.SimpleCommand, bool) {
	var sc ir.SimpleCommand
	got := false
	for {
		t, ok := p.peek()
		if !ok {
			break
		}
		switch t.kind {
		case tWord:
			sc.Argv = append(sc.Argv, t.text)
			if t.dynamic {
				p.flags.DynamicExpansion = true
			}
			got = true
			p.pos++
		case tRedir:
			p.pos++
			target := ""
			if w, ok := p.peek(); ok && w.kind == tWord {
				target = w.text
				p.pos++
			}
			sc.Redirects = append(sc.Redirects, ir.Redirect{Op: t.text, Target: target})
		default:
			if got {
				p.detectOpacity(&sc)
			}
			return sc, got
		}
	}
	if got {
		p.detectOpacity(&sc)
	}
	return sc, got
}

// skipRedirs consumes redirection operators (and their targets) with no command.
func (p *parser) skipRedirs() {
	for {
		t, ok := p.peek()
		if !ok || t.kind != tRedir {
			return
		}
		p.pos++
		if w, ok := p.peek(); ok && w.kind == tWord {
			p.pos++ // target
		}
	}
}

// detectOpacity flags cmd's shell wrappers (cmd /c, and cross-called pwsh -Command).
func (p *parser) detectOpacity(sc *ir.SimpleCommand) {
	switch strings.ToLower(sc.Program()) {
	case "cmd", "cmd.exe":
		for _, a := range sc.Args() {
			if la := strings.ToLower(a); la == "/c" || la == "/k" {
				p.flags.Wrapper = true
				break
			}
		}
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		for _, a := range sc.Args() {
			if la := strings.ToLower(a); la == "-command" || la == "-c" || la == "-encodedcommand" {
				p.flags.Wrapper = true
				break
			}
		}
	}
}
