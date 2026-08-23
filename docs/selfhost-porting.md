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
再設計しません。ただし AST / token の内部表現(node の保持方法、error 回復時の
placeholder、handle の形)は、render・diagnostic・処理順が Go と同じなら Kizu-native
に変えてよく、pass 構成と diagnostic 文面は変えません。それ以外の必要性が見つかった
場合は移植を止め、Go の shipping path と SPEC / behavior test のどこが変わるかを別の
判断として先に確定します。

機械移植を選んだ理由と却下した案:

| 案 | 却下理由 |
| --- | --- |
| compiler を Kizu で一から再設計する | 移植と architecture 変更の差が見えず、Go 版との挙動差を説明できない |
| 汎用 Go-to-Kizu transpiler を維持する | Go の interface、GC、slice、pointer semantics を再実装する恒久的な第二 toolchain になる |
| Go と Kizu を長期間ともに shipping する | 言語機能を二度実装し、fallback が片方の欠陥を隠した以前の失敗を繰り返す |
| file ごとに規則なしで AI へ一括変換させる | ownership、module 境界、diagnostic の判断が file ごとに分岐する |
| 生成 IR や内部 AST の文字列一致を gate にする | 実装の形を固定し、挙動が正しくても変更できない。examples と behavior test が契約を持つ |

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
設計の不足を見つける signal として扱います。比率は work unit と module 累計の 2 段階で
測ります。

- work unit は、対象 declaration と、その移植のために新設または変更した result type /
  helper の code LOC を Go と Kizu で数える。後で共用する予定という理由では除外しない
- work unit の完了ごとに、対象 module 全体の Go / Kizu code LOC と比率も更新する。
  Kizu module に production の internal submodule がある場合はその subtree も合算し、
  module 累計から共用 helper や先行 work unit の基盤を除外しない
- どちらも blank、comment、test、document を実装比率へ混ぜない。改行だけで比率が動いて
  いないことを確かめるため、空白区切りの code word 数と比率も併記する

- work unit または module 累計の Kizu が Go の 1.5 倍以上なら、増えた行を ownership の
  明示、error 処理、API shape、重複処理に分類し、それぞれの行数を報告する
- 2 倍以上なら移植を止める。既存の原理から source に残す必要がある操作だけを不可避な
  増分として数え、分類名を付けただけでは説明済みにしない
- 累計 gate 違反が後から判明した module は、以前 complete と記録していても未完了へ戻し、
  原因を閉じるまで後続 module の移植を進めない
- 同じ ownership / error / construction の定型が複数の call site に増える場合は、個々の
  行が必要でも local representation または API shape の問題として扱う
- local representation や分割の問題なら target を直す
- 複数 module で必要になる primitive がないなら std gap として切り出す
- source で表現できない、または毎回 boilerplate を要求するなら language gap として止め、
  移植の途中で仕様を足さない

LOC を揃えるために lifecycle、境界 check、意味のある名前を消しません。比率は品質目標では
なく調査 trigger です。明示 ownership に必要な増分は、その操作を共通化しても source から
消してはいけない理由と cleanup path を示せる場合だけ許容します。module 完了時に累計 gate を
満たさない、または不可避と確認できない反復が残るなら complete と記録しません。

## Mechanical mapping

型は綴りだけで決めず、value ごとの ownership から決めます。

| Go | Kizu | 規則 |
| --- | --- | --- |
| named string constant set | `enum` + spelling function | identity と表示用 bytes を分ける |
| `string` | `[]u8` または `std::string::String` | static/source view は前者、生成して保持する bytes は後者 |
| `typ.Type.String()` / `typ.Text` | `typ::render(allocator, table, value)` | `Type` handle の owner `Table` を渡し、返った `String` は使用scopeで `deinit` する |
| AST nodes が共有する `string` | AST 所有の `Text` handle | logical text ごとに一度保持し、生成 node 間で handle を再利用する。content interning はしない |
| `[]T` | `[]T` または `std::array::Array<T>` | borrow view は前者、容量と cleanup を持つ slice は後者 |
| AST nodes が共有する `[]Expression` | AST 所有の `ExpressionList` handle | Go の共有 slice backing を一つの retained list と copy handle で表す |
| `map[string]V` | `std::map::Map<[]u8, V>` | iteration order と overwrite behavior を先に確認する |
| immutable map literal | pure lookup function または明示 owner の `Map` | hidden global allocator を作らず、hot path は計測する |
| `*T` | `&T` / `&var T` / `Box<T>` / arena handle | nullable、mutation、owner、lifetime を ownership 表で固定する |
| parse error 後の nil 埋め partial node | `ast::RecoveredNode` variant | diagnostic を出した後は node を作らず `add_recovered_*` を返す。partial AST は CLI も LSP も捨てるので契約ではない |
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

compiler package の外へ見せる module は root の `compiler` だけです。実装 module は
`src/internal/` に置き、Go の `internal/x` を `compiler::internal::x` に対応させます。
module 本体は `src/internal/parser/parser.kizu`、その test は
`src/internal/parser/parser_test.kizu` に置き、同じmoduleとして扱います。Go packageの
production fileを分ける場合も同じdirectoryへ置きます。別moduleとして閉じる必要が
ある実装だけを `src/internal/parser/internal/x/x.kizu` に置き、parser subtreeの外へ
公開しません。

| Go | Kizu |
| --- | --- |
| `internal/source` | `compiler::internal::source` |
| `internal/token` | `compiler::internal::token` |
| `internal/lexer` | `compiler::internal::lexer` |
| `internal/parser` | `compiler::internal::parser` |
| `internal/ast` | `compiler::internal::ast` |
| `internal/diagnostic` | `compiler::internal::diagnostic` |
| `internal/typ` | `compiler::internal::typ` |
| `internal/types` | `compiler::internal::types` |
| `internal/ownership` | `compiler::internal::ownership` |
| `internal/ir` | `compiler::internal::ir` |
| `internal/llvm` | `compiler::internal::llvm` |
| `internal/wasm` | `compiler::internal::wasm` |
| `internal/project` | `compiler::internal::project` |
| `internal/fmt` | `compiler::internal::fmt` |
| `cmd/kizu` | `compiler` |

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

`kizu check compiler` と `kizu test compiler` は pre-commit hook `selfhost` が回します。

### Behavior corpus

module の公開挙動は、Go test の white-box 構造を Kizu に写すのではなく、両 compiler が
同じ入力 corpus を読む形で検査します。parser の corpus は `compiler/tests/parser/` で、
1 file = 入力 + 空行 + `// parse` + 期待 block(失敗なら `// L:C-L:C message` の
diagnostics、成功なら `// ast:` 以下に rendered program)です。期待 block は Go の
`go test ./internal/parser -run TestParserCorpus -update` が生成し、Kizu 側は
`compiler/src/internal/parser/corpus_test.kizu` の runner が同じ file を読んで比較します。
Go の挙動を変えたら `-update` で期待 block を作り直し、Kizu 側を同じ commit で追従させます。
Kizu の unit test に残すのは、入出力で表せない ownership / lifecycle の invariant だけです。

並列 agent は同じ file を編集しません。shared type と ownership 表は一人の
integrator だけが変更し、他の agent は変更要求を返します。

## Unattended work

module 単位の機械移植は、次の契約で人の判断を待たずに進めます。

- 範囲は 1 module ずつ。module の完了条件(下)を満たすまで次へ進まない
- work unit ごとに topic branch へ commit し、review は module 単位の PR で受ける
- 止まる条件: SPEC / std の変更が要る、分類と修正の後も累計 ratio が 2 倍以上、
  Go 側の shipping 挙動を変える修正が要る、ir / llvm の内部表現を決める時
- 見つけた language / std gap は止めずにこの文書末尾の「見つかった gap」へ
  証拠付きで記録し、仕様を足さない局所解で進める。module 完了時にまとめて判断する
- 検証は corpus、`kizu check compiler`、`kizu test compiler`、fmt、pre-commit で
  機械的に行い、人の目は PR で入る

## Completion

module complete は、対象 Go file の全 declaration が移り、stub と unexplained
difference がなく、Kizu check と対応する behavior test が通った状態です。
compiler 全体の cutover は module の完了とは別で、self-build した compiler が同じ
source をもう一度 build でき、既存の examples と `tests/behavior/` を shipping
compiler と同じ契約で通した状態です。

binary byte identity や LLVM text identity は要求しません。契約は実行結果、
diagnostic、artifact の意味が持ちます。

## Module status

code LOC は blank / comment / test を除いた行数。ratio は Kizu / Go。検証欄は挙動一致を
何で確認しているか。ratio が 2.0 以上の module は gate 未達で、圧縮 pass が残っている。

| module | Go | Kizu | ratio | 検証 |
| --- | --- | --- | --- | --- |
| source | 44 | 39 | 0.89 | unit |
| token | 123 | 254 | 2.07 | unit(parser corpus 経由) |
| diagnostic | 126 | 291 | 2.31 | unit(corpus の render 経由) |
| lexer | 325 | 593 | 1.82 | unit + parser corpus |
| ast | 1042 | 2330 | 2.24 | parser corpus(render) |
| parser | 1796 | 2990 | 1.66 | parser corpus 308 case(diagnostics + render) |
| typ | 524 | 1047 | 2.00 | unit |
| stdprim / stdmeta / stdmethod / unsafecap | 140 / 211 / 80 / 60 | 259 / 420 / 214 / 94 | 1.85 / 1.99 / 2.67 / 1.57 | unit |
| manifest | 170 | 261 | 1.54 | unit(Go と 27 入力で byte 一致) |
| project(+stdlib) | 1820 | 3262 | 1.79 | check corpus 635 case の load 段 + package 単位 render |
| types | 7768 | 15293 | 1.97 | check corpus 635 case(diagnostics) |

## 見つかった gap

移植中に見つかった language / std gap と、その場で使った局所解。module 完了時に判断する。

| gap | 証拠 | 局所解 |
| --- | --- | --- |
| sequence literal(`[N]T{a, b}` / table / table-driven test)が無い | parser の precedence table(if 連鎖 23 行)、Go の table test 7 本が展開されている | 展開のまま |
| 文字列組立の std API が `append_*` だけ | parser の message helper 155 行、checker の `errorf` 315 箇所 | parser は `error_*` method に融合(parser.kizu) |
| struct field からの take / replace が無い | parser が cur/peek を持てず全 token を arena に保持 | `Arena<Token>` + handle stream |
| `orelse` の右辺に block を書けない | parser の early return | `if opt \|v\| {} else {}` に展開 |
| owner の `?T` は産出した場所で capture / orelse が必須 | parser test | 同上 |
| `[]u8` view は関数から返せず、`as_bytes()` は local binding に束縛必須 | loader の `module_name() -> []u8` 系 helper が書けない | `&string::String` を返す / 渡し、callee 側で bind する。append 系 helper に変える |
| `match try f()` では owner payload を move out できない | loader の `match try self.read_std_graph()` | `let r = try f(); match r { ... }` |
| `else if` が無い | loader | `else { if ... }` に展開 |
| owner field への代入不可のため「field を入れ替える」書き方が無い | loader の `self.order` の差し替え、ModuleFile.imports の設定 | 空で作って in place に insert する。入れ替えが要る場合は 2 つの field を持つ |
| Array.at の結果は capture 限定で `orelse` も不可 | checker body port | `if arr.at(i) \|v\| {} else {}` |
| `typ::map_names` の rename callback は error set しか返せず diagnostic を運べない | loader の resolve_type_node | mapper struct に失敗を記録して呼び出し後に読む |
| view field を持つ struct(`ast::ConstructField { []u8, []u8 }`)を view binding から作れない | checker body の construct_expansion 呼び出し | builder(`begin_construct` / `add_field` / `finish_construct`)に変えた |
| `while` 本体で作った view が同 block の `defer owner.deinit()` と衝突する | checker body | view を iteration ごとに bind し直す |
| `mem::slice` を view に使えない / `?[]u8` を返す helper に view binding を渡せない | checker body | `x[a..b]` で切る / index を返す helper にする |
| union payload に `?T` を置けない | loader の qualify(optional child を持つ結果) | optional 子は inline で分岐 |
| `?Owner` を値で渡せず `&?T` 引数も不可 | loader の qualify(`copy_docs`) | capture した `&Map` を渡す。literal を 2 箇所に複製 |
| expression の match arm で `return` できない | loader / checker body | statement の match に包む |
| closure が無いので callback 型 API(`typ::map_names`)に Loader を渡せない(struct に借用を持てない) | loader の resolve_type_node | 2 pass(名前を集めて解決し、rename 表で map_names) |

