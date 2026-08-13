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

`kizu run` builds the same native executable `kizu build --target native`
writes, and then runs it. The only difference between the two commands is
whether the result is executed, so a program cannot behave one way under `run`
and another way under `build` -- there is one lowering, not two (ADR-0083).
What a program is *supposed* to do is defined by the conformance manifests, not
by any one execution path.

| Feature | Examples | check | run | llvm | wasm |
| --- | ---: | :--: | :--: | :--: | :--: |
| fn / let / struct / literals | 25 | ✅ | 22/25 | 23/25 | 9/25 |
| arithmetic / comparison / logical | 3 | ✅ | ✅ | ✅ | 2/3 |
| while / break / continue / for / label | 6 | ✅ | ✅ | ✅ | 5/6 |
| if / match | 7 | ✅ | 6/7 | 6/7 | 1/7 |
| enum / union | 8 | ✅ | ✅ | ✅ | ❌ |
| error union `!T` / try / errdefer | 9 | ✅ | ✅ | ✅ | ❌ |
| move / borrow | 16 | ✅ | 14/16 | 15/16 | 2/16 |
| deinit / defer | 5 | ✅ | ✅ | ✅ | ❌ |
| arena / handle | 6 | ✅ | ✅ | ✅ | ❌ |
| comptime | 2 | ✅ | ✅ | ✅ | 1/2 |
| cast / slice / raw pointer / box | 11 | ✅ | 8/11 | 8/11 | 1/11 |
| contract / dyn / generics | 5 | ✅ | 4/5 | 4/5 | 1/5 |
| std::array | 10 | ✅ | 9/10 | 9/10 | ❌ |
| std::string | 11 | ✅ | 10/11 | 10/11 | ❌ |
| std::map | 9 | ✅ | 8/9 | 8/9 | ❌ |
| std::mem / allocator | 8 | ✅ | 7/8 | 7/8 | ❌ |
| std::testing | 13 | ✅ | 11/13 | 11/13 | ❌ |
| std::fmt | 3 | ✅ | ✅ | ✅ | ❌ |
| std::fs / path / io / process | 9 | ✅ | 6/9 | 6/9 | ❌ |
| TaskGroup / channel / queue / parallel | 9 | ✅ | 1/9 | 1/9 | ❌ |
| thread / atomic / mutex | 5 | ✅ | ❌ | ❌ | ❌ |
| std::kizu self-describing layer | 11 | ✅ | 10/11 | 10/11 | ❌ |

`✅` means every example in the row passes, a fraction means only some do, and
`❌` means none do. 83 runnable examples, measured on 2026-08-13 with
`just backend-matrix` -- re-run it after touching a backend. `run` and `wasm`
are judged on the program's output: `run` executes the native build, `wasm`
loads the emitted module with `wasmtime`. `llvm` is judged on whether lowering
succeeded, because `run` already builds the native target from the same text.

| Route | Passing |
| --- | --- |
| `kizu check` | 83/83 |
| `kizu run` | 67/83 |
| `kizu build --emit-llvm` | 68/83 |
| `kizu build --target wasm32-wasi` | 16/83 |

The 16 examples `run` cannot reproduce are registered in the manifests with a
`pending` reason. A pending case is tested for *still failing*, so closing a gap
forces its entry to be removed in the same change.

What is missing, in the order it should be closed:

1. **One example computes the wrong answer.** It does not observe a mutation
   made through a mutable borrow. This is the worst of the three groups: it does
   not fail, so nothing tells the caller the answer is wrong.
2. **Five negative examples report the wrong failure.** A failing `Io`
   capability is ignored so the program succeeds (2), a returned error union is
   not surfaced, and two report a message other than the one the manifest wants.
3. **Fourteen examples have no lowering yet.** `std::builtin::task_group` (6),
   `Channel<T>` (3) and `Atomic<T>`, a `dyn` contract method, a `Box` borrow,
   `if` as an expression, and the scoped-thread worker value. One more compares
   a borrow against a value in LLVM and is rejected there.

Tooling around the language core:

- typed SSA IR with an opt-in optimization pipeline
- bounded local build cache and rebuild explanations
- limited C header import for extern function declarations
- the Kizu standard library in `std/`, including the self-describing
  `std/src/kizu/` lexer, parser, and AST
- an LSP server (`cmd/kizu-lsp`)

There is no interpreter. `kizu test` builds and runs test blocks the same way
`kizu run` builds and runs `main`, so a language feature has exactly one
implementation.

`kizu run` and `kizu test` need host `clang` and libc, the same requirement the
native build path already had. no-libc / freestanding builds are part of the
accepted build policy but are not implemented.

This repository is still experimental. Syntax and implementation details can
change while the language design is being tested.

## Example

```kizu
fn main() {
    print("hello, kizu");
}
```

Build and run it:

```sh
go run ./cmd/kizu run examples/hello.kizu
```

Run one Kizu test source:

```kizu
test "std testing assertions" {
    std::testing::expect(true);
}
```

```sh
go run ./cmd/kizu test examples/std_testing.kizu
```

Pass prototype process arguments with `--`:

```sh
go run ./cmd/kizu run examples/std_io_process.kizu -- input.kizu
```

See the [examples catalog](examples/README.md) for runnable feature
examples and negative safety-rule examples. The machine-readable conformance
manifests are [tests/conformance/v0_1.json](tests/conformance/v0_1.json) for the
language core and [tests/conformance/v0_2.json](tests/conformance/v0_2.json) for
the stdlib prototypes.
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
go run ./cmd/kizu build --target native --libc on --runtime hosted --linker clang examples/hello.kizu
go run ./cmd/kizu cache status
go run ./cmd/kizu why-rebuild examples/hello.kizu
go run ./cmd/kizu import-c-header examples/c_abi.h
```

## CLI

- `kizu parse <file>` parses a `.kizu` source file.
- `kizu check <file>` runs type, ownership, move, borrow, and arena checks.
- `kizu fmt [--write|-w] <file>` prints or writes canonical token formatter output. `--write` currently rejects files with line comments until comment trivia is preserved.
- `kizu run <file>` builds a native executable and runs it.
- `kizu test <file-or-package>` runs checked top-level test blocks without invoking `main`.
- `kizu ir [--opt] <file>` prints typed SSA IR.
- `kizu build --emit-llvm [--opt] <file>` emits LLVM IR text.
- `kizu build --target wasm32-wasi [--opt] <file>` emits WASI-compatible WAT.
- `kizu build --target native [--opt] [--triple <triple>] [--cpu <cpu>] [--abi <abi>] [--libc on|off] [--runtime hosted|freestanding] [--emit exe|obj|llvm] [--linker clang] [-o <out>] <file>` links a native executable.
- `kizu cache status` prints local build cache status.
- `kizu cache prune` clears local build cache entries.
- `kizu why-rebuild <file>` explains cache hit or rebuild reasons.
- `kizu import-c-header <file>` converts supported C prototypes to Kizu externs.

`kizu lint` is not implemented.

## Project Documents

- [docs/architecture.md](docs/architecture.md): architecture overview (in Japanese; start here for onboarding)
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
