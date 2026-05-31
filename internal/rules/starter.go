package rules

// StarterConfig is a minimal, check-in-able rules file written by `ltk manage
// install` when none exists. It is engine-agnostic — just rules.
const StarterConfig = `# llm-tool-killer rules — restrict casual LLM tool use for THIS project.
# Commands are parsed (across shells) and matched against these rules; the first
# matching deny wins and its message is returned to the model so it can retry.
# Full model: https://github.com/abbitt/llm-tool-killer/blob/main/docs/RULES.md
version: 1

defaults:
  on_opaque: allow       # eval / $(...) / wrappers: allow (cooperative) | deny (harden)
  on_parse_error: allow

rules:
  - id: no-git-tag
    match: { command: [git, tag] }
    message: "Don't tag releases by hand — use the established release pipeline (Versionator)."

  - id: tests-via-task-runner
    match: { command: [go, test] }
    message: "Run tests through the project task runner, not the compiler directly."
    suggest: "just test"   # or: make test
`
