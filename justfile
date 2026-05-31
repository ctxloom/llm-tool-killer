# llm-tool-killer — build & tooling
# Run `just` to list recipes.

bin := "bin/ltk"
pkg := "./cmd/ltk"

# Version from versionator (fallback "dev" where versionator isn't available).
# Requires versionator >= v0.2.0 (DateTimeDirty + `output version`). Same version
# string format as ctxloom: v<major.minor.patch>[-<shorthash><dirty timestamp>].
version := `versionator output version -t "{{Prefix}}{{MajorMinorPatch}}{{PreReleaseWithDash}}" --prefix --prerelease="{{ShortHash}}{{DateTimeDirty}}" 2>/dev/null || echo "dev"`
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

# Auto-bump the version from commit messages (versionator).
bump:
    versionator bump

# Tag and push a release for the current version (versionator).
release:
    versionator release push

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

# Full pre-commit gate: format check, vet, complexity, defaults sync, tests.
check: fmt-check vet complexity defaults-check test

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
