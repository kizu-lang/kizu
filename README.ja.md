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

`kizu run` は `kizu build --target native` が書き出すのと同じ native 実行ファイルを
作り、それを実行します。2 つのコマンドの違いは「実行するかどうか」だけです。
lowering は 1 本しかないため、同じプログラムが `run` と `build` で違う挙動を示すことは
原理的に起きません(ADR-0083)。プログラムが「どう動くべきか」を定義するのは
conformance manifest であり、特定の実行経路ではありません。

| 機能 | 例の数 | check | run | llvm | wasm |
| --- | ---: | :--: | :--: | :--: | :--: |
| fn / let / struct / literals | 25 | ✅ | 22/25 | 23/25 | 9/25 |
| arithmetic / comparison / logical | 3 | ✅ | ✅ | ✅ | 2/3 |
| while / break / continue / for / label | 6 | ✅ | ✅ | ✅ | 5/6 |
| if / match | 7 | ✅ | 6/7 | 6/7 | 1/7 |
| enum / union | 8 | ✅ | 5/8 | ✅ | ❌ |
| error union `!T` / try / errdefer | 9 | ✅ | 7/9 | ✅ | ❌ |
| move / borrow | 16 | ✅ | 14/16 | 15/16 | 4/16 |
| deinit / defer | 5 | ✅ | ✅ | ✅ | ❌ |
| arena / handle | 6 | ✅ | ✅ | ✅ | ❌ |
| comptime | 2 | ✅ | 1/2 | 1/2 | 1/2 |
| cast / slice / raw pointer / box | 11 | ✅ | 8/11 | 8/11 | 1/11 |
| contract / dyn / generics | 4 | ✅ | 2/4 | 2/4 | ❌ |
| std::array | 10 | ✅ | 7/10 | 9/10 | ❌ |
| std::string | 11 | ✅ | 10/11 | 10/11 | ❌ |
| std::map | 9 | ✅ | 7/9 | 8/9 | ❌ |
| std::mem / allocator | 8 | ✅ | 6/8 | 7/8 | ❌ |
| std::testing | 13 | ✅ | 10/13 | 10/13 | ❌ |
| std::fmt | 3 | ✅ | ✅ | ✅ | ❌ |
| std::fs / path / io / process | 9 | ✅ | 6/9 | 6/9 | ❌ |
| TaskGroup / channel / queue / parallel | 9 | ✅ | 1/9 | 1/9 | ❌ |
| thread / atomic / mutex | 5 | ✅ | ❌ | ❌ | ❌ |
| std::kizu self-describing layer | 11 | ✅ | 9/11 | 10/11 | ❌ |

`✅` はその行の example が全て通ること、分数は一部だけ通ること、`❌` は 1 つも
通らないことを表します。runnable example は 82 件、測定は 2026-08-13 に
`just backend-matrix` で実施しました。backend を触ったら回し直してください。
`run` はプログラムの出力で判定し、`llvm` と `wasm` は lowering が通ったかで判定します。

| 経路 | 通過 |
| --- | --- |
| `kizu check` | 82/82 |
| `kizu run` | 60/82 |
| `kizu build --emit-llvm` | 66/82 |
| `kizu build --target wasm32-wasi` | 17/82 |

`run` が再現できない 22 件は、manifest に `pending` として理由付きで登録してあります。
pending なケースは「今も通らないこと」を検査するので、穴を塞いだ変更は
同じ変更で登録を消すことになります。

埋めるべき順序:

1. **6 件が違う答えを出す。** 3 件は `print` から enum 名が落ち、1 件は mutable borrow
   経由の変更を観測せず、1 件は配列長を誤り、1 件は回復済み error union で
   非ゼロ終了します。失敗しないため呼び出し側が気づけない、3 つの中で最も悪い種類です。
2. **negative example 4 件が違う失敗を報告する。** `Array.get_or_panic` は今も
   メッセージ無しで trap し、failing な `Io` capability は無視されてプログラムが
   成功し、返された error union が surface されません。範囲外 index と slice は
   `index out of bounds` / `range out of bounds` を報告するようになりました(ADR-0084)。
3. **16 件は lowering が未実装。** `std::builtin::task_group` (6)、
   `Channel<T>` と `Atomic<T>` (4)、明示 generics、`dyn` contract method、
   `Box` borrow、`if` 式、scoped thread の worker 値。

language core 周辺の tooling:

- typed SSA IR と opt-in の最適化 pipeline
- 上限付きローカルビルドキャッシュと再ビルド理由表示
- extern function 宣言向けの限定的な C header import
- `std/` の Kizu 標準ライブラリ(`std/src/kizu/` の自己記述 lexer / parser / AST を含む)
- LSP server (`cmd/kizu-lsp`)

interpreter はありません。`kizu test` は `kizu run` が `main` をビルドして実行するのと
同じ経路で test block をビルドして実行します。言語機能の実装は 1 つだけです。

`kizu run` と `kizu test` は host の `clang` と libc を必要とします。native build path が
元から持っていた要件がそのまま及びます。no-libc / freestanding build は
build policy としては受理済みですが、未実装です。

このリポジトリはまだ実験的です。言語設計の検証中は、構文と実装の詳細が変わり得ます。

## 例

```kizu
fn main() {
    print("hello, kizu");
}
```

ビルドして実行します。

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
- `kizu run <file>` は native 実行ファイルを作って実行します。
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
