set shell := ["sh", "-eu", "-c"]

default:
    @just --list

# Format Go sources.
fmt:
    gofmt -w cmd internal

# Run all Go tests.
test:
    go test ./...

# Run static analysis.
lint:
    golangci-lint run

# Run the full commit gate.
check:
    pre-commit run --all-files

# Install local git hooks.
hooks:
    pre-commit install

# Parse a Kizu file.
parse file="examples/hello.kizu":
    go run ./cmd/kizu parse {{file}}

# Type, ownership, and borrow check a Kizu file.
kizu-check file="examples/hello.kizu":
    go run ./cmd/kizu check {{file}}

# Run a Kizu file through the interpreter.
run file="examples/hello.kizu":
    go run ./cmd/kizu run {{file}}

# Dump typed SSA IR for a Kizu file.
ir file="examples/hello.kizu":
    go run ./cmd/kizu ir {{file}}

# Emit LLVM IR for a Kizu file.
llvm file="examples/hello.kizu":
    go run ./cmd/kizu build --emit-llvm {{file}}

# Emit WASI WebAssembly text for a Kizu file.
wasm file="examples/hello.kizu":
    go run ./cmd/kizu build --target wasm32-wasi {{file}}

# Build and run Phase 2 examples with wasmtime.
wasi-smoke:
    scripts/run-wasi-smoke.sh

# Show local Kizu build cache status.
cache-status:
    go run ./cmd/kizu cache status

# Prune the local Kizu build cache.
cache-prune:
    go run ./cmd/kizu cache prune

# Explain whether a file would rebuild.
why file="examples/hello.kizu":
    go run ./cmd/kizu why-rebuild {{file}}

# Run the broad baseline timing script.
perf:
    scripts/measure-baseline.sh

# Run cache-specific cold, warm, no-op, and small-edit timings.
perf-cache:
    scripts/measure-cache.sh

# Run cache checks against an isolated temporary cache directory.
perf-cache-isolated:
    @tmp="$(mktemp -d)"; \
    trap 'rm -rf "$tmp"' EXIT; \
    KIZU_CACHE_DIR="$tmp/cache" scripts/measure-cache.sh

# Exercise cache hit, status, why-rebuild, and prune on hello.kizu.
cache-smoke:
    @tmp="$(mktemp -d)"; \
    trap 'rm -rf "$tmp"' EXIT; \
    KIZU_CACHE_DIR="$tmp/cache" go run ./cmd/kizu build --emit-llvm examples/hello.kizu >/dev/null; \
    KIZU_CACHE_DIR="$tmp/cache" go run ./cmd/kizu build --target wasm32-wasi examples/hello.kizu >/dev/null; \
    KIZU_CACHE_DIR="$tmp/cache" go run ./cmd/kizu build --emit-llvm examples/hello.kizu >/dev/null; \
    KIZU_CACHE_DIR="$tmp/cache" go run ./cmd/kizu why-rebuild examples/hello.kizu; \
    KIZU_CACHE_DIR="$tmp/cache" go run ./cmd/kizu cache status; \
    KIZU_CACHE_DIR="$tmp/cache" go run ./cmd/kizu cache prune

# Run the everyday local validation sequence.
verify: fmt test lint
