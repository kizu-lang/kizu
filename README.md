# Kizu

<p align="center">
  <img src="docs/assets/kizu-logo.svg" alt="Kizu logo" width="180">
</p>

Kizu is an explicit, memory-safe systems programming language prototype.

The name comes from the Japanese word "kizu", meaning "wound" or "scratch".

> Do not create wounds. Do not hide wounds.

Kizu borrows some safety ideas from Rust, but it is not a Rust clone. The goal is
to explore a language that is simpler than Rust, safer than C/C++/Zig in safe
code, and less likely to grow heavy CI and build caches.

[日本語版 README](README.ja.md)

## Status

Kizu is an early prototype implemented in Go.

The v0.1 target is the interpreter-first language core. The current v0.2
baseline adds the minimal standard-library surface and tooling needed to keep
language behavior testable. The authoritative behavior is still the Go
interpreter plus `kizu check`.

Implemented language-core pieces:

- lexer, parser, AST, and CLI
- interpreter
- type checker
- move checker
- local borrow checker
- `arena<T>` / `handle<T>`
- `while`, `break`, `continue`, labeled loop branches, and bounded `for`
- limited `comptime` expressions, parameters, and branch selection
- minimal `!T` and `try` error propagation
- unsafe boundary and C ABI declaration checks
- explicit `cast<T>(value)` checker policy
- Zig/C-style tag `enum`, tagged `union`, and exhaustive `match`
- `std::io::blocking/threaded/failing` and `TaskGroup` structured task model
- `std::channel::Channel<T>` owned message passing
- `std::task::Queue` deterministic deferred task queue
- `std::task::parallel_for` and `std::task::parallel_map` safe data-parallel prototypes
- scoped thread, `Atomic<T>`, and `Mutex<T>` boundary prototypes
- `contract`, `satisfy`, and `&Dyn<Contract>`
- minimal `std::mem`, `std::array::Array<T>`, `std::string::String`,
  `std::map::Map<K, V>`, and `std::testing`
- explicit-Io `std::fs`, `std::path`, `std::io`, and `std::process` helpers
- `kizu test <file>` single-file test runner

Experimental compiler and tooling pieces:

- Kizu stdlib migration layout in `std/`
- typed SSA IR
- LLVM IR text backend
- bounded local build cache and rebuild explanations
- WASI-compatible WebAssembly text backend
- limited C header import for extern function declarations
- opt-in IR optimization pipeline
- limited native executable generation through LLVM IR and clang

These experimental pieces are not the language oracle yet. LLVM and WASM
currently support more limited target subsets than the interpreter. Native
builds are limited to the LLVM-lowered subset and a small `kizu_print_*`
runtime shim. The current native path uses host `clang` and libc; future
no-libc / freestanding builds are part of the accepted build policy.

There are no open v0.2 issues at the time of writing. Future compiler migration
work should start from new GitHub Issues with explicit acceptance criteria.

This repository is still experimental. Syntax and implementation details can
change while the language design is being tested.

## Example

```kizu
fn main() {
    print("hello, kizu");
}
```

Run it with the interpreter:

```sh
go run ./cmd/kizu run examples/hello.kizu
```

Run one Kizu test source:

```sh
go run ./cmd/kizu test examples/std_testing.kizu
```

Pass prototype process arguments with `--`:

```sh
go run ./cmd/kizu run examples/std_io_process.kizu -- input.kizu
```

See the [examples catalog](examples/README.md) for runnable feature
examples and negative safety-rule examples. The machine-readable conformance
manifest is [tests/conformance/v0_1.json](tests/conformance/v0_1.json); it is
currently reused for both v0.1 language-core and v0.2 stdlib prototype coverage.
The safe-code memory-safety contract is documented in
[docs/memory-safety.md](docs/memory-safety.md).
Open compiler specification gaps are tracked in
[docs/compiler-spec-gaps.md](docs/compiler-spec-gaps.md).

## Development Environment

The recommended development environment is the Nix flake.

```sh
nix develop
pre-commit install
```

The shell includes Go, golangci-lint, pre-commit, just, and wasmtime.

## Common Commands

```sh
just --list
just verify
just perf
just perf-cache
just cache-smoke
just wasi-smoke
```

The same commands can be run directly:

```sh
go test ./...
golangci-lint run
pre-commit run --all-files

go run ./cmd/kizu parse examples/hello.kizu
go run ./cmd/kizu check examples/hello.kizu
go run ./cmd/kizu fmt examples/hello.kizu
go run ./cmd/kizu run examples/hello.kizu
go run ./cmd/kizu test examples/std_testing.kizu
go run ./cmd/kizu ir examples/hello.kizu
go run ./cmd/kizu ir --opt examples/hello.kizu
go run ./cmd/kizu build --emit-llvm examples/hello.kizu
go run ./cmd/kizu build --emit-llvm --opt examples/hello.kizu
go run ./cmd/kizu build --target wasm32-wasi examples/hello.kizu
go run ./cmd/kizu build --target native --libc on --runtime hosted examples/hello.kizu
go run ./cmd/kizu cache status
go run ./cmd/kizu why-rebuild examples/hello.kizu
go run ./cmd/kizu import-c-header examples/c_abi.h
```

## CLI

- `kizu parse <file>` parses a `.kizu` source file.
- `kizu check <file>` runs type, ownership, move, borrow, and arena checks.
- `kizu fmt <file>` prints the current compact AST formatter output.
- `kizu run <file>` executes the file with the interpreter.
- `kizu test <file>` runs one checked Kizu source as a test file.
- `kizu ir [--opt] <file>` prints typed SSA IR.
- `kizu build --emit-llvm [--opt] <file>` emits LLVM IR text.
- `kizu build --target wasm32-wasi [--opt] <file>` emits WASI-compatible WAT.
- `kizu build --target native [--opt] [--libc on|off] [--runtime hosted|freestanding] [--emit exe|obj|llvm] [-o <out>] <file>` links a native executable.
- `kizu cache status` prints local build cache status.
- `kizu cache prune` clears local build cache entries.
- `kizu why-rebuild <file>` explains cache hit or rebuild reasons.
- `kizu import-c-header <file>` converts supported C prototypes to Kizu externs.

`kizu lint` is not implemented.

## Project Documents

- [SPEC.md](SPEC.md): language specification
- [docs/memory-safety.md](docs/memory-safety.md): safe Kizu memory-safety contract
- [examples](examples/README.md): examples catalog
- [tests/conformance](tests/conformance/README.md): reusable conformance manifests
- [docs/stdlib.md](docs/stdlib.md): standard-library builtin registry and migration plan
- [docs/adr](docs/adr): architecture decision records
- [docs/perf.md](docs/perf.md): build and cache performance policy
- [AGENTS.md](AGENTS.md): implementation guidance for Codex agents

## License

Kizu is licensed under the [MIT License](LICENSE).
