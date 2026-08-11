# Kizu アーキテクチャ概観

このドキュメントは、リポジトリに初めて触れる人(人間・agent とも)向けの 1 枚地図です。
「どこに何があるか」「データがどう流れるか」「どの実装が正か」を最短で掴むことを目的にします。
各領域の深掘りは末尾のドキュメント索引から辿ってください。

## 1. 全体像: 2 つのコンパイラ実装

Kizu には同じ言語の実装が 2 系統あり、両者の一致(parity)をテストで固定しながら
selfhost 側へ所有権を移していく、という構造で開発が進んでいます。

| 実装 | 場所 | 言語 | 役割 |
| --- | --- | --- | --- |
| Go 実装 | `internal/` + `cmd/kizu` | Go | **挙動の正**。interpreter・checker・IR・LLVM/WASM backend・CLI |
| selfhost 実装 | `selfhost/src/` | Kizu | Kizu で書かれたコンパイラ。frontend は既に本番経路、backend は移行中 |

規模感(2026-08 時点、テスト込み): Go 側(internal + cmd)約 88k 行、
selfhost 約 164k 行、Kizu 製 std 約 10k 行。

重要な前提: `kizu parse` / `kizu check` は**既に selfhost frontend が処理**しています
(Go 実装の interpreter が selfhost のコードを実行する形)。`kizu run` / `kizu test` は
native → selfhost → Go の優先順で dispatch されます(`cmd/kizu/main.go` の `dispatchRun`)。
つまり「Go が正」とは、最終的な受け入れ判定と迷ったときの参照先が Go 実装、という意味です。

## 2. リポジトリ地図

```
cmd/kizu/            CLI 本体と、パッケージ横断の統合テスト・selfhost gate 群
internal/            Go 実装(1 パッケージ = 1 責務)
  token, lexer, parser, ast     字句・構文・AST
  types, ownership              型検査・所有権/借用検査
  interp                        tree-walking interpreter(挙動の正)
  ir, llvm, wasm, native        typed SSA IR と各 backend(native は clang リンク)
  project, buildcache           パッケージ解決(kizu.toml)とビルドキャッシュ
  stdlib, stdprim               std/ の Kizu ソースを Go 側検査系に取り込む wrapper 層
  fmt, diagnostic, lsp, cimport フォーマッタ・診断・LSP・C ヘッダ取り込み
std/src/             Kizu で書かれた標準ライブラリ
  kizu/              言語の自己記述層: std::kizu::{lexer,parser,ast,diagnostic}
selfhost/src/        Kizu で書かれたコンパイラ(§4)
selfhost/tests/      selfhost 用 fixture・parity manifest(tsv)・golden・probes
examples/            言語仕様の実例(約 90 本)。パーサ/実行の parity corpus でもある
tests/conformance/   実装横断で再利用する適合性 manifest
docs/                トピック別ドキュメントと docs/adr/(意思決定記録、80 本超)
scripts/, justfile   開発コマンド(gate 実行レシピは justfile に集約)
SPEC.md              言語仕様。実装と矛盾する変更は SPEC か docs/adr の更新とセット
AGENTS.md            開発ルール(テスト時間予算・禁止事項・PR 運用)
```

## 3. Go 実装のパイプライン

```
source.kizu
  → internal/lexer → internal/parser → internal/ast
  → internal/stdlib が std/src/*.kizu の宣言を合流(demand-load)
  → internal/types(型)→ internal/ownership(所有権)
  → 実行系: internal/interp(tree-walking、挙動の正)
  → 生成系: internal/ir(typed SSA)→ internal/llvm → clang(internal/native)
                                    → internal/wasm(wasm32-wasi)
```

CLI(`cmd/kizu`)のコマンド: `run` `parse` `check` `test` `fmt` `init` `ir`
`build`(`--emit-llvm` / `--target native|wasm32-wasi`)`cache` `why-rebuild`
`import-c-header`。基本の実行経路は `kizu run examples/hello.kizu`。

## 4. selfhost 実装と bootstrap

`selfhost/src/` の構成(トップの `<name>.kizu` が module の facade、同名 dir が実装):

```
lexer/ parser/ resolver  types/ ownership/   frontend(std::kizu::* を土台に構築)
comptime_eval / comptime_diagnostic          typed comptime 評価器と check 段 gate
ir/            checked AST → テキスト IR facts(executable_* / codegen / code_render)
backend/       facts → MIR(compiled_mir_*)→ LLVM テキスト、hosted CLI の描画(cli_*_llvm)
cli/           hosted CLI の check/実行系
abi/ cache/ source/                          ABI 契約・ビルドキャッシュ・ソース管理
*_oracle.kizu  Go 実装との突き合わせ用エントリポイント
```

backend の内部は一貫して**テキスト表現のパイプライン**です:
`stage` コマンドが checked AST を IR facts(`body-node` / `body-token` / `struct-field` /
`type-llvm` … の行指向テキスト、値もテキストで運ぶ)に落とし、`compiled_mir_lower` が
facts を MIR に、`compiled_mir_llvm` が LLVM テキストに描画します。

bootstrap(`just selfhost-bootstrap`)は 3 段の自己コンパイル比較です:

```
stage0-native : Go backend が selfhost パッケージを native 実行体にコンパイル
stage         : stage0 が selfhost 自身をコンパイルし LLVM artifacts を emit
stage1/stage2 : その artifacts を clang でリンク → stage1 が再び自身を emit → stage2
                stage1 と stage2 の出力・fingerprint の一致を検証
```

stage2 実行体(`target/selfhost/stage2/selfhost`)が「hosted artifact」で、
parity gate 群はこれを Go 実装と突き合わせます。

### selfhost コードを書くときの注意

selfhost のソースは **selfhost backend 自身でコンパイル可能な範囲(subset)** に
収める必要があります。checker が通っても backend が拒否する形があり、拒否は
`compiled mir: ...` の関数名付きエラーで返ります。代表的な制約:
match arm の body は Return/ExprStmt のみ、if の then 内で field 代入しない、
index/slice の対象は Var、式を call 引数に直接書かず let で束縛する、など。
新しいループ形状は `selfhost/tests/probes/` に probe を足してから使うのが安全です。
また、**emit 側に関数名決め打ちの静的分岐を足すことは禁止**です(AGENTS.md)。

## 5. std の二層構造

- 実行時の組み込み(print、メモリ、fs、task 等)は Go 実装が提供し、
  `std/src/*.kizu` はその上の Kizu 製 API 面(`std::array` `std::map` `std::string` …)。
- `std/src/kizu/` は**言語の自己記述層**で、Kizu で書かれた lexer/parser/AST。
  selfhost frontend の `selfhost::parser` はこの `std::kizu::parser` を土台にしています。
- Go 側は `internal/stdlib`(+ `internal/types` の allowlist)経由で std の宣言を
  取り込みます。std に public 型を足すときは **wrapper の allowlist 更新が必要**
  (漏れると checked コードでその型名が unknown type になります)。

## 6. テスト体系(要点だけ)

詳細は docs/selfhost-test-tiers.md。原則は「日常は速い層だけ、自動で」です。

| 層 | 実行方法 | 予算/内容 |
| --- | --- | --- |
| daily | `go test ./...`(pre-push hook で自動) | 約 90 秒。ユニット + CLI smoke + std lexer/parser parity(native 実行)|
| commit hooks | `pre-commit run --all-files`(pre-commit hook) | gofmt / golangci-lint / コメント検査。数秒 |
| opt-in gates | `KIZU_RUN_SELFHOST_*` 環境変数 + `just` レシピ | bootstrap、check/run/test/fmt parity、backend artifact、probe 差分など |

parity 系 gate は manifest(`selfhost/tests/cli/*.tsv`)+ golden 比較で、
stage2 実行体と Go 実装の出力一致を固定します。`selfhost/tests/probes/` は
「同じ関数を両 backend でコンパイルして実行結果を突き合わせる」最小再現の置き場で、
baseline.tsv が既知の一致/不一致を記録します。

## 7. 開発フロー

- 作業は topic branch + PR(`main` 直 push 禁止)。PR には目的・主要変更・検証結果を書く。
- コミット時に fast hooks、push 時に `go test ./...` が走る(120 秒以内が目標)。
- backend/codegen を触ったら: focused gate で iteration → `just selfhost-bootstrap` →
  関連 parity gate、の順で検証してから PR。
- 仕様に関わる判断をしたら SPEC.md か docs/adr/ を必ず更新する。

## 8. オンボーディング順路(読む順)

1. `README.ja.md` → `SPEC.md` の冒頭と §13 あたりまで(言語像)
2. `examples/hello.kizu` を `kizu run` / `check` / `build --emit-llvm` で触る
3. `cmd/kizu/main.go` の `dispatch`(CLI がどの実装に流れるか)
4. `internal/interp` と `internal/types`(挙動の正がどう決まるか)
5. `selfhost/src/main.kizu` → `parser.kizu` → `types/checker.kizu`(selfhost frontend)
6. `docs/selfhost-test-tiers.md`(検証の全体地図)
7. 必要になった領域の docs/ と docs/adr/(下の索引)

## 9. ドキュメント索引

| 知りたいこと | 場所 |
| --- | --- |
| 言語仕様 | SPEC.md |
| 開発ルール・禁止事項 | AGENTS.md |
| テスト層の使い分けと実測 | docs/selfhost-test-tiers.md |
| bootstrap の詳細 | docs/selfhost-bootstrap.md |
| selfhost 実行体の runtime ABI | docs/selfhost-runtime-abi.md |
| CLI parity の枠組み | docs/selfhost-cli-parity.md |
| メモリ安全モデル | docs/memory-safety.md |
| stdlib の設計 | docs/stdlib.md |
| 性能作業の記録 | docs/perf.md |
| 主要な設計判断(IR/型/所有権/comptime …) | docs/adr/(特に 0006 comptime、0009 IR、0011 phase 順、0014 typed SSA) |
