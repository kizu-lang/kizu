# Kizu アーキテクチャ概観

このドキュメントは、リポジトリに初めて触れる人(人間・agent とも)向けの 1 枚地図です。
「どこに何があるか」「データがどう流れるか」を最短で掴むことを目的にします。
各領域の深掘りは末尾のドキュメント索引から辿ってください。

## 1. 全体像: shipping 実装は 1 つ

利用者が受け取る binary は `compiler/` から build した Kizu compiler です。Go の
`internal/` + `cmd/kizu` はそれを build する seed であり、両実装の出力を突き合わせる
oracle でもあります。以前の selfhost は独立した第二実装として育ち、片方がもう片方を
偽装して緑を保ったため削除しました。今回は Go の構造と挙動を保った機械移植にし、
両者の差分を corpus で突き合わせています(ADR-0130)。

規模感(2026-08 時点): Go 実装 約 42k 行 + テスト 約 18k 行、Kizu 製 std 約 10k 行、
examples 約 5k 行。

## 2. リポジトリ地図

```
cmd/kizu/            CLI 本体と、パッケージ横断の統合テスト・conformance
internal/            Go 実装(1 パッケージ = 1 責務)
  source, token, lexer, parser, ast   入力 text・字句・構文・AST
  typ                           型の綴りを 1 度だけ parse した形(文字列手術の代わり)
  types, ownership              型検査・所有権/借用検査
  ir, llvm, wasm, native        typed SSA IR と各 backend
  project, stdlib, manifest     パッケージ/モジュール解決(std も含む)、std の在処、kizu.toml
  stdmethod, stdprim, stdmeta   std の method 署名、builtin primitive の一覧、`std::meta` の形
  unsafecap                     unsafe が覆う操作の種類と診断文言
  conformance                   example が末尾に宣言する case の読み取り
  version, selfhost             binary の名乗りと、移植先が読めない生成 source
  fmt, diagnostic, quote, buildcache, cimport, lsp
compiler/            non-shipping の Kizu compiler 移植先(cutover まで CLI 非接続)
  src/main_test.kizu test-only package root。cutover までは user-facing entrypoint を持たない
  src/internal/      1 directory = 1 module の、package外へ公開しない移植済み実装
lib/kizu/std/src/     Kizu で書かれた標準ライブラリ
examples/            言語機能ごとの実例(末尾に自分の case を書く)
tests/behavior/      振る舞いの assert を 1 package に束ねたもの
tests/fixtures/      module 解決などが使う固定入力
docs/, docs/adr/     設計ドキュメントと ADR
```

## 3. パイプライン

```
source.kizu
  → internal/lexer → internal/parser → internal/ast
  → internal/project が import した std module を合流(std も 1 つの package)
  → internal/types(型)→ internal/ownership(所有権)
  → internal/ir(typed SSA)→ internal/llvm → clang(internal/native)
                           → internal/wasm(wasm32-wasi)
```

`run` と `test` は生成した実行ファイルを走らせます。経路は 1 本で、interpreter は
ありません(ADR-0083)。挙動の正は例そのものが末尾に書いた case が持ちます。

`internal/llvm` は出した text を自分で検査します(`verify.go`)。読む register が
その関数の中で定義されていること、`alloca` が entry block にあることの 2 つで、
どちらも壊れると診断の無い実行時の失敗になります —— 前者は clang が SSA 名だけを
言って落ち、後者はループがスタックを 1 回転ごとに食って guard page で死にます。
命令の形は固定しません。

その実行ファイルは build cache に残ります。鍵は LLVM IR・runtime object・
toolchain、つまり実行ファイルが**何でできているか**だけで、ファイル名も時刻も
入りません。front end は数 ms なので毎回通り、そこから先の link を飛ばします
(`kizu run examples/hello.kizu`: 初回 ~250ms → 2 回目以降 ~9ms)。
`build --target native` は逆に、利用者が名前を指定した成果物とその build 記録を
書くコマンドなので cache から読まず、毎回 link します。

CLI(`cmd/kizu`)のコマンド: `run` `parse` `check` `test` `fmt` `init` `ir`
`build`(`--emit-llvm` / `--target native|wasm32-wasi`)`cache` `import-c-header`。
基本の実行経路は `kizu run examples/hello.kizu`。
どのコマンドがどこへ流れるかは `cmd/kizu/main.go` の `dispatch` が全てです。

## 4. std の二層構造

- 実行時の組み込み(print、メモリ、fs、process 等)は Go 実装が提供し、
  `lib/kizu/std/src/*/*.kizu` はその上の Kizu 製 API 面(`std::array` `std::map` `std::string` …)。
- Go 側は `internal/project`(+ `internal/types` の `knownTypes`)経由で std の宣言を
  取り込みます。std は利用者の package と同じ loader を通り、`internal/stdlib` が
  持つのは「ツリーがどこにあるか」だけです。std に public 型を足すときは
  **`knownTypes` の更新が必要**(漏れると checked コードでその型名が
  `unknown type` になります)。
- std が宣言する method 署名は `internal/stdmethod` が、その裏の `std::internal::builtin::*`
  primitive の形は `internal/stdprim` が一本化します。checker と backend は署名を
  書き直さずにここを読みます。

## 5. テスト体系

言語の正しさは **examples + tests/behavior** が持ちます。どちらも自分が何を
約束するかを末尾のコメントブロックに書き、conformance test が木を歩いてそれを
読みます。登録簿はないので、登録し忘れという状態が存在しません。

| 層 | 実行方法 | 内容 |
| --- | --- | --- |
| conformance | `go test ./cmd/kizu` | `examples/` と `tests/behavior/` を全件実行。case を書いていない例があれば落ちる |
| ユニット | `go test ./...`(pre-push hook) | 各 internal パッケージ + CLI smoke + std lexer/parser parity(native 実行)|
| commit hooks | `pre-commit run --all-files` | gofmt / golangci-lint / コメント検査。数秒 |

CI は push/PR ごとに 1 job(`go test ./...` + gofmt)。定時実行は置きません。
言語機能を足したら `examples/` に実例を足し、その末尾に case を書きます。
書式は `internal/conformance` の package doc にあります。

## 6. 開発フロー

- 作業は topic branch + PR(`main` 直 push 禁止)。PR には目的・主要変更・検証結果を書く。
- コミット時に fast hooks、push 時に `go test ./...` が走る(120 秒以内が目標)。
- 仕様に関わる判断をしたら SPEC.md か docs/adr/ を必ず更新する。

## 7. オンボーディング順路(読む順)

1. `README.ja.md` → `SPEC.md` の冒頭と §13 あたりまで(言語像)
2. `examples/hello.kizu` を `kizu run` / `check` / `build --emit-llvm` で触る
3. `cmd/kizu/main.go` の `dispatch`(CLI の入口)
4. `internal/types` と `internal/ownership`(何が拒否されるか)
5. `internal/ir` → `internal/llvm`(生成系)
6. 必要になった領域の docs/ と docs/adr/(下の索引)

## 8. ドキュメント索引

| 知りたいこと | 場所 |
| --- | --- |
| 言語仕様 | SPEC.md |
| 開発ルール・禁止事項 | AGENTS.md |
| メモリ安全モデル | docs/memory-safety.md |
| std の API リファレンス | docs/std/ |
| stdlib の移行計画と builtin registry | docs/stdlib.md |
| 性能作業の記録 | docs/perf.md |
| self-host の移植を選んだ理由 | docs/adr/0130-selfhost-by-mechanical-porting.md |
| 移植で見つかった言語 / std の gap | docs/language-gaps.md |
| 主要な設計判断(IR/型/所有権/comptime …) | docs/adr/(特に 0006 comptime、0009 IR、0011 phase 順、0014 typed SSA、0049 モジュール解決)|
