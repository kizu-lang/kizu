# Kizu

<p align="center">
  <img src="docs/assets/kizu-logo.svg" alt="Kizu logo" width="180">
</p>

Kizu is a small, explicit, memory-safe systems programming language prototype.

The name comes from the Japanese word "kizu", meaning "wound" or "scratch".

> Do not create wounds. Do not hide wounds.

Kizu borrows some safety ideas from Rust, but it is not a Rust clone. The goal is
to explore a language that is simpler than Rust, safer than C/C++/Zig in safe
code, and less likely to grow heavy CI and build caches.

[日本語版 README](README.ja.md)

## Status

Kizu is an early prototype implemented in Go.

Implemented phases:

- lexer, parser, AST, and CLI
- interpreter
- type checker
- move checker
- local borrow checker
- `arena<T>` / `handle<T>`
- typed SSA IR
- LLVM IR text backend
- bounded local build cache and rebuild explanations
- WASI-compatible WebAssembly text backend
- unsafe boundary and C ABI declaration checks
- limited `comptime` expressions, parameters, and branch selection
- limited C header import for extern function declarations
- opt-in IR optimization pipeline
- explicit `cast<T>(value)` for low-level type conversions
- minimal `result<T>` and `try` error propagation

This repository is still experimental. Syntax and implementation details can
change while the language design is being tested.

## Example

```kizu
fn main() {
    print("hello, kizu")
}
```

Run it with the interpreter:

```sh
go run ./cmd/kizu run examples/hello.kizu
```

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
go run ./cmd/kizu ir examples/hello.kizu
go run ./cmd/kizu ir --opt examples/hello.kizu
go run ./cmd/kizu build --emit-llvm examples/hello.kizu
go run ./cmd/kizu build --emit-llvm --opt examples/hello.kizu
go run ./cmd/kizu build --target wasm32-wasi examples/hello.kizu
go run ./cmd/kizu cache status
go run ./cmd/kizu why-rebuild examples/hello.kizu
go run ./cmd/kizu import-c-header examples/tiny.h
```

## CLI

- `kizu parse <file>` parses a `.kizu` source file.
- `kizu check <file>` runs type, ownership, move, borrow, and arena checks.
- `kizu fmt <file>` prints stable formatted source.
- `kizu run <file>` executes the file with the interpreter.
- `kizu ir [--opt] <file>` prints typed SSA IR.
- `kizu build --emit-llvm [--opt] <file>` emits LLVM IR text.
- `kizu build --target wasm32-wasi [--opt] <file>` emits WASI-compatible WAT.
- `kizu cache status` prints local build cache status.
- `kizu cache prune` clears local build cache entries.
- `kizu why-rebuild <file>` explains cache hit or rebuild reasons.
- `kizu import-c-header <file>` converts supported C prototypes to Kizu externs.

## Project Documents

- [SPEC.md](SPEC.md): language specification
- [PHASES.md](PHASES.md): implementation phase tracker
- [docs/adr](docs/adr): architecture decision records
- [docs/perf.md](docs/perf.md): build and cache performance policy
- [AGENTS.md](AGENTS.md): implementation guidance for Codex agents

## License

Kizu is licensed under the [MIT License](LICENSE).
