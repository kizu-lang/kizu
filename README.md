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

Kizu is an early prototype, and it compiles itself. The binary a release ships
is the Kizu compiler under `compiler/`, written in Kizu; the Go implementation
(`internal/` + `cmd/kizu`) is the seed that builds it and the oracle both
implementations are diffed against (ADR-0130). `TestSelfhostBootstrap` requires
the self-built compiler to reproduce itself byte for byte.

This repository is still experimental. Syntax and implementation details can
change while the language design is being tested.

### What runs

`kizu run` builds the same native executable `kizu build --target native`
writes, and then runs it. The only difference between the two commands is
whether the result is executed, so a program cannot behave one way under `run`
and another way under `build` -- there is one lowering, not two (ADR-0083).
What a program is *supposed* to do is written at the end of the program itself,
not in any one execution path.

| Feature | Examples | check | run | llvm | wasm | wasm-opt | wasm-bin | browser |
| --- | ---: | :--: | :--: | :--: | :--: | :--: | :--: | :--: |
| fn / let / struct / literals | 41 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 40/41 |
| arithmetic / comparison / logical | 3 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| while / break / continue / for / label | 10 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| if / match | 15 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| enum / union | 15 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 14/15 |
| error union `!T` / try / errdefer | 45 | ✅ | ✅ | ✅ | 27/45 | 27/45 | 27/45 | 24/45 |
| optional `?T` / orelse / capture | 24 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 22/24 |
| move / borrow | 52 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| deinit / defer | 20 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| arena / handle | 10 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| comptime / reflection | 13 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cast / slice / stack buffer / box | 13 | ✅ | ✅ | ✅ | 12/13 | 12/13 | 12/13 | 12/13 |
| unsafe / raw pointer / extern C | 4 | ✅ | ✅ | ✅ | 1/4 | 1/4 | 1/4 | 1/4 |
| contract / generics | 14 | ✅ | ✅ | ✅ | 13/14 | 13/14 | 13/14 | 13/14 |
| std::array | 17 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::string | 32 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::map | 13 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::mem / allocator | 20 | ✅ | ✅ | ✅ | 19/20 | 19/20 | 19/20 | 18/20 |
| std::json | 14 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::sort | 1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::fmt | 6 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::testing | 1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::fs / path / io / process | 27 | ✅ | ✅ | ✅ | 10/27 | 10/27 | 10/27 | 3/27 |
| std::net / http | 19 | ✅ | ✅ | ✅ | 2/19 | 2/19 | 2/19 | 2/19 |
| async / coro | 2 | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |

`✅` means every example in the row passes, a fraction means only some do, and
`❌` means none do. A row counts every example that declares one of its feature
tags, so an example appears in more than one row. 162 runnable examples,
measured on 2026-09-01 with `just backend-matrix` -- re-run it after touching a
backend. `run`, `wasm`, `wasm-opt`, `wasm-bin`, and `browser` are judged on the
program's output: `run` executes the native build; `wasm` and `wasm-opt` load
the default and optimized WAT with `wasmtime`; `wasm-bin` loads the binary
module there; `browser` loads the browser binary with the JavaScript host
adapter. `llvm` is judged on whether lowering succeeded, because `run` already
builds the native target from the same text.

| Route | Passing |
| --- | --- |
| `kizu check` | 162/162 |
| `kizu run` | 162/162 |
| `kizu build --emit-llvm` | 162/162 |
| `kizu build --target wasm32-wasi` (WAT) | 142/162 |
| `kizu build --target wasm32-wasi --opt` (WAT) | 142/162 |
| `kizu build --target wasm32-wasi --emit wasm -o <out>` | 142/162 |
| `kizu build --target wasm32-browser --emit wasm -o <out>` | 135/162 |

The native route has no pending runnable example. WASI remains a target subset;
its remaining 20 examples are explicit target-unsupported capabilities: 16
`std::net`, two extern C, one evented I/O, and one coroutine example. The
default WAT, optimized WAT, and binary routes have no remaining lowering
failure, runtime-only refusal, or output mismatch. The browser route likewise
has no lowering failure or output mismatch: all remaining 27 are explicit
target-unsupported capabilities. The browser column is broad JavaScript-engine
coverage; the real-page fixture is `tests/browser/smoke.html`.
In addition to the standalone binary, one command writes adjacent `app.wasm`
and `app.mjs` browser artifacts. Importing the module does not start the
program.

```sh
kizu build --target wasm32-browser --emit esm -o dist app.kizu
```

Tooling around the language core:

- typed SSA IR with an opt-in optimization pipeline
- bounded local build cache, content-addressed by what an artifact is made of
- limited C header import for extern function declarations
- the Kizu standard library in `lib/kizu/std/` and browser host runtime in
  `lib/kizu/browser/`
- an LSP server (`cmd/kizu-lsp`)

There is no interpreter. `kizu test` builds and runs test blocks the same way
`kizu run` builds and runs `main`, so a language feature has exactly one
implementation.

`kizu run` and `kizu test` need host `clang` and libc, the same requirement the
native build path already had. The emitted LLVM IR uses opaque pointers, so
`clang` must be 15 or newer; clang 14 rejects it with `expected type`. no-libc /
freestanding builds are part of the accepted build policy but are not
implemented.

## Roadmap

The table above measures what runs. This is what is planned, in progress, or
deliberately excluded, so the two are not confused.

| Feature | State |
| --- | --- |
| threads for parallel work | **planned.** The earlier API was withdrawn because it had checker rules but no lowering and no runtime. ADR-0025 records the acceptance criteria it must meet to return, and the first one is that `kizu run` executes it. Coroutines (`std::coro`) and an evented `Io` are in, and they are concurrency on one thread, not parallelism (ADR-0145, ADR-0146) |
| wasm application path | **in progress.** Portable files and packages run as WASI and browser binaries; compile-time selection between native/WASI/browser host adapters remains |
| raw pointer runtime operations | **check-only.** `pointer_policy.kizu` and `raw_pointer_deref.kizu` are checked but not executed |
| float literals and float arithmetic | **not started.** `f32` / `f64` name a type; `1.5` does not lex as one literal |
| type alias | **not started** |
| `kizu lint` | **not started** |
| TLS / HTTPS, middleware | **not started.** `std::http` is HTTP/1 over plaintext TCP; middleware waits on closures |
| full generics | **not planned as such.** Explicit static arguments only, no inference, no bounds, no HKT (ADR-0066) |
| `async fn` / `await` syntax | **not adopted.** Function coloring is the cost this language does not pay (ADR-0025) |
| Rust `Send` / `Sync` traits | **not adopted.** Whatever replaces them must be one rule users can read, not a hand-written whitelist (ADR-0025) |

A feature is "implemented" here only when a conformance case runs it and checks
its output. Rules that only a checker enforces are not counted as features --
that is the mistake ADR-0025 exists to record.

## Example

```kizu
fn main() {
    print("hello, kizu");
}
```

```sh
go run ./cmd/kizu run examples/hello.kizu
```

A test block runs the same way, without `main`:

```kizu
test "std testing assertions" {
    std::testing::expect(true);
}
```

```sh
go run ./cmd/kizu test examples/std_testing.kizu
go run ./cmd/kizu run examples/std_io_process.kizu -- input.kizu   # process args after --
```

[`examples/`](examples/README.md) holds one readable program per feature, and
`examples/negative/` one per safety rule the language refuses. Every example
ends with the case it declares -- the command to run it with and what that has
to produce -- which is what the conformance test reads.
The safe-code memory-safety contract is documented in
[docs/memory-safety.md](docs/memory-safety.md).

## Getting a Binary

Prebuilt binaries are attached to
[GitHub Releases](https://github.com/kizu-lang/kizu/releases); each names its
version with `kizu version`, so an old binary identifies itself instead of
producing confusing parse errors against newer sources. The flake builds the
same layout locally:

```sh
nix build   # ./result/bin/kizu with its library tree in ./result/lib/kizu
```

Development runs the Go seed from source instead, so a compiler change is one
`go run` away:

```sh
go run ./cmd/kizu run examples/hello.kizu
```

## Development Environment

The recommended development environment is the Nix flake. The shell includes
Go, golangci-lint, pre-commit, just, wasmtime, and Node.js.

```sh
nix develop
pre-commit install
```

`just --list` shows every recipe. The ones used most:

```sh
just verify          # gofmt + go test ./... + golangci-lint
just check           # pre-commit run --all-files, the commit gate
just selfhost        # check and test the Kizu compiler under compiler/
just backend-matrix  # regenerate the table above
just perf            # build and cache timings
just wasi-smoke      # run the wasm examples under wasmtime
```

## CLI

- `kizu parse <file>` parses a `.kizu` source file.
- `kizu check <file-or-package>` runs type, ownership, move, borrow, and arena checks.
- `kizu run <file-or-package>` builds a native executable and runs it.
- `kizu test <file-or-package>` runs checked top-level test blocks without invoking `main`.
- `kizu fmt [--write|-w] <file>` prints or writes canonical token formatter output. It is not a source-preserving formatter: line comments are kept, but the canonical form puts each on its own line, so a comment trailing code moves to the next line.
- `kizu init [path]` scaffolds a package.
- `kizu ir [--opt] <file>` prints typed SSA IR.
- `kizu build --emit-llvm [--opt] <file>` emits LLVM IR text.
- `kizu build --target wasm32-wasi [--opt] [--emit wat] [-o <out>] <file|package>` emits WASI-compatible WAT to stdout, or to `-o` when supplied.
- `kizu build --target wasm32-wasi [--opt] --emit wasm -o <out> <file|package>` writes a binary `.wasm`; binary output never goes to the terminal implicitly.
- `kizu build --target wasm32-browser [--opt] [--emit wat] [-o <out>] <file|package>` emits browser-hosted WAT for inspection.
- `kizu build --target wasm32-browser [--opt] --emit wasm -o <out> <file|package>` writes the browser `.wasm`; [`docs/wasm-browser.md`](docs/wasm-browser.md) defines its host adapter and capability boundary.
- `kizu build --target native [--opt] [--triple <triple>] [--cpu <cpu>] [--abi <abi>] [--libc on|off] [--runtime hosted|freestanding] [--emit exe|obj|llvm] [--linker clang] [-o <out>] <file>` links a native executable.
- `kizu cache status` / `kizu cache prune` show and clear the local build cache.
- `kizu import-c-header <file>` converts supported C prototypes to Kizu externs.
- `kizu version` prints what the binary is.

`kizu lint` is not implemented.

## Project Documents

- [docs/architecture.md](docs/architecture.md): architecture overview (in Japanese; start here for onboarding)
- [docs/wasm-browser.md](docs/wasm-browser.md): browser WebAssembly ABI, adapter, and target capabilities
- [SPEC.md](SPEC.md): language specification
- [docs/principles.md](docs/principles.md): the design principles every decision is checked against
- [docs/style.md](docs/style.md): how std chooses the shape of an API
- [docs/memory-safety.md](docs/memory-safety.md): safe Kizu memory-safety contract
- [docs/std/](docs/std/README.md): standard-library API reference
- [docs/tutorial/](docs/tutorial/README.md): building one whole thing, start to finish
- [examples](examples/README.md): readable programs per feature, and the refusals in `negative/`
- [docs/adr](docs/adr): architecture decision records
- [docs/language-gaps.md](docs/language-gaps.md): what could not be written yet, and the workaround used
- [docs/stdlib.md](docs/stdlib.md): the trusted-builtin boundary and the rules for new std APIs
- [docs/perf.md](docs/perf.md): build and cache performance policy
- [AGENTS.md](AGENTS.md): implementation rules for contributors and coding agents

## License

Kizu is licensed under the [MIT License](LICENSE).
