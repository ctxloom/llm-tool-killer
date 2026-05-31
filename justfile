# llm-tool-killer — build & tooling
# Run `just` to list recipes.

bin := "bin/ltk"
pkg := "./cmd/ltk"

# List available recipes.
default:
    @just --list

# Build a dev binary into ./bin.
build:
    go build -o {{bin}} {{pkg}}

# Build a stripped, CGO-free static binary (release shape).
build-static:
    CGO_ENABLED=0 go build -ldflags='-s -w' -o {{bin}} {{pkg}}

# Run all tests.
test:
    go test ./...

# Run tests with the race detector.
test-race:
    go test -race ./...

# Report any unformatted files (CI-friendly; non-zero on drift).
fmt-check:
    @test -z "$(gofmt -l .)" || { echo "unformatted:"; gofmt -l .; exit 1; }

# Format the tree in place.
fmt:
    gofmt -w .

# go vet.
vet:
    go vet ./...

# Tidy modules.
tidy:
    go mod tidy

# Full pre-commit gate: format check, vet, tests.
check: fmt-check vet test

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
