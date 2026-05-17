# Kizu

<p align="center">
  <img src="docs/assets/kizu-logo.svg" alt="Kizu logo" width="180">
</p>

Kizu は、明示的で、メモリ安全なシステムプログラミング言語のプロトタイプです。

名前の Kizu は日本語の「傷」に由来します。

> 傷を作らない。傷を隠さない。

Kizu は Rust の安全性の考え方を一部参考にしますが、Rust clone ではありません。
Rust より単純で、safe code では C/C++/Zig より安全で、CI とビルドキャッシュが重くなりにくい言語を探索します。

[English README](README.md)

## 現在の状態

Kizu は Go 製の初期プロトタイプです。

v0.1 の対象は interpreter-first の language core です。
現在の v0.2 baseline では、言語挙動を検証し続けるための最小 stdlib surface と tooling を追加しています。
正となる挙動は引き続き Go 製 interpreter と `kizu check` です。

実装済み language core:

- lexer, parser, AST, CLI
- interpreter
- type checker
- move checker
- local borrow checker
- `arena<T>` / `handle<T>`
- `while`、`break`、`continue`、labeled loop branch、bounded `for`
- unsafe 境界と C ABI 宣言の検査
- 限定的な `comptime` expression / parameter / branch selection
- 低レベル型変換向けの明示 `cast<T>(value)`
- 最小の `!T` と `try` error propagation
- Zig/C-style tag `enum`、tagged `union`、exhaustive `match`
- `std::io::blocking/threaded/failing` と `TaskGroup` structured task model
- `std::channel::Channel<T>` owned message passing
- `std::task::Queue` deterministic deferred task queue
- `std::task::parallel_for` / `std::task::parallel_map` safe data-parallel prototype
- scoped thread、`Atomic<T>`、`Mutex<T>` boundary prototype
- `contract`、`satisfy`、`&Dyn<Contract>`
- 最小の `std::mem`、`std::array::Array<T>`、`std::string::String`、
  `std::map::Map<K, V>`、`std::testing`
- explicit-Io の `std::fs`、`std::path`、`std::io`、`std::process` helper
- `kizu test <file>` single-file test runner

実験的な compiler / tooling:

- typed SSA IR
- LLVM IR text backend
- 上限付きローカルビルドキャッシュと再ビルド理由表示
- WASI-compatible WebAssembly text backend
- extern function 宣言向けの限定的な C header import
- opt-in の IR optimization pipeline

これらは将来の compiler work の土台ですが、まだ言語の正ではありません。
LLVM と WASM は interpreter より限定された subset だけを扱い、native executable generation は未実装です。

現時点で open な v0.2 Issue はありません。将来の compiler migration work は、
明確な受け入れ条件を持つ新しい GitHub Issues から開始します。

まだ実験段階です。構文や実装詳細は、言語設計を検証しながら変わる可能性があります。

## 例

```kizu
fn main() {
    print("hello, kizu");
}
```

interpreter で実行します。

```sh
go run ./cmd/kizu run examples/hello.kizu
```

Kizu の test source は次のように実行します。

```sh
go run ./cmd/kizu test examples/std_testing.kizu
```

prototype の process argument は `--` 以降で渡します。

```sh
go run ./cmd/kizu run examples/std_io_process.kizu -- input.kizu
```

機能ごとの実行例と失敗すべき安全性ルールは
[examples catalog](examples/README.md) にまとめています。
機械判定用の conformance manifest は
[tests/conformance/v0_1.json](tests/conformance/v0_1.json) です。現在は v0.1 language-core と
v0.2 stdlib prototype coverage の両方に再利用しています。
safe code のメモリ安全契約は
[docs/memory-safety.md](docs/memory-safety.md) に明文化しています。

## 開発環境

推奨する開発環境は Nix flake です。

```sh
nix develop
pre-commit install
```

shell には Go、golangci-lint、pre-commit、just、wasmtime が入ります。

## よく使うコマンド

```sh
just --list
just verify
just perf
just perf-cache
just cache-smoke
just wasi-smoke
```

直接実行する場合:

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
go run ./cmd/kizu cache status
go run ./cmd/kizu why-rebuild examples/hello.kizu
go run ./cmd/kizu import-c-header examples/c_abi.h
```

## CLI

- `kizu parse <file>` は `.kizu` source file を parse します。
- `kizu check <file>` は type / ownership / move / borrow / arena check を実行します。
- `kizu fmt <file>` は現在の compact AST formatter output を出力します。
- `kizu run <file>` は interpreter で実行します。
- `kizu test <file>` は check 済みの Kizu source を単一 test file として実行します。
- `kizu ir [--opt] <file>` は typed SSA IR を表示します。
- `kizu build --emit-llvm [--opt] <file>` は LLVM IR text を出力します。
- `kizu build --target wasm32-wasi [--opt] <file>` は WASI-compatible WAT を出力します。
- `kizu cache status` はローカルビルドキャッシュの状態を表示します。
- `kizu cache prune` はローカルビルドキャッシュを削除します。
- `kizu why-rebuild <file>` は cache hit または rebuild 理由を表示します。
- `kizu import-c-header <file>` は対応する C prototype を Kizu extern に変換します。

`kizu lint` は未実装です。

## プロジェクト文書

- [SPEC.md](SPEC.md): 言語仕様
- [docs/memory-safety.md](docs/memory-safety.md): safe Kizu memory-safety contract
- [examples](examples/README.md): examples catalog
- [tests/conformance](tests/conformance/README.md): reusable conformance manifests
- [docs/stdlib.md](docs/stdlib.md): standard-library builtin registry と移行計画
- [docs/adr](docs/adr): Architecture Decision Record
- [docs/perf.md](docs/perf.md): build/cache performance policy
- [AGENTS.md](AGENTS.md): Codex agent 向け実装方針

## License

Kizu は [MIT License](LICENSE) で公開します。
