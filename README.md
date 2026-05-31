# llm-tool-killer (`ltk`)

A small static binary that an LLM coding agent calls as a **pre-tool hook**. It
parses the shell command the agent is about to run and, if a rule matches,
**turns it away and tells the model what to do instead**.

```
go test ./...   ⟶   ✗ "Run tests through the task runner."   →   the model retries with `just test`
```

---

## Why

A plain "deny these commands" hook hits two walls:

1. **It's error-prone** — substring/regex matching misfires on quoting, paths,
   and arguments.
2. **The agent walks around it** — wrapping a blocked command in a sub-shell, a
   variable, or `eval` slips right past naive matching.

`ltk` goes a level deeper: it **parses and understands** the command — resolving
known variables and re-parsing trivial wrappers — then matches rules against the
*real* command, however it's dressed up.

## Scope — read this first

`ltk` is **not a hard compliance control, and not a sandbox.** It's a cooperative
helper that redirects the agent away from operations you'd **rather it not do** —
not operations it must *never, under any circumstances* be able to do.

If you tell the agent to work around a rule, **it will**: write and compile
equivalent code, download the blocked tool and recompile it under a different
name, create a symlink, or any of a hundred other paths. `ltk` understands
*trivial* workarounds and matches the real command behind them; it makes no
claim beyond that. Deeper intent-based detection is aspirational and may never be
feasible.

One bypass deserves special mention precisely because the redirect model leans
on it: the agent can **add a `just`/`make` target that runs the blocked command,
then invoke that target**. The hook only sees the command the agent runs
directly (`just sneaky-test`); the `go test` the task runner spawns is a child
process the hook never inspects — the same property that lets a legitimate
`just test` through. So treat your `justfile`/`Makefile` as part of the trusted
surface, not something the rules can police.

**If you have "never-ever" requirements, run the agent in a sandbox / container.**
That's the right tool for hard isolation; `ltk` is not.

## How it works

```
hook payload (stdin) → engine adapter → resolve shell → parse + understand → match rules → decision (stdout/exit)
```

- The agent picks a **tool** (`Bash`, `PowerShell`), which determines the shell.
- POSIX shells (sh/bash/zsh/mksh) are parsed in-process via
  [`mvdan.cc/sh`](https://github.com/mvdan/sh); **PowerShell** defers to its own
  native parser; **cmd.exe** uses a small built-in lexer. All lower to one common
  command graph.
- **Understanding:** shell variables are resolved against the environment the
  agent runs in plus assignments made in the same command, and trivial wrappers
  (`bash -c`, `eval`, `cmd /c`, `pwsh -Command`) are re-parsed — so a denied
  command can't be smuggled through. Things we genuinely can't resolve (runtime
  values, command output) are left alone, not blocked.
- Rules match the parsed graph, so a denied command is caught inside pipelines,
  `&&`/`;` sequences, subshells, and substitutions too. Quoted text
  (`echo "go test"`) is not a command and does not match.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full design.

## Behavior (living specification)

The scenarios below are the real acceptance tests: `go test ./tests/acceptance/`
**extracts this Gherkin from this README** and runs it against the decision
engine via [godog](https://github.com/cucumber/godog). The documented behavior
*is* the tested behavior.

```gherkin
Feature: Keeping an LLM agent on the project's golden path

  When an agent proposes a command we'd rather it not run, we turn it away and
  point it at the project's preferred way of doing the job. This is a nudge, not
  a lock: it keeps a cooperative agent on the rails, but anyone determined to go
  around it still can.

  Background:
    Given the project asks agents to use:
      | instead of running | use this  | because                                        |
      | go test            | just test | tests run through the task runner              |
      | git tag            |           | releases go through the pipeline (Versionator) |

  Rule: A discouraged command is turned away, with a reason

    Scenario: Running the tests the direct way
      When the agent runs "go test ./..."
      Then the command is turned away
      And the agent is told "tests run through the task runner"
      And the agent is pointed at "just test"

    Scenario: Tagging a release by hand
      When the agent runs "git tag v1.4.0"
      Then the command is turned away
      And the agent is told "releases go through the pipeline (Versionator)"

    Scenario: An ordinary command is left alone
      When the agent runs "ls -la && cat README.md"
      Then the command is allowed

  Rule: Phrasing the same command differently does not get it through

    Scenario Outline: The discouraged command, dressed up another way
      When the agent runs "<command>"
      Then the command is turned away

      Examples:
        | command                  |
        | bash -c 'go test'        |
        | task=test; go $task      |
        | CMD='go test'; eval $CMD |

  Rule: We point the way; we do not stand guard

    Scenario: A command whose meaning is only settled when it runs is left alone
      When the agent runs "eval $RESOLVED_AT_RUNTIME"
      Then the command is allowed
```

Run them with `just acceptance`.

## Install

Build a static binary (no runtime dependencies):

```sh
just build-static      # → bin/ltk
# or: go install github.com/benjaminabbitt/llm-tool-killer/cmd/ltk@latest
```

Register the hook in your agent's config and scaffold a starter rules file.
`manage install` **auto-detects the most relevant agent** (e.g. a `.claude/`
directory) and installs there:

```sh
ltk manage install               # detect engine; write its config + .ltk.yaml
ltk manage install --global      # user-level config instead of project
ltk manage install --print       # dry run: show the merged config, write nothing
ltk manage uninstall             # cleanly remove the hook again (exact inverse)
```

`install` merges the hook **non-destructively** (your other settings are
preserved) and writes a starter `.ltk.yaml` you can edit and commit.

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

defaults:
  on_parse_error: allow   # if a command can't be parsed at all: allow (fail-open) | deny

rules:
  - id: tests-via-task-runner
    match: { command: [go, test] }      # also matches `go --mod=mod test …`
    message: "Run tests through the task runner."
    suggest: "just test"

  - id: no-git-tag
    match: { command: [git, tag] }
    message: "Releases go through the pipeline (Versionator)."

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
| **Shells** | sh, bash, zsh, mksh (in-process via mvdan/sh); PowerShell (native parser); cmd.exe (built-in lexer). Variable resolution: shell family. |
| **Engines** | Claude Code (`PreToolUse`). Codex & Gemini: planned (adapters only — see ARCHITECTURE.md). |

## Develop

```sh
just                # list recipes
just check          # fmt-check + vet + complexity + test
just test           # go test ./... (includes the README-driven acceptance suite)
just acceptance     # run the embedded Gherkin with pretty output
just complexity     # cyclomatic-complexity gate (gocyclo, fails > 15)
just build-static   # static release binary
```

The complexity gate uses [`gocyclo`](https://github.com/fzipp/gocyclo), pinned
via a `go.mod` tool directive. Versions are stamped from
[Versionator](https://github.com/benjaminabbitt/versionator) (`ltk --version`).

## License

BSD 3-Clause. See [LICENSE](LICENSE).
