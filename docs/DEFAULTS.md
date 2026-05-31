# Shipped default rules

This document is the **source of truth** for the rule set `ltk` ships with and
writes on `ltk manage install` (unless `--no-default-rules`). The fenced `yaml`
blocks below are extracted, in order, into [`cmd/ltk/sample.ltk.yaml`](../cmd/ltk/sample.ltk.yaml)
(which is embedded in the binary) by [`tools/extract-defaults`](../tools/extract-defaults).
A lefthook pre-commit hook regenerates and fails the commit on drift, so the doc
and the shipped file can never disagree. To regenerate by hand: `just defaults`.

Each rule below is a YAML chunk followed by **why it's a default**. Every default
is a *cooperative nudge* — it turns a command away and points at the safer path;
it is not a security boundary (for hard isolation, run the agent in a container).

Rules use `mode` (default `enable`, a firm denial). Workflow redirects ship as
`mode: confirm` — you can still run the raw command by repeating it within the
window — while the destructive-action guards stay firm (`enable`), so repeating
them changes nothing. See [RULES.md](RULES.md#rule-mode).

## Header

The document opens with the config header: fail open on unparseable input, and a
default window for any `mode: confirm` rule (re-run a denied command within 30s
to proceed anyway — an escape hatch, not a control).

```yaml
version: 1

defaults:
  on_parse_error: allow
  repeat_window_seconds: 30

rules:
```

## Tests through the task runner

The canonical redirect, and the one users most often customize. Agents reach for
`go test` directly; routing through the task runner keeps the suite, env, and
flags identical to CI. Ships as `mode: confirm` — repeat to run raw `go test`
anyway. Edit this for your stack (`make test`, `npm test`, …) or set
`mode: disable`.

The shipped default names no specific runner (no `just`/`make`/`npm` assumption);
fill in your project's command in `suggest` when you adopt it.

```yaml
  - id: tests-via-task-runner
    match: { command: [go, test] }
    mode: confirm
    message: "Run tests through your project's task runner, not the compiler directly, so the suite matches CI."
```

## Don't discard uncommitted work

`git reset --hard` is the single most-reported agent disaster: it silently and
irreversibly throws away uncommitted changes. Stash or commit first.

```yaml
  - id: no-reset-hard
    match: { command: [git, reset, --hard] }
    message: "`git reset --hard` discards uncommitted work irreversibly. Stash or commit first."
    suggest: "git stash --include-untracked"
```

`git clean` deletes untracked files for good — easy for an agent to fire while
"tidying up" and wipe new, unsaved files.

```yaml
  - id: no-git-clean
    match: { command: [git, clean] }
    message: "`git clean` deletes untracked files for good. Preview with `-n`, or stash."
    suggest: "git stash --include-untracked"
```

## Don't bypass the gate

Agents habitually add `--no-verify` to slip past a failing pre-commit/pre-push
hook instead of fixing what it caught — the most common way guardrails get
quietly defeated.

```yaml
  - id: no-skip-commit-hooks
    match: { command: [git, commit, --no-verify] }
    message: "Don't bypass commit hooks with --no-verify — fix what the hook flags, then commit."
```

```yaml
  - id: no-skip-push-hooks
    match: { command: [git, push, --no-verify] }
    message: "Don't bypass push hooks with --no-verify — fix the failure, then push."
```

A plain `--force` push can overwrite a teammate's commits; `--force-with-lease`
refuses if the remote moved.

```yaml
  - id: no-force-push
    match: { command: [git, push, --force] }
    message: "A plain force-push can overwrite a teammate's commits."
    suggest: "git push --force-with-lease"
```

## Keep commits scoped and correctly attributed

`git add -A` / `git add .` sweeps in files the task never touched (and can stage
deletions), so the default is to stage in-scope paths explicitly. But blanket
staging is genuinely the right call when a change spans a large file set — so
this ships as `mode: confirm`: it nudges once, and repeating the command stages
everything.

```yaml
  - id: stage-explicitly
    match: { command: [git, add], args_any: ["-A", "--all", "."] }
    mode: confirm
    message: "Prefer staging the paths this change actually touches. If the change really does span many files, run the same command again to stage them all."
    suggest: "git add <path> [<path> …]"
```

The agent should commit as you, with an identity configured once outside the
session — not rewrite `user.name`/`user.email` per command.

```yaml
  - id: no-rewrite-git-identity
    match: { command: [git, config], args_any: ["user.email", "user.name"] }
    message: "Don't change the git identity from inside the agent; set it once in your own global config."
```

## Destructive shell and privilege

A recursive force-delete with a typo'd path is catastrophic and irreversible.
`-rf`, `-fr`, and `-r -f` are all caught (bundled short options are split for
matching under POSIX shells).

```yaml
  - id: rm-rf-careful
    match: { command: rm, args_all: ["-r", "-f"] }
    message: "Double-check the path before a recursive force-delete; delete specific paths, not trees, when you can."
```

An unattended agent loop has no business escalating privileges.

```yaml
  - id: no-sudo
    match: { command: sudo }
    message: "Don't escalate privileges from inside the agent — ask a human to run anything that needs sudo."
```

Killing processes by name (`pkill`/`killall`) is a blunt instrument — it can hit
the wrong PID, your editor, or a sibling agent. Target a specific PID instead.

```yaml
  - id: no-pkill
    match: { command: pkill }
    message: "Avoid pkill by name (wrong-PID risk) — find the specific PID and `kill` it."
```

```yaml
  - id: no-killall
    match: { command: killall }
    message: "Avoid killall by name (wrong-PID risk) — find the specific PID and `kill` it."
```
