# Selfhost porting guide

この文書は `internal/` + `cmd/kizu` の Go compiler を `compiler/` の Kizu source
へ機械移植する agent と reviewer の共通契約です。移植中だけ置き、cutover で Go
compiler source、移植 workflow、`docs/selfhost-ownership.tsv` と一緒に削除します。

## 目的

- 今の compiler の挙動、処理順、diagnostic、package 境界を保って Kizu へ移す
- ownership の判断を source と evidence から一度だけ行う
- 独立した work unit を AI で並列移植できる状態にする
- cutover の前後とも user-facing compiler path を一つに保つ

移植と同時に language feature、compiler architecture、optimization、public API を
再設計しません。必要性が見つかった場合は移植を止め、Go の shipping path と
SPEC / ADR / behavior test のどこが変わるかを別の判断として先に確定します。

## Source of truth

矛盾した場合は次の順で判断します。

1. `SPEC.md` と `docs/std/` の現在の契約
2. `examples/` と `tests/behavior/` が宣言する挙動
3. Go compiler の observable behavior と diagnostic
4. Go source の内部構造

Go source が上位の契約と違う場合、その bug を Kizu へ写しません。work unit を
止め、Go 側で契約を満たしてから同じ確定済み挙動を移します。

## Shipping boundary

- cutover までは `internal/` + `cmd/kizu` だけを release する
- `compiler/` を `cmd/kizu`、fallback、feature flag、user-facing command へ接続しない
- Kizu compiler から Go package や Go helper を呼ばない
- 一部だけ移植した module を complete と記録しない
- cutover 後は Go compiler とこの移植専用文書を削除する

## Work unit

1 work unit は、独立して `kizu check` できる一つの module、または明記した一組の
declaration です。agent は着手前に次を読みます。

1. この文書全体
2. 対象の Go file 全体
3. `docs/selfhost-ownership.tsv` の対象行
4. target module が直接使う Kizu module の public declaration
5. 対象挙動を持つ Go test、example、behavior test

広い周辺探索から architecture を推測しません。不足する cross-module contract は
推測で追加せず、work unit の blocker として返します。

work unit の出力は次を含みます。

- Go source と Kizu target の path
- port した declaration と意図的に範囲外にした declaration
- 参照した ownership 行
- 対象 declaration と専用 helper の Go / Kizu code LOC と比率
- observable な差がないこと、または残る差
- 実行した check / test
- `high / medium / low` の confidence と、その根拠

### Size parity gate

移植で同じ処理が大幅に長くなることは、単なる見た目ではなく language / std / ownership
設計の不足を見つける signal として扱います。blank、comment、test、共用済みの基盤を除き、対象
declaration とその移植だけが必要とする result type / helper の code LOC を Go と Kizu で
数えます。test と document の行数は別に報告し、実装比率へ混ぜません。

- Kizu が Go の 1.5 倍以上なら、増えた行を ownership の明示、error 処理、API shape、
  重複処理に分類して理由を調べる
- 2 倍以上なら、全増分を既存の原理で説明できない限り移植を止める
- local representation や分割の問題なら target を直す
- 複数 module で必要になる primitive がないなら std gap として切り出す
- source で表現できない、または毎回 boilerplate を要求するなら language gap として止め、
  移植の途中で仕様を足さない

LOC を揃えるために lifecycle、境界 check、意味のある名前を消しません。比率は品質目標では
なく調査 trigger です。明示 ownership に必要な増分は、その根拠と cleanup path が説明できれば
許容します。

## Mechanical mapping

型は綴りだけで決めず、value ごとの ownership から決めます。

| Go | Kizu | 規則 |
| --- | --- | --- |
| named string constant set | `enum` + spelling function | identity と表示用 bytes を分ける |
| `string` | `[]u8` または `std::string::String` | static/source view は前者、生成して保持する bytes は後者 |
| AST nodes が共有する `string` | AST 所有の `Text` handle | logical text ごとに一度保持し、生成 node 間で handle を再利用する。content interning はしない |
| `[]T` | `[]T` または `std::array::Array<T>` | borrow view は前者、容量と cleanup を持つ slice は後者 |
| AST nodes が共有する `[]Expression` | AST 所有の `ExpressionList` handle | Go の共有 slice backing を一つの retained list と copy handle で表す |
| `map[string]V` | `std::map::Map<[]u8, V>` | iteration order と overwrite behavior を先に確認する |
| immutable map literal | pure lookup function または明示 owner の `Map` | hidden global allocator を作らず、hot path は計測する |
| `*T` | `&T` / `&var T` / `Box<T>` / arena handle | nullable、mutation、owner、lifetime を ownership 表で固定する |
| `(T, error)` | named error または `!T` | caller が error を列挙する契約なら named set を保つ |
| nullable value | `?T` | absence と failure を混ぜない |
| interface | static contract / closed union / generic | runtime dispatch を仮定せず、対応が閉じなければ止める |
| `defer` | `defer` / `errdefer` | cleanup を source に残す。自動 Drop へ移さない |

Go の `panic`、reflection、goroutine、shared mutable global、unsafe pointer が現れた
場合は local mapping を発明しません。移植全体で一つの判断が必要な境界として
止めます。

## Naming and layout

Kizu の通常の snake_case と module namespace へ機械的に変換します。名前変更で
意味を整理し直しません。

| Go | Kizu |
| --- | --- |
| `internal/source` | `compiler::source` |
| `internal/token` | `compiler::token` |
| `internal/lexer` | `compiler::lexer` |
| `internal/parser` | `compiler::parser` |
| `internal/ast` | `compiler::ast` |
| `internal/diagnostic` | `compiler::diagnostic` |
| `internal/typ` | `compiler::typ` |
| `internal/types` | `compiler::types` |
| `internal/ownership` | `compiler::ownership` |
| `internal/ir` | `compiler::ir` |
| `internal/llvm` | `compiler::llvm` |
| `internal/wasm` | `compiler::wasm` |
| `internal/project` | `compiler::project` |
| `internal/fmt` | `compiler::fmt` |
| `cmd/kizu` | `compiler::cli` |

package cycle を hook、global registry、extern call、duplicate type で迂回しません。
cycle が出たら Go 側の実際の ownership を調べ、共有型の owner を一つに決めます。

## Forbidden output

- `todo`、空 body、成功を返すだけの stub、未実装 branch
- Go fallback、hidden fallback、二つの backend を選ぶ feature flag
- Go source にない source-shape dispatch や payload template
- LLVM text の手書き literal
- compile error を消すためだけの copy、leak、unsafe、default allocator
- Go test の内部構造や生成 text を grep する新しい構造 pin
- agent ごとに異なる共通 type、error、diagnostic

範囲を小さく試す場合は、port した declaration を明記し、source file 全体を完了扱いに
しません。stub を置く代わりに、未着手の declaration を target へ書きません。

## Review loop

各 work unit は同じ agent の自己確認だけで閉じません。

1. port: Go source から Kizu source を作る
2. adversarial review: 別の reviewer が Go source、ownership 表、この文書と比較する
3. fix: 指摘を局所修正する
4. check: target module を Kizu compiler で check する
5. behavior: 対象の公開挙動を Kizu test で実行する

並列 agent は同じ file を編集しません。shared type と ownership 表は一人の
integrator だけが変更し、他の agent は変更要求を返します。

## Completion

module complete は、対象 Go file の全 declaration が移り、stub と unexplained
difference がなく、Kizu check と対応する behavior test が通った状態です。
compiler 全体の cutover は module の完了とは別で、self-build した compiler が同じ
source をもう一度 build でき、既存の examples と `tests/behavior/` を shipping
compiler と同じ契約で通した状態です。

binary byte identity や LLVM text identity は要求しません。契約は実行結果、
diagnostic、artifact の意味が持ちます。
