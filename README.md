# llm-tool-killer (`ltk`)

A small static binary that an LLM coding agent calls as a **pre-tool hook**. It
parses the shell command the agent is about to run, and — if a rule matches —
**denies it and tells the model what to do instead**.

```
go test ./...   ⟶   ✗ denied: "Run tests through the task runner."  →  the model retries with `just test`
```

## Why

I started with a plain "deny these bash commands" hook and hit two walls:

1. **It was error-prone** — substring/regex matching misfires on quoting, paths,
   and arguments.
2. **The LLM bypassed it** — wrapping a blocked command in a subshell, `$(…)`,
   `bash -c`, or a pipeline slipped right past naive matching.

`ltk` goes one level deeper: it **parses** the command into a command graph and
matches rules against *every* command in it, however nested. It is not a
security boundary (a determined process can still evade a static parser — see
[Scope](#scope)); it's there to **hold a cooperative LLM back a touch** with a
per-project, **check-in-able** config that restricts casual tool use.

### Use cases

- **Don't `git tag` by hand** — use the established release pipeline (e.g.
  [Versionator](https://github.com/benjaminabbitt/versionator)).
- **Don't run tests with the compiler** — use the `make`/`just` target so the
  real test environment (fixtures, env, build tags) is set up.
- Nudge any "the casual way" → "the project's way": `npm install` → `pnpm`,
  `docker build` → CI, and so on.

## How it works

```
hook payload (stdin) → engine adapter → resolve shell → parse to IR → match rules → decision (stdout/exit)
```

- The agent picks a **tool** (`Bash`, `PowerShell`), which determines the shell.
- POSIX shells (sh/bash/zsh/mksh) are parsed in-process via
  [`mvdan.cc/sh`](https://github.com/mvdan/sh); **PowerShell** is parsed by
  deferring to its own native parser and mapping the result back to the IR.
- Rules match the parsed command graph, so a blocked command is caught inside
  pipelines, `&&`/`;` sequences, subshells, `$(…)`, backticks, process
  substitution, and `if`/`for` bodies. Quoted text (`echo "go test"`) is not a
  command and does not match.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full design.

## Install

Build and install the binary (static, no dependencies):

```sh
just build-static      # → bin/ltk
# or: go install github.com/benjaminabbitt/llm-tool-killer/cmd/ltk@latest
```

Then register the hook and scaffold a starter rules file. `manage install`
**auto-detects the most relevant agent** for the project (e.g. a `.claude/`
directory) and installs there:

```sh
ltk manage install               # detect engine; write its config + .ltk.yaml
ltk manage install --global      # user-level config instead of project
ltk manage install --print       # dry run: show the merged config, write nothing
ltk manage install --engine claude-code   # force a specific engine
ltk manage uninstall             # cleanly remove the hook again
```

`install` merges the hook **non-destructively** (your other settings and hooks
are preserved) and, if `.ltk.yaml` doesn't exist, writes a starter you can edit
and commit. `uninstall` is its exact inverse.

<details>
<summary>Manual Claude Code settings.json</summary>

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash|PowerShell",
        "hooks": [ { "type": "command", "command": "ltk evaluate --config .ltk.yaml" } ] }
    ]
  }
}
```
</details>

## Rules

Rules live in a YAML file (`.ltk.yaml` by convention). The first matching `deny`
wins; its `message`/`suggest` is returned to the model.

```yaml
version: 1
rules:
  - id: no-git-tag
    match: { command: [git, tag] }
    message: "Don't tag releases by hand — use the release pipeline (Versionator)."

  - id: tests-via-task-runner
    match: { command: [go, test] }      # also matches `go --mod=mod test …`
    message: "Run tests through the task runner."
    suggest: "just test"

  - id: no-shell-wrapper
    match: { command: "sh -c" }         # flags match in any order
```

`command` is an argv pattern: a **program** (matched by name or basename),
**positional** args (subcommands, order matters), and **options** (flags, order
doesn't). The full model — including cross-shell portability — is in
[docs/RULES.md](docs/RULES.md).

## Supported

| | |
|---|---|
| **Shells** | sh, bash, zsh, mksh (in-process via mvdan/sh); PowerShell (native parser); cmd.exe (hand-written lexer). |
| **Engines** | Claude Code (`PreToolUse`). Codex & Gemini: planned (adapters only — see ARCHITECTURE.md). |

## Develop

```sh
just                # list recipes
just check          # fmt-check + vet + complexity + test
just test           # go test ./...
just complexity     # cyclomatic-complexity gate (gocyclo, fails > 15)
just complexity-top # show the most complex functions (informational)
just smoke          # build + run sample decisions
```

The complexity gate uses [`gocyclo`](https://github.com/fzipp/gocyclo), pinned
via a `go.mod` tool directive (`go tool gocyclo`), so it needs no separate
install and tracks the toolchain.

## Scope

This is a **cooperative guardrail**, not a sandbox. A static parser can't
soundly resolve `eval`, dynamic expansion, or `bash -c "$VAR"` without executing
them. Those constructs are flagged, and `defaults.on_opaque: deny` lets you turn
the screws (deny anything that can't be statically analyzed) when you want to
harden — but for hard isolation, use OS-level sandboxing.
