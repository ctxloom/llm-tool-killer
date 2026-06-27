# llm-tool-killer — build & tooling
# Run `just` to list recipes.

bin := "bin/ltk"
pkg := "./cmd/ltk"

# Version from versionator (fallback "dev" where versionator isn't available).
# Standardized stamp format across the ctxloom family:
#   v<major.minor.patch>-<short-sha>-<YYYYMMDDTHHMMSS commit datetime, utc>
# versionator emits the compact datetime (no separator); sed inserts the 'T'.
version := `if v=$(versionator output version -t "{{Prefix}}{{MajorMinorPatch}}-{{ShortHash}}-{{CommitDateCompact}}" --prefix 2>/dev/null); then echo "$v" | sed -E 's/([0-9]{8})([0-9]{6})$/\1T\2/'; else echo dev; fi`
ldflags := "-X main.Version=" + version

# List available recipes.
default:
    @just --list

# Build a dev binary into ./bin (version-stamped).
build:
    go build -ldflags "{{ldflags}}" -o {{bin}} {{pkg}}

# Build a stripped, CGO-free static binary (release shape, version-stamped).
build-static:
    CGO_ENABLED=0 go build -ldflags "-s -w {{ldflags}}" -o {{bin}} {{pkg}}

# Install ltk into GOBIN (version-stamped).
install:
    go install -ldflags "{{ldflags}}" {{pkg}}

# Show the version that builds will stamp.
show-version:
    @versionator output version

# Set the release version in a PR (the only supported way to release).
# Releases are merge-triggered: bump here, commit VERSION, merge — CI tags it.
# See docs/RELEASING.md. Example: just set-version 0.7.0
set-version version:
    versionator set {{version}}

# Releases are cut by merging a VERSION bump to main, not from your machine.
release:
    @echo "Releases are merge-triggered. Run 'just set-version <v>', commit VERSION," >&2
    @echo "open a PR, and merge it — CI tags and publishes. See docs/RELEASING.md." >&2
    @exit 1

# Run all tests.
test:
    go test ./...

# Run tests with the race detector.
test-race:
    go test -race ./...

# Run the human-readable acceptance features (godog, pretty output).
acceptance:
    go test ./tests/acceptance/ -v

# Report any unformatted files (CI-friendly; non-zero on drift).
fmt-check:
    @test -z "$(gofmt -l .)" || { echo "unformatted:"; gofmt -l .; exit 1; }

# Format the tree in place.
fmt:
    gofmt -w .

# go vet.
vet:
    go vet ./...

# golangci-lint (v2, pinned via go.mod tool directive; config in .golangci.yml).
# Includes errcheck with the std-error-handling exclusions.
lint:
    go tool golangci-lint run

# Cyclomatic complexity gate (gocyclo, pinned via go.mod tool directive).
# Reports any function whose complexity exceeds 15.
complexity:
    go tool gocyclo -over 15 .

# Show the most complex functions (informational; never fails).
complexity-top:
    go tool gocyclo -top 15 .

# Tidy modules.
tidy:
    go mod tidy

# Regenerate the shipped default rules from docs/DEFAULTS.md (the source of truth).
defaults:
    go run ./tools/extract-defaults

# Fail if the shipped defaults are out of sync with docs/DEFAULTS.md.
defaults-check:
    go run ./tools/extract-defaults -check

# Install git hooks via lefthook (run once).
hooks:
    lefthook install

# Full pre-commit gate: format check, vet, lint, complexity, defaults sync, tests.
check: fmt-check vet lint complexity defaults-check test

# Build then run the bundled smoke checks against examples/rules.yaml.
smoke: build
    @echo "--- go test ./...  (expect deny) ---"
    @echo '{"tool_name":"Bash","tool_input":{"command":"go test ./..."}}' | {{bin}} evaluate --config examples/rules.yaml || true
    @echo "\n--- ls -la  (expect silent allow) ---"
    @echo '{"tool_name":"Bash","tool_input":{"command":"ls -la"}}' | {{bin}} evaluate --config examples/rules.yaml || true
    @echo "(allow is intentionally silent)"

# Remove build artifacts.
clean:
    rm -rf bin
