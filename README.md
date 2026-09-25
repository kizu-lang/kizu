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
| fn / let / struct / literals | 51 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 50/51 |
| arithmetic / bitwise / float | 11 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| while / break / continue / for / label | 10 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| if / match | 19 | ✅ | ✅ | ✅ | 18/19 | 18/19 | 18/19 | 16/19 |
| enum / union | 18 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 15/18 |
| error union `!T` / try / errdefer | 59 | ✅ | ✅ | ✅ | 38/59 | 38/59 | 38/59 | 35/59 |
| optional `?T` / orelse / capture | 28 | ✅ | ✅ | ✅ | 27/28 | 27/28 | 27/28 | 25/28 |
| move / borrow | 70 | ✅ | ✅ | ✅ | 68/70 | 68/70 | 68/70 | 68/70 |
| deinit / defer | 25 | ✅ | ✅ | ✅ | 24/25 | 24/25 | 24/25 | 24/25 |
| arena / handle | 10 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| comptime / reflection | 20 | ✅ | ✅ | ✅ | 18/20 | 18/20 | 18/20 | 18/20 |
| cast / slice / stack buffer / box | 17 | ✅ | ✅ | ✅ | 16/17 | 16/17 | 16/17 | 16/17 |
| unsafe / raw pointer / extern C | 11 | ✅ | ✅ | ✅ | 2/11 | 2/11 | 2/11 | 2/11 |
| contract / generics | 24 | ✅ | ✅ | ✅ | 23/24 | 23/24 | 23/24 | 20/24 |
| std::array | 26 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::string | 32 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::map | 17 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 16/17 |
| std::mem / allocator | 22 | ✅ | ✅ | ✅ | 21/22 | 21/22 | 21/22 | 20/22 |
| std::json | 18 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 15/18 |
| std::compress | 3 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::sort | 1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::float | 1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::math | 5 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::rand | 1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::time | 1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::date | 2 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::fmt | 7 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::testing | 1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| std::fs / path / io / process | 33 | ✅ | ✅ | ✅ | 14/33 | 14/33 | 14/33 | 4/33 |
| std::net / http | 23 | ✅ | ✅ | ✅ | 4/23 | 4/23 | 4/23 | 4/23 |
| async / coro | 2 | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| std::thread | 2 | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |

`✅` means every example in the row passes, a fraction means only some do, and
`❌` means none do. A row counts every example that declares one of its feature
tags, so an example appears in more than one row. `just backend-matrix`
regenerates the table; re-run it after touching a backend. `run`, `wasm`,
`wasm-opt`, `wasm-bin`, and `browser` are judged on the program's output: `run`
executes the native build; `wasm` and `wasm-opt` load the default and optimized
WAT with `wasmtime`; `wasm-bin` loads the binary module there; `browser` loads
the browser binary with the JavaScript host adapter. `llvm` is judged on whether
lowering succeeded, because `run` already builds the native target from the
same text.

Every runnable example passes `check`, `run`, and `llvm`. The Wasm routes have
no lowering failure. What they refuse is a capability the target does not
have -- `std::net`, extern C, evented I/O, coroutines, threads, and on the
browser also `std::fs` and process arguments -- with an explicit
target-unsupported error. The two examples that differ in output print which
target they were built for (`else_if.kizu`, `target_os.kizu`), so they differ
by design. The browser column is broad JavaScript-engine
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
| threads for parallel work | **in progress.** `std::thread::Pool` runs one function over the non-overlapping chunks of a slice on several threads and returns after every chunk ran (native only). `each_lane` does the same for lanes whose elements are a fixed distance apart, such as a matrix's columns. A worker fails with a member of the set the call names, and the first failure is what the call returns (ADR-0025). Coroutines (`std::coro`) and an evented `Io` are concurrency on one thread, not parallelism (ADR-0145, ADR-0146) |
| wasm beyond the current target subsets | **in progress.** Every example the Wasm routes do not run is refused as a capability the target lacks |
| raw pointer runtime operations | **check-only.** `pointer_policy.kizu` and `raw_pointer_deref.kizu` are checked but not executed |
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
- `kizu fmt [--write|-w] <file>` prints or writes the formatter's output. It settles spacing, indentation, and the trailing comma of a multi-line declaration; line breaks stay where the author put them, so a block written on one line stays on one line, one blank line between statements is kept, and a comment keeps its line and its column.
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
