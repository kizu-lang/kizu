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

# Install VSCode extension dependencies.
vscode-extension-install:
    cd editors/vscode && npm ci

# Compile the VSCode extension.
vscode-extension-build:
    cd editors/vscode && npm run compile

# Package the VSCode extension as a local VSIX.
vscode-extension-package:
    cd editors/vscode && npm ci && npm run compile && npm run package

# Run VSCode extension checks.
vscode-extension-check:
    cd editors/vscode && npm ci && npm run compile

# Install local git hooks.
hooks:
    pre-commit install

# Parse a Kizu file.
parse file="examples/hello.kizu":
    go run ./cmd/kizu parse {{file}}

# Type, ownership, and borrow check a Kizu file.
kizu-check file="examples/hello.kizu":
    go run ./cmd/kizu check {{file}}

# Print stable formatted Kizu source.
kizu-fmt file="examples/hello.kizu":
    go run ./cmd/kizu fmt {{file}}

# Run a Kizu file through the native build path.
run file="examples/hello.kizu":
    go run ./cmd/kizu run {{file}}

# Run a Kizu file with one prototype process argument.
run-arg file="examples/std_io_process.kizu" arg="input.kizu":
    KIZU_TEST_ENV=env-ok go run ./cmd/kizu run {{file}} -- {{arg}}

# Run a single Kizu test source.
kizu-test file="examples/std_testing.kizu":
    go run ./cmd/kizu test {{file}}

# Dump typed SSA IR for a Kizu file.
ir file="examples/hello.kizu":
    go run ./cmd/kizu ir {{file}}

# Dump optimized typed SSA IR for a Kizu file.
ir-opt file="examples/hello.kizu":
    go run ./cmd/kizu ir --opt {{file}}

# Emit LLVM IR for a Kizu file.
llvm file="examples/hello.kizu":
    go run ./cmd/kizu build --emit-llvm {{file}}

# Emit LLVM IR from optimized typed SSA IR.
llvm-opt file="examples/hello.kizu":
    go run ./cmd/kizu build --emit-llvm --opt {{file}}

# Emit WASI WebAssembly text for a Kizu file.
wasm file="examples/hello.kizu":
    go run ./cmd/kizu build --target wasm32-wasi {{file}}

# Emit WASI WebAssembly text from optimized typed SSA IR.
wasm-opt file="examples/hello.kizu":
    go run ./cmd/kizu build --target wasm32-wasi --opt {{file}}

# Build a native executable through LLVM IR and clang.
native file="examples/hello.kizu":
    go run ./cmd/kizu build --target native --libc on --runtime hosted --linker clang {{file}}

# Build and run the default native hello artifact.
native-smoke:
    go run ./cmd/kizu build --target native --libc on --runtime hosted --linker clang examples/hello.kizu
    ./target/native/hello

# Build and run Phase 2 examples with wasmtime.
wasi-smoke:
    scripts/run-wasi-smoke.sh

# Show local Kizu build cache status.
cache-status:
    go run ./cmd/kizu cache status

# Prune the local Kizu build cache.
cache-prune:
    go run ./cmd/kizu cache prune

# Report which backends accept each conformance example.
backend-matrix:
    go run ./scripts/backend-matrix

# Run the broad baseline timing script.
perf:
    scripts/measure-baseline.sh

# Run cache-specific cold, warm, no-op, and single-file edit timings.
perf-cache:
    scripts/measure-cache.sh

# Run cache checks against an isolated temporary cache directory.
perf-cache-isolated:
    @tmp="$(mktemp -d)"; \
    trap 'rm -rf "$tmp"' EXIT; \
    KIZU_CACHE_DIR="$tmp/cache" scripts/measure-cache.sh

# Exercise opt-in IR optimization through IR and build commands.
opt-smoke file="examples/arithmetic.kizu":
    go run ./cmd/kizu ir --opt {{file}} >/dev/null
    go run ./cmd/kizu build --emit-llvm --opt {{file}} >/dev/null
    go run ./cmd/kizu build --target wasm32-wasi --opt {{file}} >/dev/null
    go run ./cmd/kizu build --target native --opt --libc on --runtime hosted --linker clang {{file}} >/dev/null

# Run the everyday local validation sequence.
verify: fmt test lint

# Cut a release: tag main and push the tag. CI builds and attaches binaries.
release version:
    @echo "{{version}}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || { echo "error: version must look like v0.1.2" >&2; exit 1; }; \
    test "$(git branch --show-current)" = main || { echo "error: release from main" >&2; exit 1; }; \
    test -z "$(git status --porcelain)" || { echo "error: working tree is not clean" >&2; exit 1; }; \
    git fetch origin main; \
    test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" || { echo "error: main is not in sync with origin/main" >&2; exit 1; }; \
    git tag "{{version}}"; \
    git push origin "{{version}}"
