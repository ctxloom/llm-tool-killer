# Rule reference

Rules live in a YAML file (see [`examples/rules.yaml`](../examples/rules.yaml)).
Every command an agent proposes is parsed into a command graph; **each** command
in that graph is tested against the rules **in order**, and the **first matching
`deny` rule wins**. Its `message` and `suggest` are returned to the model so it
can retry the right way.

"Each command in the graph" means a denied command is caught no matter how it is
wrapped — inside pipelines, `&&`/`||`/`;` sequences, subshells `( … )`, brace
groups, command substitution `$( … )` / backticks, process substitution
`<( … )`, assignments (`x=$(…)`), backgrounding (`&`), and `if`/`for`/`while`
bodies. Quoted text is *not* a command, so `echo "go test"` does not match a
`go test` rule. (See the nesting tests in `internal/app`.)

```yaml
version: 1

defaults:
  shell: bash            # fallback dialect (see Shell resolution in ARCHITECTURE.md)
  on_opaque: allow       # eval / $(...) / bash -c / unparsed → allow | deny
  on_parse_error: allow  # command couldn't be parsed → allow | deny

rules:
  - id: go-test-to-just            # required, unique
    match: { command: [go, test] } # see "Matching commands" below
    action: deny                   # deny (default) | allow
    message: "Use `just test`."    # shown to the model on deny
    suggest: "just test"           # optional replacement command
```

Unknown YAML keys are rejected, so a typo (`programm:`) is an error, not a
silent no-op.

## Matching commands

The `match.command` pattern is the program plus its arguments. It can be written
as a string (split on whitespace) or a list (verbatim):

```yaml
match: { command: "go test" }     # ≡
match: { command: [go, test] }
```

Tokens are classified into two kinds. **This is the core of the model:**

| Kind | What it is | How it matches |
|---|---|---|
| **Program** | the first token | `argv[0]` exactly **or by basename** (so `/usr/local/go/bin/go` matches `go`) |
| **Positional** (operand) | a non-option token (subcommand: `test`, `commit`) | **order matters** — must be an ordered prefix of the command's non-option arguments |
| **Option** (flag) | any flag (`-c`, `-x`, `--no-cache`) | **order does not matter** — must appear somewhere in the arguments; never consumes a positional slot |

(These are the standard CLI terms: a positional is a POSIX *operand*; an option
is the flag kind, what `argparse`/`clap` print under "options".)

Trailing arguments are always allowed. A pattern array may **freely mix**
positionals and options in any list order — tokens are classified by kind, not
by where they sit in the list, so `[docker, --debug, build]` and
`[docker, build, --debug]` are equivalent.

### Why positional vs option

A subcommand's position is meaningful: `go test` and `go help test` are different
operations, so `test` is matched **positionally** (it must be the first
non-option argument). An option's position is not meaningful: `sh -e -c …` and
`sh -c -e …` are the same, so options are matched as an **unordered set**.
Options are also skipped when locating positionals, so a leading option never
hides a subcommand: `go --mod=mod test` still matches `[go, test]`.

### Examples

```yaml
match: { command: go }                              # any `go …`
match: { command: [go, test] }                      # `go test …`, `go --mod=mod test …`
match: { command: "sh -c" }                         # `sh -c …`, `sh -e -c …`  (NOT `sh -e`)
match: { command: [git, push, --force, --no-verify] } # those flags in ANY order after `push`
```

### Refinements

Two optional, program-agnostic conditions can be ANDed with (or used without)
`command`:

```yaml
match:
  command: docker
  args_any: [build, buildx]   # at least one present anywhere in args
  args_all: [--push, --tag]   # all present anywhere in args
```

## Portability across shells

Flag syntax differs by shell, so the option/positional classification is
**shell-aware**. The shell is resolved per command (see ARCHITECTURE.md) and the
same rule works everywhere:

| Shell family | A token is an **option** (flag) when it… |
|---|---|
| sh, bash, zsh, mksh | starts with `-` (`-c`, `--no-cache`). A lone `-` is positional (stdin). |
| PowerShell (pwsh) | starts with `-` (`-Path`, `-Recurse`). |
| cmd.exe | starts with `/` (`/c`, `/s`) **or** `-`. |

The `cmd` distinction matters: under cmd, `/c` is a switch, but under a POSIX
shell `/usr/bin/x` is a path — so the same leading-`/` token is an option in one
and positional in the other. Restrict a rule to specific shells with
`match.shells: [cmd]` when needed.

## Opacity (the "adversarial later" seam)

Constructs that can't be statically resolved — `eval`, `bash -c` wrappers,
dynamic expansion (`$VAR`, `$(...)`), or anything the frontend couldn't parse —
are flagged. `defaults.on_opaque` decides what happens when no rule matched but
such a construct is present: `allow` (cooperative default) or `deny` (harden).
This is the single switch that tightens against evasion without touching rules.
