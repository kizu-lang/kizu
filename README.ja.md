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

Kizu は Go 製の初期プロトタイプです。正となる挙動は interpreter です。
`kizu check` と `kizu run` が正しい挙動を定義し、下の表に出てくる example は
すべて conformance を通っています。backend はその subset を受け付けます。

| 機能 | 例の数 | interp | LLVM | native | WASM |
| --- | ---: | :--: | :--: | :--: | :--: |
| fn / let / struct / literals | 25 | ✅ | 23/25 | 22/25 | 9/25 |
| arithmetic / comparison / logical | 3 | ✅ | ✅ | ✅ | 2/3 |
| while / break / continue / for / label | 6 | ✅ | ✅ | ✅ | 5/6 |
| if / match | 7 | ✅ | 6/7 | 6/7 | 1/7 |
| enum / union | 8 | ✅ | ✅ | 5/8 | ❌ |
| error union `!T` / try / errdefer | 9 | ✅ | ✅ | 7/9 | ❌ |
| move / borrow | 16 | ✅ | 15/16 | 14/16 | 4/16 |
| deinit / defer | 5 | ✅ | ✅ | ✅ | ❌ |
| arena / handle | 6 | ✅ | ✅ | ✅ | ❌ |
| comptime | 2 | ✅ | 1/2 | 1/2 | 1/2 |
| cast / slice / raw pointer / box | 11 | ✅ | 8/11 | 8/11 | 1/11 |
| contract / dyn / generics | 4 | ✅ | 2/4 | 2/4 | ❌ |
| std::array | 10 | ✅ | 9/10 | 7/10 | ❌ |
| std::string | 11 | ✅ | 10/11 | 10/11 | ❌ |
| std::map | 9 | ✅ | 8/9 | 7/9 | ❌ |
| std::mem / allocator | 8 | ✅ | 7/8 | 6/8 | ❌ |
| std::testing | 13 | ✅ | 10/13 | 10/13 | ❌ |
| std::fmt | 3 | ✅ | ✅ | ✅ | ❌ |
| std::fs / path / io / process | 9 | ✅ | 6/9 | 6/9 | ❌ |
| TaskGroup / channel / queue / parallel | 9 | ✅ | 1/9 | 1/9 | ❌ |
| thread / atomic / mutex | 5 | ✅ | ❌ | ❌ | ❌ |
| std::kizu self-describing layer | 11 | ✅ | 10/11 | 9/11 | ❌ |

`✅` はその行の example が全て通ること、分数は一部だけ通ること、`❌` は
1 つも通らないことを表します。runnable example は 82 件、測定は 2026-08-12 に
`go run ./scripts/backend-matrix`(または `just backend-matrix`)で実施しました。
backend を触ったら回し直してください。native 列は link したバイナリを実行して
interpreter の出力と比較するため「一致するか」を表します。LLVM と WASM 列は
lowering が通ったかどうかだけを表します。

| 経路 | 通過 |
| --- | --- |
| `kizu check` + `kizu run` (interpreter) | 82/82 |
| `kizu build --emit-llvm` | 66/82 |
| `kizu build --target native` | 60/82 |
| `kizu build --target wasm32-wasi` | 17/82 |

6 件は LLVM に lower して link も通りますが、実行結果が interpreter と食い違います。
上の native 列ではそれらを失敗として数えています。

backend が受け付けないもの:

- LLVM / native: `std::builtin::task_group` (6 件)、明示 generics と
  `Channel<T>` / `Atomic<T>` の typed call (5 件)、それと `if` の式利用、
  未知の `borrow` / `write` method、`&i64` と `i64` の比較が各 1 件。
- WASM: `slice.len` (28 件)、`unary.!` (5 件)、`struct.new` (4 件)、
  `error.ok` (4 件)、`union.load` / `error.try` / 整数以外の const (各 2 件)。
  加えて上の IR 段階の穴も同じく効きます。
- link 成功後の native: `enum.kizu` / `std_array_token_list.kizu` /
  `std_map_symbol_table.kizu` は `print` から enum 名が落ち、`mutable_borrow.kizu` は
  変更前の値を出力し、`std_array.kizu` は長さを誤り、`error_union_try.kizu` は exit 2。

language core 周辺の tooling:

- typed SSA IR と opt-in の最適化 pipeline
- 上限付きローカルビルドキャッシュと再ビルド理由表示
- extern function 宣言向けの限定的な C header import
- `std/` の Kizu 標準ライブラリ(`std/src/kizu/` の自己記述 lexer / parser / AST を含む)
- LSP server (`cmd/kizu-lsp`)

native 経路は host の `clang` と libc を使います。no-libc / freestanding build は
build policy としては受理済みですが、未実装です。

このリポジトリはまだ実験的です。言語設計の検証中は、構文と実装の詳細が変わり得ます。

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
language core が [tests/conformance/v0_1.json](tests/conformance/v0_1.json)、
stdlib prototype が [tests/conformance/v0_2.json](tests/conformance/v0_2.json) です。
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
go run ./cmd/kizu build --target native --libc on --runtime hosted --linker clang examples/hello.kizu
go run ./cmd/kizu cache status
go run ./cmd/kizu why-rebuild examples/hello.kizu
go run ./cmd/kizu import-c-header examples/c_abi.h
```

## CLI

- `kizu parse <file>` は `.kizu` source file を parse します。
- `kizu check <file>` は type / ownership / move / borrow / arena check を実行します。
- `kizu fmt [--write|-w] <file>` は canonical token formatter output を出力、または書き込みます。comment trivia preservation までは `--write` は line comment を含む file を拒否します。
- `kizu run <file>` は interpreter で実行します。
- `kizu test <file>` は check 済みの Kizu source を単一 test file として実行します。
- `kizu ir [--opt] <file>` は typed SSA IR を表示します。
- `kizu build --emit-llvm [--opt] <file>` は LLVM IR text を出力します。
- `kizu build --target wasm32-wasi [--opt] <file>` は WASI-compatible WAT を出力します。
- `kizu build --target native [--opt] [--triple <triple>] [--cpu <cpu>] [--abi <abi>] [--libc on|off] [--runtime hosted|freestanding] [--emit exe|obj|llvm] [--linker clang] [-o <out>] <file>` は native executable を link します。
- `kizu cache status` はローカルビルドキャッシュの状態を表示します。
- `kizu cache prune` はローカルビルドキャッシュを削除します。
- `kizu why-rebuild <file>` は cache hit または rebuild 理由を表示します。
- `kizu import-c-header <file>` は対応する C prototype を Kizu extern に変換します。

`kizu lint` は未実装です。

## プロジェクト文書

- [docs/architecture.md](docs/architecture.md): アーキテクチャ概観(オンボーディングはここから)
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
