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
プログラム自身の末尾に書かれた case であり、特定の実行経路ではありません。

| 機能 | 例の数 | check | run | llvm | wasm |
| --- | ---: | :--: | :--: | :--: | :--: |
| fn / let / struct / literals | 34 | ✅ | ✅ | ✅ | 12/34 |
| arithmetic / comparison / logical | 3 | ✅ | ✅ | ✅ | ✅ |
| while / break / continue / for / label | 10 | ✅ | ✅ | ✅ | 9/10 |
| if / match | 13 | ✅ | ✅ | ✅ | 2/13 |
| enum / union | 15 | ✅ | ✅ | ✅ | ❌ |
| error union `!T` / try / errdefer | 18 | ✅ | ✅ | ✅ | ❌ |
| move / borrow | 41 | ✅ | ✅ | ✅ | 2/41 |
| deinit / defer | 18 | ✅ | ✅ | ✅ | ❌ |
| arena / handle | 9 | ✅ | ✅ | ✅ | ❌ |
| comptime | 8 | ✅ | ✅ | ✅ | 1/8 |
| cast / slice / raw pointer / box | 8 | ✅ | ✅ | ✅ | 1/8 |
| contract / generics | 8 | ✅ | ✅ | ✅ | 1/8 |
| std::array | 14 | ✅ | ✅ | ✅ | ❌ |
| std::string | 29 | ✅ | ✅ | ✅ | ❌ |
| std::map | 10 | ✅ | ✅ | ✅ | ❌ |
| std::mem / allocator | 13 | ✅ | ✅ | ✅ | ❌ |
| std::testing | 1 | ✅ | ✅ | ✅ | ❌ |
| std::fmt | 5 | ✅ | ✅ | ✅ | ❌ |
| std::fs / path / io / process | 6 | ✅ | ✅ | ✅ | ❌ |

`✅` はその行の example が全て通ること、分数は一部だけ通ること、`❌` は 1 つも
通らないことを表します。runnable example は 129 件、測定は 2026-08-25 に
`just backend-matrix` で実施しました。backend を触ったら回し直してください。
`run` と `wasm` はプログラムの出力で判定します。`run` は native build を実行し、
`wasm` は出力した module を `wasmtime` で読み込みます。`llvm` は lowering が
通ったかで判定します —— native target は `run` が同じ text から build するためです。

| 経路 | 通過 |
| --- | --- |
| `kizu check` | 129/129 |
| `kizu run` | 129/129 |
| `kizu build --emit-llvm` | 129/129 |
| `kizu build --target wasm32-wasi` | 21/129 |

native 経路に pending の runnable example はありません。WASI はまだ target subset
であり、未対応 lowering と出力差分は `just backend-matrix` が報告します。

language core 周辺の tooling:

- typed SSA IR と opt-in の最適化 pipeline
- 上限付きローカルビルドキャッシュと再ビルド理由表示
- extern function 宣言向けの限定的な C header import
- `std/` の Kizu 標準ライブラリ
- LSP server (`cmd/kizu-lsp`)

interpreter はありません。`kizu test` は `kizu run` が `main` をビルドして実行するのと
同じ経路で test block をビルドして実行します。言語機能の実装は 1 つだけです。

`kizu run` と `kizu test` は host の `clang` と libc を必要とします。native build path が
元から持っていた要件がそのまま及びます。no-libc / freestanding build は
build policy としては受理済みですが、未実装です。

このリポジトリはまだ実験的です。言語設計の検証中は、構文と実装の詳細が変わり得ます。

## Roadmap

上の表は「今動くもの」を測った結果です。ここには予定・進行中・意図的に採らないものを
書きます。両者を混同しないためです。

| 機能 | 状態 |
| --- | --- |
| 並列処理のための thread | **予定。** 以前の API は checker rule だけを持ち lowering も runtime も無かったため撤回しました。戻すための受け入れ条件は ADR-0025 にあり、その第 1 条件は `kizu run` で実行できることです |
| 現在の subset を超える wasm backend | **進行中。** 129 件中 21 件が load して動きます |
| raw pointer の実行時操作 | **check のみ。** `pointer_policy.kizu` と `raw_pointer_deref.kizu` は検査だけで実行しません |
| float literal と float 演算 | **未着手。** `f32` / `f64` は型名として存在しますが、`1.5` は 1 つの literal として字句解析されません |
| type alias | **未着手** |
| `kizu lint` | **未着手** |
| full generics | **その形では予定しません。** 明示的な static 引数のみ。推論・bounds・HKT は入れません(ADR-0066) |
| `async fn` / `await` 構文 | **採用しません。** function coloring はこの言語が払わないコストです(ADR-0025) |
| Rust の `Send` / `Sync` trait | **採用しません。** 代わりに置くものは、ユーザーが読める 1 つの規則であって、手書きの whitelist ではありません(ADR-0025) |
| self-host compiler | **移植中。** 一括 cutover までは Go だけを shipping する |

ここで「実装済み」と呼ぶのは、conformance case がそれを実行して出力を検査している
場合だけです。checker だけが強制する規則は機能に数えません。それが ADR-0025 の
記録する失敗そのものだからです。

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

[`examples/`](examples/README.md) は機能ごとに読めるプログラムを 1 つずつ、
`examples/negative/` は言語が拒否する規則を 1 つずつ持ちます。
すべての例は末尾に自分の case -- どのコマンドで実行し、何を出力すべきか -- を
書いており、conformance test はそれを読みます。
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
- `kizu import-c-header <file>` は対応する C prototype を Kizu extern に変換します。

`kizu lint` は未実装です。

## プロジェクト文書

- [docs/architecture.md](docs/architecture.md): アーキテクチャ概観(オンボーディングはここから)
- [SPEC.md](SPEC.md): 言語仕様
- [docs/memory-safety.md](docs/memory-safety.md): safe Kizu memory-safety contract
- [examples](examples/README.md): 機能ごとの読めるプログラムと、`negative/` の拒否例
- [docs/stdlib.md](docs/stdlib.md): standard-library builtin registry と移行計画
- [docs/adr](docs/adr): Architecture Decision Record
- [docs/perf.md](docs/perf.md): build/cache performance policy
- [AGENTS.md](AGENTS.md): Codex agent 向け実装方針

## License

Kizu は [MIT License](LICENSE) で公開します。
