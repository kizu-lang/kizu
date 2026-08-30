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

Kizu は初期プロトタイプで、自分自身を compile します。release が配る binary は
`compiler/` にある Kizu 製の Kizu compiler です。Go 実装(`internal/` + `cmd/kizu`)は
それを build する seed であり、両実装の出力を突き合わせる oracle でもあります
(ADR-0130)。`TestSelfhostBootstrap` は、self-build した compiler が自分自身を
byte 単位で再現することを要求します。

このリポジトリはまだ実験的です。言語設計の検証中は、構文と実装の詳細が変わり得ます。

### 今動くもの

`kizu run` は `kizu build --target native` が書き出すのと同じ native 実行ファイルを
作り、それを実行します。2 つのコマンドの違いは「実行するかどうか」だけです。
lowering は 1 本しかないため、同じプログラムが `run` と `build` で違う挙動を示すことは
原理的に起きません(ADR-0083)。プログラムが「どう動くべきか」を定義するのは
プログラム自身の末尾に書かれた case であり、特定の実行経路ではありません。

| 機能 | 例の数 | check | run | llvm | wasm |
| --- | ---: | :--: | :--: | :--: | :--: |
| fn / let / struct / literals | 39 | ✅ | ✅ | ✅ | 12/39 |
| arithmetic / comparison / logical | 3 | ✅ | ✅ | ✅ | ✅ |
| while / break / continue / for / label | 10 | ✅ | ✅ | ✅ | 9/10 |
| if / match | 14 | ✅ | ✅ | ✅ | 2/14 |
| enum / union | 15 | ✅ | ✅ | ✅ | ❌ |
| error union `!T` / try / errdefer | 40 | ✅ | ✅ | ✅ | ❌ |
| optional `?T` / orelse / capture | 22 | ✅ | ✅ | ✅ | ❌ |
| move / borrow | 51 | ✅ | ✅ | ✅ | 2/51 |
| deinit / defer | 19 | ✅ | ✅ | ✅ | ❌ |
| arena / handle | 10 | ✅ | ✅ | ✅ | ❌ |
| comptime / reflection | 13 | ✅ | ✅ | ✅ | 1/13 |
| cast / slice / stack buffer / box | 11 | ✅ | ✅ | ✅ | 1/11 |
| unsafe / raw pointer / extern C | 3 | ✅ | ✅ | ✅ | ❌ |
| contract / generics | 12 | ✅ | ✅ | ✅ | 2/12 |
| std::array | 16 | ✅ | ✅ | ✅ | ❌ |
| std::string | 32 | ✅ | ✅ | ✅ | ❌ |
| std::map | 12 | ✅ | ✅ | ✅ | ❌ |
| std::mem / allocator | 16 | ✅ | ✅ | ✅ | ❌ |
| std::json | 14 | ✅ | ✅ | ✅ | ❌ |
| std::sort | 1 | ✅ | ✅ | ✅ | ❌ |
| std::fmt | 6 | ✅ | ✅ | ✅ | ❌ |
| std::testing | 1 | ✅ | ✅ | ✅ | ❌ |
| std::fs / path / io / process | 23 | ✅ | ✅ | ✅ | ❌ |
| std::net / http | 19 | ✅ | ✅ | ✅ | ❌ |
| async / coro | 2 | ✅ | ✅ | ✅ | ❌ |

`✅` はその行の example が全て通ること、分数は一部だけ通ること、`❌` は 1 つも
通らないことを表します。各行は自分の feature tag を宣言した example を数えるので、
1 つの example は複数の行に現れます。runnable example は 152 件、測定は 2026-08-30 に
`just backend-matrix` で実施しました。backend を触ったら回し直してください。
`run` と `wasm` はプログラムの出力で判定します。`run` は native build を実行し、
`wasm` は出力した module を `wasmtime` で読み込みます。`llvm` は lowering が
通ったかで判定します —— native target は `run` が同じ text から build するためです。

| 経路 | 通過 |
| --- | --- |
| `kizu check` | 152/152 |
| `kizu run` | 152/152 |
| `kizu build --emit-llvm` | 152/152 |
| `kizu build --target wasm32-wasi` | 21/152 |

native 経路に pending の runnable example はありません。WASI はまだ target subset
であり、未対応 lowering と出力差分は `just backend-matrix` が報告します。

language core 周辺の tooling:

- typed SSA IR と opt-in の最適化 pipeline
- 上限付きローカルビルドキャッシュ(artifact が何でできているかで content-addressed)
- extern function 宣言向けの限定的な C header import
- `lib/kizu/std/` の Kizu 標準ライブラリ
- LSP server (`cmd/kizu-lsp`)

interpreter はありません。`kizu test` は `kizu run` が `main` をビルドして実行するのと
同じ経路で test block をビルドして実行します。言語機能の実装は 1 つだけです。

`kizu run` と `kizu test` は host の `clang` と libc を必要とします。native build path が
元から持っていた要件がそのまま及びます。生成する LLVM IR は opaque pointer を使うので
`clang` は 15 以上が要ります(clang 14 は `expected type` で拒否します)。
no-libc / freestanding build は build policy としては受理済みですが、未実装です。

## Roadmap

上の表は「今動くもの」を測った結果です。ここには予定・進行中・意図的に採らないものを
書きます。両者を混同しないためです。

| 機能 | 状態 |
| --- | --- |
| 並列処理のための thread | **予定。** 以前の API は checker rule だけを持ち lowering も runtime も無かったため撤回しました。戻すための受け入れ条件は ADR-0025 にあり、その第 1 条件は `kizu run` で実行できることです。coroutine(`std::coro`)と evented な `Io` は入っていますが、これは 1 thread 上の並行性であって並列性ではありません(ADR-0145、ADR-0146) |
| 現在の subset を超える wasm backend | **進行中。** 152 件中 21 件が load して動きます |
| raw pointer の実行時操作 | **check のみ。** `pointer_policy.kizu` と `raw_pointer_deref.kizu` は検査だけで実行しません |
| float literal と float 演算 | **未着手。** `f32` / `f64` は型名として存在しますが、`1.5` は 1 つの literal として字句解析されません |
| type alias | **未着手** |
| `kizu lint` | **未着手** |
| TLS / HTTPS、middleware | **未着手。** `std::http` は平文 TCP 上の HTTP/1 です。middleware は closure 待ちです |
| full generics | **その形では予定しません。** 明示的な static 引数のみ。推論・bounds・HKT は入れません(ADR-0066) |
| `async fn` / `await` 構文 | **採用しません。** function coloring はこの言語が払わないコストです(ADR-0025) |
| Rust の `Send` / `Sync` trait | **採用しません。** 代わりに置くものは、ユーザーが読める 1 つの規則であって、手書きの whitelist ではありません(ADR-0025) |

ここで「実装済み」と呼ぶのは、conformance case がそれを実行して出力を検査している
場合だけです。checker だけが強制する規則は機能に数えません。それが ADR-0025 の
記録する失敗そのものだからです。

## 例

```kizu
fn main() {
    print("hello, kizu");
}
```

```sh
go run ./cmd/kizu run examples/hello.kizu
```

test block は `main` を通らずに同じ経路で走ります。

```kizu
test "std testing assertions" {
    std::testing::expect(true);
}
```

```sh
go run ./cmd/kizu test examples/std_testing.kizu
go run ./cmd/kizu run examples/std_io_process.kizu -- input.kizu   # process 引数は -- 以降
```

[`examples/`](examples/README.md) は機能ごとに読めるプログラムを 1 つずつ、
`examples/negative/` は言語が拒否する規則を 1 つずつ持ちます。
すべての例は末尾に自分の case -- どのコマンドで実行し、何を出力すべきか -- を
書いており、conformance test はそれを読みます。
safe code のメモリ安全契約は
[docs/memory-safety.md](docs/memory-safety.md) に明文化しています。

## binary の入手

prebuilt binary は [GitHub Releases](https://github.com/kizu-lang/kizu/releases)
に添付します。それぞれ `kizu version` で自分の version を名乗るので、古い binary が
新しい source に対して不可解な parse error を出す代わりに、自分が古いことを言います。
同じ layout は flake でも build できます。

```sh
nix build   # ./result/bin/kizu と ./result/lib/kizu
```

開発中は Go seed を source から動かします。compiler の変更が `go run` 1 回で試せます。

```sh
go run ./cmd/kizu run examples/hello.kizu
```

## 開発環境

推奨する開発環境は Nix flake です。shell には Go、golangci-lint、pre-commit、just、
wasmtime が入ります。

```sh
nix develop
pre-commit install
```

recipe の一覧は `just --list` です。よく使うもの:

```sh
just verify          # gofmt + go test ./... + golangci-lint
just check           # pre-commit run --all-files(commit gate)
just selfhost        # compiler/ の Kizu compiler を check / test する
just backend-matrix  # 上の表を作り直す
just perf            # build と cache の計測
just wasi-smoke      # wasm の例を wasmtime で走らせる
```

## CLI

- `kizu parse <file>` は `.kizu` source file を parse します。
- `kizu check <file-or-package>` は type / ownership / move / borrow / arena check を実行します。
- `kizu run <file-or-package>` は native 実行ファイルを作って実行します。
- `kizu test <file-or-package>` は check 済みの top-level test block を、`main` を呼ばずに実行します。
- `kizu fmt [--write|-w] <file>` は canonical token formatter output を出力、または書き込みます。source-preserving formatter ではありません。line comment は落としませんが、canonical form では 1 つ 1 行になるので、code に続く trailing comment は次の行へ移ります。
- `kizu init [path]` は package の雛形を作ります。
- `kizu ir [--opt] <file>` は typed SSA IR を表示します。
- `kizu build --emit-llvm [--opt] <file>` は LLVM IR text を出力します。
- `kizu build --target wasm32-wasi [--opt] <file>` は WASI-compatible WAT を出力します。
- `kizu build --target native [--opt] [--triple <triple>] [--cpu <cpu>] [--abi <abi>] [--libc on|off] [--runtime hosted|freestanding] [--emit exe|obj|llvm] [--linker clang] [-o <out>] <file>` は native executable を link します。
- `kizu cache status` / `kizu cache prune` はローカルビルドキャッシュの表示と削除です。
- `kizu import-c-header <file>` は対応する C prototype を Kizu extern に変換します。
- `kizu version` は binary が何であるかを表示します。

`kizu lint` は未実装です。

## プロジェクト文書

- [docs/architecture.md](docs/architecture.md): アーキテクチャ概観(オンボーディングはここから)
- [SPEC.md](SPEC.md): 言語仕様
- [docs/principles.md](docs/principles.md): すべての判断を照らす設計原理
- [docs/style.md](docs/style.md): std が API の形をどう選ぶか
- [docs/memory-safety.md](docs/memory-safety.md): safe Kizu memory-safety contract
- [docs/std/](docs/std/README.md): 標準ライブラリ API リファレンス
- [docs/tutorial/](docs/tutorial/README.md): 1 つのものを最初から最後まで作る
- [examples](examples/README.md): 機能ごとの読めるプログラムと、`negative/` の拒否例
- [docs/adr](docs/adr): Architecture Decision Record
- [docs/language-gaps.md](docs/language-gaps.md): まだ書けなかったものと、その場で使った局所解
- [docs/stdlib.md](docs/stdlib.md): trusted builtin の境界と、std API を足すときの規則
- [docs/perf.md](docs/perf.md): build/cache performance policy
- [AGENTS.md](AGENTS.md): contributor / coding agent 向けの実装ルール

## License

Kizu は [MIT License](LICENSE) で公開します。
