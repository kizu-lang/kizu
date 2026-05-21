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

# Run Go/Kizu selfhost component oracle parity checks.
selfhost-oracle:
    GOGC=1000 KIZU_RUN_SELFHOST_ORACLE=1 go test -timeout=20m ./cmd/kizu -run TestSelfhostOracleRunner -count=1 -v

# Run direct heavyweight selfhost gates for focused debugging.
selfhost-integration-gates:
    KIZU_RUN_SELFHOST_GATES=1 go test -timeout=20m ./cmd/kizu -run 'TestSelfhost(ResolverGate|TypeGate|OwnershipGate|IRHandoffGate|IRArtifactGate|BackendArtifactGate|PipelineGate)$' -v

# Run the minimum selfhost CLI contract gate.
selfhost-cli-gate:
    KIZU_RUN_SELFHOST_GATES=1 go test -timeout=10m ./cmd/kizu -run 'TestSelfhostCLIGate$' -count=1 -v

# Run the selfhost production switch review gate.
selfhost-switch-gate:
    just selfhost-production-from-scratch
    just selfhost-oracle
    go test ./cmd/kizu -run 'TestSelfhostPackageSkeletonChecks$' -v
    go test ./internal/project ./internal/types ./internal/ownership

# Run the no-Go bootstrap contract preflight before starting stage work.
selfhost-bootstrap-preflight:
    just selfhost-switch-gate
    just cache-smoke

# Run the stage0-stage1-stage2 selfhost bootstrap comparison.
selfhost-bootstrap:
    KIZU_RUN_SELFHOST_BOOTSTRAP=1 go test -timeout=20m ./cmd/kizu -run 'TestSelfhostBootstrapRunner$' -count=1 -v

# Run #458 commands through the hosted stage2 production artifact.
selfhost-production-gate:
    KIZU_RUN_SELFHOST_PRODUCTION=1 go test -timeout=20m ./cmd/kizu -run 'TestSelfhostProductionBoundaryGate$' -count=1 -v

# Build the hosted artifact once, then run production, corpus, and CLI parity gates.
selfhost-production-from-scratch:
    just selfhost-bootstrap
    just selfhost-production-gate
    just selfhost-corpus-gate
    just selfhost-parse-parity-gate
    just selfhost-check-parity-gate
    just selfhost-run-parity-gate
    just selfhost-test-parity-gate

# Run the supported corpus through the hosted selfhost artifact.
selfhost-corpus-gate:
    KIZU_RUN_SELFHOST_CORPUS=1 go test -timeout=20m ./cmd/kizu -run 'TestSelfhostSupportedCorpusGate$' -count=1 -v

# Build the hosted artifact once, then run the supported corpus.
selfhost-corpus-gate-from-scratch:
    just selfhost-bootstrap
    just selfhost-corpus-gate

# Run #525 parse <file> parity through the hosted selfhost artifact.
selfhost-parse-parity-gate:
    KIZU_RUN_SELFHOST_PARSE_PARITY=1 go test -timeout=20m ./cmd/kizu -run 'TestSelfhostParseParityGate$' -count=1 -v

# Run #530 check <file> parity through the hosted selfhost artifact.
selfhost-check-parity-gate:
    KIZU_RUN_SELFHOST_CHECK_PARITY=1 go test -timeout=20m ./cmd/kizu -run 'TestSelfhostCheckParityGate$' -count=1 -v

# Build the hosted artifact once, then run check <file> parity.
selfhost-check-parity-gate-from-scratch:
    just selfhost-bootstrap
    just selfhost-check-parity-gate

# Run #569 run <file> parity through the hosted selfhost artifact.
selfhost-run-parity-gate:
    KIZU_RUN_SELFHOST_RUN_PARITY=1 go test -timeout=20m ./cmd/kizu -run 'TestSelfhostRunParityGate$' -count=1 -v

# Build the hosted artifact once, then run run <file> parity.
selfhost-run-parity-gate-from-scratch:
    just selfhost-bootstrap
    just selfhost-run-parity-gate

# Run #570 test <file> parity through the hosted selfhost artifact.
selfhost-test-parity-gate:
    KIZU_RUN_SELFHOST_TEST_PARITY=1 go test -timeout=20m ./cmd/kizu -run 'TestSelfhostTestParityGate$' -count=1 -v

# Build the hosted artifact once, then run test <file> parity.
selfhost-test-parity-gate-from-scratch:
    just selfhost-bootstrap
    just selfhost-test-parity-gate

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

# Run a Kizu file through the interpreter.
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

# Explain whether a file would rebuild.
why file="examples/hello.kizu":
    go run ./cmd/kizu why-rebuild {{file}}

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

# Exercise opt-in IR optimization through IR and build commands.
opt-smoke file="examples/arithmetic.kizu":
    go run ./cmd/kizu ir --opt {{file}} >/dev/null
    go run ./cmd/kizu build --emit-llvm --opt {{file}} >/dev/null
    go run ./cmd/kizu build --target wasm32-wasi --opt {{file}} >/dev/null
    go run ./cmd/kizu build --target native --opt --libc on --runtime hosted --linker clang {{file}} >/dev/null

# Run the everyday local validation sequence.
verify: fmt test lint
