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
| 生成 text を golden として pin し、それを契約にする | 実装の形を固定し、挙動が正しくても変更できない。契約は `examples/` と `tests/behavior/` が持つ。両実装の出力を突き合わせる差分 corpus(`compiler/tests/`)は別物で、これは採用している — golden と違い、Go 側を変えれば `-update` で両方が一緒に動く |

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
- 2 倍以上なら、増えた行の分類を報告に必ず載せる。既存の原理から source に残す必要が
  ある操作だけを不可避な増分として数え、分類名を付けただけでは説明済みにしない
- 比率そのものは移植を止めません。設計として妥当だと説明できるなら、その ratio は
  許容します。止めるのは、説明の途中で language / std gap が見つかったときだけです
- 同じ ownership / error / construction の定型が複数の call site に増える場合は、個々の
  行が必要でも local representation または API shape の問題として扱う
- local representation や分割の問題なら target を直す
- 複数 module で必要になる primitive がないなら std gap として切り出す
- source で表現できない、または毎回 boilerplate を要求するなら language gap として止め、
  移植の途中で仕様を足さない

LOC を揃えるために lifecycle、境界 check、意味のある名前を消しません。比率は品質目標では
なく調査 trigger で、行数は目安です。明示 ownership に必要な増分は、その操作を共通化しても
source から消してはいけない理由と cleanup path を示せる場合だけ許容します。

ratio が高いこと自体は complete を妨げません。妨げるのは、増分を分類できないこと、または
分類の結果が「language / std gap があるので同じ定型を書き続けている」だと分かったのに
その gap を記録していないことです。

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
| `ir.Module` / `Function` / `Block` / `Instr` の pointer graph と `string` の op / 名前 | `Module` 所有の arena + copy handle(`Function` / `Block` / `Instr`)、`Op` enum + `operand: Name` + spelling 関数、`Module` 所有の `NameTable` に intern した `Name`(value の name / type、block label、call 先、immediate) | `call.<f>` / `field.<f>` / `binary.<op>` は `Op` の種類と運ぶ名前に分ける。llvm / wasm emitter も同じ型を読む。`Cleanup` の `Args` は常に receiver 1 つなので `arg: Value` |
| `llvm.emitter` の `string` operand / label / LLVM 型名と、SSA 名で引く `map[string]valueInfo` / `blockExitLabel` / `strings` | `Emitter` 所有の `NameTable` に intern した `Name`(operand、label、LLVM 型名、`valueInfo` の typ / operand)、SSA 名・label 名・literal の bytes を key にした `Map`(Go が関数ごとに作り直す map は entry に関数の generation 番号を持たせて区別する。`Map` は空にできない)、IR の型綴りは `module.names` の bytes view を関数間で渡す | `fmt.Fprintf` の format 文字列を `%s` / `%d` / `%%` を展開する `line1..line6` にそのまま渡し(末尾 `\n` だけ helper が足す)、`fmt.Sprintf` は `text*` / `sprint*`。Go に無い template・形状分岐は作らない。`module` は全 method が `&ir::Module` で受け、`emit` だけが cleanup 命令の `void` 型名を intern するため `&var`。emit.go は llvm.kizu(emitter・walk・dispatch・共有 helper)/ header.kizu(module 宣言)/ instr.kizu(scalar・aggregate・memory 命令)/ call.kizu(`call.*` と extern 宣言)/ error.kizu(`error.*` / `opt.*` / return)に分け、他の Go file は同名の .kizu |

| `internal/native` の埋め込み runtime C(`//go:embed runtime/runtime.c`)と build cache | 生成 file `runtime_source.kizu`(multi-line literal を返す 1 関数)。cache は `compiler::internal::buildcache` に移植し、runtime object と executable は Go CLI と byte 一致する key で共有 cache に置く(temp dir は toolchain の transient input 置き場だけになる)。cache が持つのは content-addressed な artifact だけ(ADR-0126 が text entry を廃止)。builder closure は module 境界を越えられないので `GetOrBuildArtifact` は「hit 判定と plan を返す前半 + caller が build して渡す後半(`finish_artifact` / `discard_artifact`)」の 2 段 API に写す。sha256 は `compiler::internal::sha256`(pure Kizu。bitwise 演算子が無いので AND / XOR は byte-pair table、shift / rotate は 2 の冪の乗除)、RFC3339 は `compiler::internal::timestamp`、os / filepath / strings 相当は `compiler::internal::fsutil` で、Go 対応が標準 library のためどれも LOC ratio は別記録。key の host 部(Go `runtime.GOOS-GOARCH`)は `sh -c "PATH=/usr/bin:/bin uname -sm > file"` の probe で写す | runtime C の byte 一致は `go test ./internal/native -run TestRuntimeSourceKizu -update` が gate する(data の同一性 gate であって構造 pin ではない)。cache key・entry / artifact file 名の Go との一致と双方向の cache 再利用・`cache status` / `prune` の出力一致は `TestSelfhostCache` が gate する(識別子の一致 gate であって構造 pin ではない)。sha256 は共有 corpus `compiler/tests/sha256/vectors.txt` を Go 側 `TestSharedSHA256Vectors`(crypto/sha256)と Kizu 側 unit test の両方が読む。`Metadata` / `Entry` は json tag の担体なので `std::json` encoder の field 列に写す |

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
| `internal/buildcache` | `compiler::internal::buildcache` |
| `internal/project` | `compiler::internal::project` |
| `internal/fmt` | `compiler::internal::fmt` |
| `internal/cimport` | `compiler::internal::cimport` |
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
check corpus は `compiler/tests/check/` で、1 file = 入力 + `// check` + `// load:`(loader が
出す修飾済み宣言か module error)+ `// check:`(`ok` か diagnostics)です。期待 block は
`go test ./internal/types -run TestCheckCorpus -update` が types → ownership の順に生成し
(types が通った case だけ ownership まで進む)、`compiler/src/check_corpus_test.kizu` が
同じ順で読みます。case は types の test 入力(`t_`)、ownership の test 入力(`o_`)、examples
(`ex_`)、negative examples(`neg_`)から採っています。
IR corpus は `compiler/tests/ir/` で、1 file = 入力 + `// ir` + `// lower:`(lowering した
module の render)+ `// opt:`(同じ入力を lower し直して optimize した module の render)です。
どちらも失敗なら `// ` + error text の 1 行で、std の関数(symbol が `std::` で始まるもの)は
量が多いので render から除きます(std の lowering は `TestSelfhostFrontend` の `ir` 比較が
全 example と package で byte 比較します)。期待 block は
`go test ./internal/ir -run TestIRCorpus -update` が生成し、`compiler/src/ir_corpus_test.kizu`
が同じ file を読みます。case は check corpus の入力のうち check が `ok` のもの(同名)、
tests/behavior の各 test file(`beh_`)、internal/ir の test source(`ir_`)から採っています。
LLVM corpus は `compiler/tests/llvm/` で、1 file = 入力 + `// llvm` + `// emit:`(lowering した
module を emit した LLVM IR)+ `// opt:`(optimize した module を emit した LLVM IR)です。
どちらも失敗なら `// ` + error text の 1 行で、IR corpus と同じく std の関数(symbol が
`std::` で始まるもの)は emit する前に module から落とします(std を含む full text は
`TestSelfhostFrontend` の `build --emit-llvm` 比較が持ちます)。期待 block は
`go test ./internal/llvm -run TestLLVMCorpus -update` が生成し、
`compiler/src/llvm_corpus_test.kizu` が同じ file を読みます。case の入力集合は IR corpus と
同じで、llvm 固有の入力(`llvm_`)は internal/llvm の test source から採っています。LLVM text の
一致は command の観測挙動を corpus で固定するもので、契約としての text pin ではありません。
Kizu の unit test に残すのは、入出力で表せない ownership / lifecycle の invariant だけです。

並列 agent は同じ file を編集しません。shared type と ownership 表は一人の
integrator だけが変更し、他の agent は変更要求を返します。

## Unattended work

module 単位の機械移植は、次の契約で人の判断を待たずに進めます。

- 範囲は 1 module ずつ。module の完了条件(下)を満たすまで次へ進まない
- work unit ごとに topic branch へ commit し、review は module 単位の PR で受ける
- 止まる条件: SPEC / std の変更が要る、増分の分類で language / std gap が見つかった、
  Go 側の shipping 挙動を変える修正が要る、ir / llvm の内部表現を決める時
- 見つけた language / std gap は止めずにこの文書末尾の「見つかった gap」へ
  証拠付きで記録し、仕様を足さない局所解で進める。module 完了時にまとめて判断する
- 検証は corpus、`kizu check compiler`、`kizu test compiler`、fmt、pre-commit で
  機械的に行い、人の目は PR で入る

## Completion

module complete は、対象 Go file の全 declaration が移り、stub と unexplained
difference がなく、Kizu check と対応する behavior test が通った状態です。
compiler 全体の cutover は module の完了とは別で、次の 3 つが揃った状態です。

1. self-build した compiler が同じ source をもう一度 build できる
2. 既存の examples と `tests/behavior/` を shipping compiler と同じ契約で通す
3. `cmd/kizu` の user-facing command が Kizu 側に揃っている — cutover は Go compiler を
   削除するので、CLI の機能は落とせません。残りは「未移植」表にあります

cutover 後は「Kizu compiler を build するには Kizu compiler が必要」になるので、
seed をどう供給するか(過去の release binary を使うのか、他の手段か)は cutover の
判断と一緒に決めます。この文書はまだそれを決めていません。

1 は `TestSelfhostBootstrap` が gate にします。shipping compiler が build した
compiler と、それが同じ source から build した compiler の byte 一致を見ます。
Mach-O では linker が link ごとに作る `LC_UUID` と、そこから導出される ad-hoc code
signature だけを比較前に zero 化します。これは compiler 出力ではない identity metadata
で、それ以外の code、data、symbol、load command field はすべて byte 一致を要求します。
2 は `TestSelfhostFrontend` と `TestSelfhostNative` が持ちます。3 に gate はなく、
「未移植」表が残りを持ちます。
selfhost が自分の source を最後まで build する経路はここにしかありません
(`TestSelfhostFrontend` は `compiler/` を emit-llvm text までしか見ず、
`TestSelfhostNative` が link して実行するのは examples / `tests/behavior` /
module examples で、`compiler/` は入っていない)。

ここだけ byte 一致を使うのは、fixed point の主張がまさに「両者が同じもの」で
あることと、代わりに挙動で示すなら corpus 全体をもう一周する必要があるからです。
golden text との一致ではなく、同じものを作るはずの 2 実装の差分比較なので、
codegen が変われば両方が一緒に動きます。

それ以外に binary byte identity や LLVM text identity は要求しません。契約は
実行結果、diagnostic、artifact の意味が持ちます。

## Module status

code LOC は blank / comment / test を除いた行数。生成 file は除く(`native` は
`runtime_source.kizu` の 1,576 行を含まない)。ratio は Kizu / Go。検証欄は挙動一致を
何で確認しているか。ratio は調査 trigger であって合否ではないので、2.0 以上の module も
増分を説明できていれば complete です。

| module | Go | Kizu | ratio | 検証 |
| --- | --- | --- | --- | --- |
| source | 44 | 39 | 0.89 | unit |
| token | 125 | 259 | 2.07 | unit(parser corpus 経由) |
| diagnostic | 126 | 291 | 2.31 | unit(corpus の render 経由) |
| lexer | 325 | 593 | 1.82 | unit + parser corpus |
| ast | 1062 | 2360 | 2.22 | parser corpus(render) |
| parser | 1858 | 3119 | 1.68 | parser corpus 308 case(diagnostics + render) |
| typ | 524 | 1047 | 2.00 | unit |
| stdprim / stdmeta / stdmethod / unsafecap | 142 / 211 / 80 / 60 | 263 / 420 / 214 / 94 | 1.85 / 1.99 / 2.67 / 1.57 | unit |
| manifest | 170 | 261 | 1.54 | unit(Go と 27 入力で byte 一致) |
| project(+stdlib) | 1861 | 3448 | 1.85 | check corpus 635 case の load 段 + package 単位 render |
| fmt | 789 | 1234 | 1.56(words 3,963 / 2,375 = 1.67) | `TestSelfhostFrontend`: examples / negative examples / tests/behavior / compiler/src の全 `.kizu` 691 file で `fmt` の stdout / stderr / exit status を Go CLI と byte 比較 + `--write` / `-w` の出力 file byte 比較 |
| types | 8154 | 16051 | 1.97 | check corpus 765 case(diagnostics)+ unit(catch / error set 合成の diagnostics) |
| ownership | 7893 | 12998 | 1.65(words 46,330 / 28,224 = 1.64) | check corpus 765 case(types が通った case の ownership diagnostics)+ unit(scope clone / merge) |
| ir | 5091 | 9136 | 1.79(words 32,826 / 17,509 = 1.87) | IR corpus 333 case(lower / opt の render)+ `TestSelfhostFrontend` の `ir` / `ir --opt`(examples 466 file + 6 package、std の lowering を含む)+ unit(verify の rejection) |
| llvm | 4035 | 5918 | 1.47(words 25,206 / 15,489 = 1.63) | LLVM corpus 352 case(emit / opt の LLVM IR text、emit error を含む)+ `TestSelfhostFrontend` の `build --emit-llvm` / `--emit-llvm --opt`(examples 466 file + 6 package、std の関数を含む full text を byte 比較)|
| native | 249 | 641 | 2.58(words 2,212 / 867 = 2.55)| `TestSelfhostNative`: run / test / build --target native を Go CLI と比較(423 case。stdout / stderr(toolchain noise のみ除去)/ exact exit status、build は両 exe も実行して比較、metadata は絶対 path 正規化で byte 比較)+ `kizu check compiler` / `kizu test compiler` |
| buildcache | 217 | 431 | 1.99(words 1,441 / 764 = 1.89) | unit(artifact round trip / eviction 順序)+ `TestSelfhostCache`: 隔離 KIZU_CACHE_DIR で Go build → selfhost run の再利用(toolchain の無い PATH で成功 + entry file 名一致)、逆方向、`cache status` / `prune` の出力比較(同一 cache 状態は byte 一致、別 fill は byte 数正規化) |
| sha256(別記録: Go 対応は crypto/sha256)| — | 298 | —(別記録) | 共有 vector corpus `compiler/tests/sha256/vectors.txt` を Go `TestSharedSHA256Vectors` と Kizu unit test が両方読む(NIST FIPS 180-4 vectors + padding 境界長) |
| timestamp(別記録: Go 対応は time)| — | 87 | —(別記録) | unit(RFC3339 UTC の spelling と trim、nanos / millis spelling の順序) |
| fsutil(別記録: Go 対応は os / path/filepath / strings)| — | 76 | —(別記録) | buildcache / native の経路と `TestSelfhostCache`、init の `filepath.Abs` 対応を `TestSelfhostFrontend` の current-directory case で Go と比較 |
| compiler(cmd/kizu の parse / check / fmt / init / ir / build --emit-llvm / --target native / run / test / cache) | 1023(全 command) | 1437(main 1432 + import 衝突 helper 5) | — | 従来の `TestSelfhostFrontend` 2,826 case に fmt 691 file と init(生成 2 file、current directory、両 existing-file rejection)を追加。native の run / test / build は `TestSelfhostNative`、compiler/ 自身を最後まで build して同じ compiler になることは `TestSelfhostBootstrap` |

### 未移植

`internal/` のうち Kizu 側に対応が無いものです。cutover は Go compiler を削除するので、
`kizu` から到達できるものは全部移植します。

| module | Go | `kizu` から到達 | 状態 |
| --- | --- | --- | --- |
| `internal/wasm` | 549 | `build --target wasm32-wasi` | 移植する |
| `internal/cimport` | 196 | `extern "c"` の宣言読み | 移植する |
| `internal/quote` | 31 | 無(ir / llvm 経由)| 移植しない。`std::fmt::append_bytes_literal` が同じ仕事をしていて、selfhost 側は既にそれを使っている |
| `internal/conformance` | 199 | 無(Go test 専用)| 移植しない。example の case block を読むのは Go test harness の仕事 |
| `internal/lsp` | 4627 | 無(`cmd/kizu-lsp` 別 binary)| `kizu` の CLI ではなく release にも入らない(`go install` で配る)。cutover 条件の外 |
| `internal/stdlib` | — | std の読み込み | `project` 行に含めて計上済み |

types の増分(+7,525 行)の分類。閉じ括弧だけの行が Go 1,751 に対して Kizu 3,219 で、
差の大半は 100 桁を超える呼び出しの折返し(API shape)。message 組立(`append_*` 941 行、
builder 165 関数)は `fmt.Sprintf` 相当が無い std gap。`tree` / `scope` / `out` の
context 引数 576 行と `var x = empty_type()` 131 行は「Go の `(T, error)` 返しを out
引数 + `!?Diagnostic` で写した」API shape。`defer` / `errdefer` 402 行と `as_bytes`
束縛 325 行、owned copy 53 行が ownership の明示。error 伝播 `return move x;` 298 行は
Go の `if err != nil { return err }` 350 箇所と同数で、増分ではない。残る圧縮余地は
message builder の共通化(parser の `error_*` と同じ融合)と `tree` 引数の扱いだが、
2.0 未満に入ったので後続 module を優先する。

ownership の増分(+5,006 行)の分類。API shape が最大で、関数宣言が Go 418 に対して
Kizu 737(`Name` / binding の accessor、`ast_view` の node reader、`NameTable` の綴り
helper)、signature を 1 引数 1 行に折り返した 1,589 行、閉じ括弧だけの行が Go 1,845 に対して
Kizu 2,750(`if try f() |failed| {` の block 310 と 100 桁超の呼び出し折返し 243)。error
処理は `out` 引数の `var x = empty_name()` 120 行、`handled.* / out.*` 206 行、
`return move failed` 304 行で、Go の `if err != nil` / `return err` 457 行に対して 734 行。
ownership の明示は `defer` / `errdefer` 143 行、`as_bytes()` / `name_text` の view 束縛 246 行。
重複処理は `while` の `index = index + 1` 125 行(Go の `for range`)と diagnostic の
`Arg::` 262 行(Go は `errorf` の引数に inline)。型名・binding 名・修飾名を 1 つの
`NameTable` に intern して copy handle `Name` で渡す表現(types の `TypeTable` と同じ判断)が
`string` の自由な複製を吸収しており、それ以上の圧縮は signature 折返しの 1 行化だけで
1.5 を切れないため後続 module を優先する。

ir の増分(+3,900 行)の分類。API shape が最大で、signature を 1 引数 1 行に折り返した
545 行、閉じ括弧だけの行が Go 1,126 に対して Kizu 1,841(`if try f() |failed| {` の block 142
と 100 桁超の呼び出し折返し)、関数宣言が Go 298 に対して Kizu 540(arena handle / view の
accessor、out 引数の result type)、`(T, error)` 返しを out 引数で写した `out.* =` 151 行と
`var x = empty_*()` 146 行、`while` の `index = index + 1` 169 行(Go の `for range`)。
error 伝播は `return move failed` 138 + `|failed|` 142 で、Go の `if err != nil` / `return err`
258 と同数。ownership の明示は `name_text` + `as_bytes()` の view 束縛 128 組(`[]u8` view を
関数から返せない gap)と `defer` / `errdefer` 175 行。std gap は `fmt.Errorf` 相当の `Arg::`
131 箇所 + `diagnostic.kizu` 72 行、sequence literal の無い primitive 表(`call.kizu` +68 行)。
Go の ir が string literal を `%q`(`strconv.Quote`)で綴っていたのは `internal/quote.Bytes` への
統一の取りこぼしで、Go 側を揃えた(`kizu ir` の非 ASCII literal の表示だけが変わり、llvm / wasm
の `strconv.Unquote` は両方を読む)。Go が `string` で持つ op を `Op` enum + spelling
関数にした `ir.kizu` の約 200 行は Mechanical mapping の判断そのもの。2.0 未満に入ったので
後続 module(llvm)を優先する。

llvm の増分(+1,864 行)の分類。API shape が最大で、閉じ括弧だけの行が Go 762 に対して
Kizu 1,017(`if try f() \|failed\| {` の block と 100 桁超の `line*` 呼び出しの折返し)、折り返した
`Arg::` 引数行 170、Go が `fmt.Fprintf` の引数に inline する `localName` / `+ ".ptr"` /
`nextSyntheticValue` / `llvmType` / module 名の変換を `let` に束縛した行 345(入れ子の
`&var self` 呼び出しが引数に置けないため)、`while` の `index = index + 1` 77 行(Go の
`for range`)。ownership の明示は `name_text` + `as_bytes()` の view 束縛 236 行(`[]u8` view を
関数から返せない gap)と `defer` / `errdefer` 130 行。error 処理は `if try f() \|failed\| { return
move failed; }` 22 組で、Go の `if err != nil { return err }` 34 組より少ない(Go にも Kizu にも
tail call の `return self.write_x(...)` がある)。std gap は `fmt.Sprintf` / `Fprintf` 相当が無い
ことによる formatter(`Arg` union、`format_args` / `place_arg`、`line1..6` / `text1..5` /
`sprint*` / `fail0..4` / `wrap`)188 行、named constant set を enum + spelling / parse 関数にした
`PanicKey` / `FailureKey` 180 行(Go の table 39 行)、`strconv.Unquote` / `sort.Strings` /
`strings.Contains` 系 helper 約 145 行。module 単位の result type(`ValueInfo` / `ExitLabel` /
`SliceParts` / `ResultLabels` / `FailureCode` / `CapacityResult` / `SliceOperand` と callback の
代わりの `ContainerKind` / `TypeCollector`)約 80 行は API shape。行比 1.48 で 2.0 未満に入った
ので圧縮 pass は置かず、後続 module(native link)を優先する。

fmt module の増分(+445 行、1,234 / 789 = 1.56)の分類。error 処理は、Go の
`strings.Builder` / slice append が失敗しないのに対して Kizu の String / Array 操作を
明示的に伝播する `try` 73 行。ownership の明示は Token / comment の owner Array、
`defer` / `errdefer`、`as_bytes()` の local view、owner field を take できない Builder の
最終出力 copy を合わせて約 54 行。std gap は comparator 付き stable sort が無い先頭 import
range の insertion / rotation、`strings.Split` / `ContainsAny` / `TrimSpace` 相当の局所 helper、
view を返せない `tokenSpelling` の append 形で約 94 行。残る約 224 行は signature の折返し、
Go の `switch` / range loop を `if` / `while` に展開した API shape(閉じ括弧だけの差 +70 行を
含む)。code words は 3,963 / 2,375 = 1.67 で、改行だけの増分ではないが、この 4 分類で
説明できるため complete とする。

fmt + init work unit 全体は Go 1,024 行(`internal/fmt` 789 + `cmd/kizu/fmt.go` 92 +
`cmd/kizu/init.go` 143)、Kizu 1,786 行(fmt 1,234 + main の増分 539 + import 衝突 helper 5 +
fsutil の増分 8)で 1.74。words は 5,684 / 3,214 = 1.77。未移植表の init 37 行は
user-facing core の到達規模だったが、size gate は規則どおり `init.go` の全 declaration と
専用 helper を数えた。compiler module 累計は Go 1,023 行 / Kizu 1,437 行 = 1.40、words
4,880 / 3,294 = 1.48 なので 1.5 trigger 未満。work unit の増分は上の fmt 分類に加え、init の
明示 error message 組立、既存 file 2 件の owned path、`filepath.Abs` / exclusive create の
fs gap で尽きる。


native の増分(+245 行、490 + runtime_source wrapper 4 に対して Go 249。Go 側には
buildcache module へ回した cache key 3 関数 14 行を含む)の分類。buildcache を移植しない
代替(`TempDirs` / `claim_dir` / `clean_build_dir` / `remove_temp_file` / `create_dir_all`)が
65 行で、Go の `os.MkdirTemp` / `RemoveAll` / `MkdirAll` 8 行に当たる std gap。signature の
1 引数 1 行折返しが 60 行、`as_bytes()` の view 束縛が 30 行、`defer` / `errdefer` が 26 行
(API shape と ownership の明示)。spawn_wait8 の固定 8 引数形(`spawn_clang` の triple
有無の分岐)が 15 行、map の sorted iteration が無いことによる `sorted_keys` の copy-sort が
13 行、`fmt.Errorf` 相当の無い fail helper が 17 行(std gap)。圧縮 pass 済み: 手書き JSON
escaper と Metadata copy を `std::json` encoder に、8 slot argv copy を view 直渡しの
`spawn_clang` に置換し、752 → 494 で 2.0 未満に入った。words 比 2.02 は spawn の 8 引数
呼び出しと encoder の field 列が語数を持つためで、行の折返しでは動いていない。


native の cache 統合の増分(494 → 641、+147 行。Go 側は変わらず 249 で、cache key
3 関数 14 行を含む)の分類。host probe(`host_key` 38 + `append_host_of` 19 行)は
Go の `runtime.GOOS + "-" + runtime.GOARCH` 1 式に当たる std gap。cache key builder
3 関数(65 行)は Go の 14 行に対し、`/` join と `&var` receiver の入れ子制約で
`append_*` 形に展開した API shape。`executable` / `runtime_object` の Miss arm
(scratch copy 7 行 × 2 と discard / finish / clean の分岐)約 55 行は、closure を
module 境界へ渡せないことによる 2 段 API の caller 側 protocol(language gap)。
旧経路の削除と `create_dir_all` の fsutil への統合で -44。module 累計 2.58 は
gate 未達で、増分は上の 3 分類で尽きる: 残る圧縮余地は signature の折返し
(100 桁制限で不可)以外に見つからなかった。

buildcache の増分(+202 行、418 / 216 = 1.94)の分類。closure gap の 2 段 API
(`ArtifactPlan` / `PendingArtifact` の宣言と `finish_artifact` /
`discard_artifact` / `install_artifact` / `remove_scratch` / `artifact_hit` /
`output_name_of` / `output_path_of` の borrow shim)が約 75 行: shim は
「`errdefer` の cleanup receiver は error path で borrow されていてはならない」
規則が、view を callee scope へ閉じることを要求するため消せない。残りは
ownership の明示(`as_bytes` 束縛約 30、defer / errdefer 約 20)、`(T, error)`
返しを out 引数と result struct に写した API shape、eviction の selection sort
(generic sort が無い std gap、14 行)。Go 対応が標準 library の実装は sha256 と
同じ別記録の module(timestamp / fsutil)が持ち、この表の上の行にある。

## 見つかった gap

移植中に見つかった language / std gap と、その場で使った局所解。module 完了時に判断する。

| gap | 証拠 | 局所解 |
| --- | --- | --- |
| sequence literal(`[N]T{a, b}` / table / table-driven test)が無い | parser の precedence table(if 連鎖 23 行)、Go の table test 7 本が展開されている | 展開のまま |
| 文字列組立の std API が `append_*` だけ | parser の message helper 155 行、checker の `errorf` 315 箇所 | parser は `error_*` method に融合(parser.kizu) |
| struct field からの take / replace が無い | parser が cur/peek を持てず全 token を arena に保持。fmt の Builder から完成した `out` だけを取り出せない | parser は `Arena<Token>` + handle stream。fmt は完成時に出力を 1 回 copy し、Builder 全体を deinit |
| `orelse` の右辺に block を書けない | parser の early return | `if opt \|v\| {} else {}` に展開 |
| owner の `?T` は産出した場所で capture / orelse が必須 | parser test | 同上 |
| `[]u8` view は関数から返せず、`as_bytes()` は local binding に束縛必須 | loader の `module_name() -> []u8` 系 helper と fmt の `tokenSpelling(Token) -> string` が書けない | `&string::String` を返す / 渡し、callee 側で bind する。fmt は `append_token_spelling` に変える |
| comparator を受ける stable sort が無い(`std::sort` は owned String 専用) | fmt の `normalizeLeadingImports` は variable-length token range を `sort.SliceStable` で path key 順にする | Token owner を 1 Array に保ったまま、隣接 import range の stable insertion / rotation を局所実装 |
| alias import が無く、import は末尾 segment 名だけを束縛する | compiler main は既存の `std::fmt` と新しい `compiler::internal::fmt` の両方を使うため `fmt` が衝突する | 既存の decimal append 4 call だけを 5 行の `compiler::internal::stdfmt` wrapper 経由にする |
| error set を宣言しない `!T` は `try` 専用で `catch` できない | fmt の Go `insertMoveMarkers` は `loadFileProgram` の全 error で元 source を返すが、Kizu の `Loader.load_source -> !LoadResult` は load graph 内の fs / allocation failure を catch できない | `LoadResult::Error`、types diagnostic、ownership diagnostic は元 source のまま返す。診断になる前の fs / allocation failure だけは伝播し、project 全体の error set を移植途中で変更しない |
| current working directory を読む API が無い | init の Go `filepath.Abs(target)`。`TestSelfhostFrontend/init` の引数無し case は process の cwd を変えて比較する | `fsutil::absolute` は test / shell が cwd と同じ値に保つ `PWD` と `path::join` / `clean` を使う |
| file の exclusive create が無い | init の Go `writeNewFile` は `O_CREATE|O_EXCL` で事前検査後の race でも既存 file を上書きしない | `reject_existing_init_files` で `kizu.toml` と `src/main.kizu` を先に拒否してから `fs::write_file`。検査と write の間に別 process が作る race は残る |
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
| `std::process` は argv[0](実行ファイルの path)を出さない | CLI の lib dir 探索(Go は binary 隣の `lib/kizu`) | `--lib-dir` / `KIZU_LIB_DIR` のみ。既定は `lib/kizu` |
| method の `&T` 戻り値は ownership checker が borrow として追跡しない(`calledFunction` が free function と namespace 修飾名しか解決せず、`let s = self.text(n); s.as_bytes()` が view 初期化子と認識されない) | ownership の `NameTable`: intern した綴りの bytes を読む accessor | free function `name_text(names: &NameTable, name) -> &string::String` にし、call site で `let retained = ...; let bytes = retained.as_bytes();` と束縛する |
| IR lowering が `&var` 引数の slot 判定を method 名だけで program 全体に union する(`internal/ir/slots.go` `markLentMethodArgs`)ため、別 module に同名で `&var` 位置が違う method があると capture / payload binding の lowering が壊れる(`clone`、`is_plain_data_type` で project / types の関数が clang error になった) | ownership module の `ScopeStore.clone` / `Checker.is_plain_data_type` ほか | 衝突した method だけ名前を変えた(`clone_scope`、`binding_mut`、`check_call`、`check_value_block`、`is_plain_data`、`check_owned_string_method` など)。同名 method の `&var` 位置差を列挙する確認を module 完了時に行う |
| `test` block の本体は同 module の private field を読めない | ownership の `binding_test.kizu`(`BindingHandle.node` の比較) | 比較を同 module の `fn` に出す |
| `if try f() \|v\|` の capture から `&var self` method を呼べないため、borrow を返す accessor(`?&T`)は使いにくい | ownership の struct / enum / union 表の読み出し | accessor は copy 値(`?Name`、`?BindingHandle`、`bool`)を返す形にし、`Map.at` は accessor 内部で閉じる |
| Go の `defer` による状態復帰(`c.loopDepth--`、flag の戻し)を `defer` で書けない(cleanup method 呼び出し以外は `defer` 不可)上、`?Owner` 戻り値を `let` に束縛できない | ownership の `check_block` / `check_while_stmt` / generic instantiation の enter / restore | 失敗 branch と成功 path の両方に復帰を書く(`if try f() \|failed\| { restore; return move failed; } restore;`) |
| union の payload に `Allocator` / `Io` を持つ struct を置けない(llvm の inline payload layout #991 が `Allocator` を知らない) | ir の `LowerResult { Module(Module), Error(Diagnostic) }`(`Module` は `NameTable` の allocator を持つ) | `Lowerer` が `pub module` を持ち、caller が `Lowerer` を生かしたまま `&lowerer.module` を読む(Go の `Lower() (*Module, error)` の戻りを in place にした) |
| 値受け `self` の method(`fn (self: T) into_x() -> X`)は receiver を consume しない(consume になるのは `deinit` だけ)。`deinit` を宣言した型の field は move out できない | ir の `Lowerer` から `Module` を取り出す | `Lowerer` に `deinit` を宣言せず(導出)、module は取り出さず上の形にした |
| method 名だけで `&var` 位置を union する slot 判定(既出)の新しい衝突: `set`(`EnvStore.set` と `Array.set`)、`clone`(`EnvStore.clone` と `Array.clone`)、`resolve_type`(ir と project) | `project::Loader.qualify_decl` の match payload `value.tags.clone(..)` が slot 化され clang error | `bind` / `unbind` / `clone_env` / `resolve_bound_type` に改名。module 完了時に全 method 名の `&var` 位置差を列挙し、ir が増やした差は `tree` 引数の位置(常に parameter を渡すので slot 化しない)だけであることを確認した |
| `sync.Once` の process 内 cache(`project.StdErrorSets`)に当たるものが無い(shared mutable global を置かない) | ir の error set 番号付け | `project::Loader.std_error_codes()` を呼び出し側(CLI / corpus runner)が 1 回読み、`&StdErrorCodes` を lowering に渡す |
| `typ::Table.parse` の失敗は `!` で伝播し、Go の「parse できなければ text のまま」に当たる optional parse が無い | ir の `lowerReturnType` / `errorUnionParts` / `resolveMetaTypeDeep` | 空 text だけ guard し、checked program の spelling は parse できる前提にした |
| `ownership.Result` を値で次の phase へ渡せない(`Name` が Checker の `NameTable` に tied) | ir の `Lower(program, ownershipResult)` | `Checker` を caller が生かし、`retired_return_at` / `retired_try_at` / `retired_name_text` で読んで lowerer 側の `NameTable` へ copy する |
| `if try f(view) \|x\| { ... }` は capture の間 condition の借用が生きるため、body で `&var self` を呼べない | ir の `split_static_args` / `generic_bindings` | text を local `String` に copy してから split する |
| `&var ?T` の parameter を書けず、後の file で宣言される型の `?T` field も置けない(struct の field 検査が file 順) | ir の control.kizu(`lower_loop_header` の index phi) | union `LoopTest { Plain(Value), Indexed(cond, phi) }` で header の結果を運ぶ |
| `if`/`match` は statement と expression で別 node なので、値位置の `if`/`match` を statement として歩けない | ir の `statementValue` / `collectAssigned` / slot walk | `TrailingValue` union と、値位置専用の walk(`collect_assigned_if` / `collect_mut_borrows_value_stmt`) |
| `Map` を空にできず(`clear` が無い)、owner field の差し替えもできないので、Go が関数ごとに作り直す `map[string]T` を写せない | llvm の `values` / `blockExitLabel`(`writeFunction` が `= map{}` で作り直す) | entry に関数の generation 番号を持たせ、違う generation の entry を無いものとして読む |
| `Array.clear` / `truncate` は std 専用 method で user code から呼べない | llvm verify の `uses = nil`、corpus runner が std 関数を module から落とす処理 | `while values.pop() \|v\| { v.deinit(); }` で空にする。module の `functions` は全部 pop して user 関数だけ append し直す |
| 同じ式の中で `&var self.out` と `&self.names` を同時に渡せない(`self` 全体が借用済みになる)。逐次の文なら disjoint field の借用は通る | llvm の `line*`(`format_args(&var self.out, &self.names, ...)`) | `let names = &self.names;` に束縛してから `format_args(&var self.out, names, ...)` |
| 引数式の中で `&var self` method を入れ子に呼べない(`self.line2(fmt, Arg::Str(try self.own(...)))` は receiver の借用と衝突) | llvm の全 writer | 引数を先に `let` に束縛する |
| 関数が返した view(`deref_llvm_type(bytes)` の結果)をそのまま別の呼び出しの引数に置けない(escape 扱い)。`let x = f(view)` に束縛すれば通る。`let x = f(view) orelse ...` も escape 扱い | llvm の `takes_address_of` / `write_optional_types` | 束縛してから渡す。`orelse` の代わりに `if f(view) \|x\| {}` |
| union は copy payload だけでも move-only で、`!` を返す関数に渡した union 引数は `errdefer arg.deinit()` を要求する | llvm の `Arg` union(`fmt.Fprintf` の引数) | formatter は引数を 1 つずつ `place_arg(... move arg)` に渡し、各 helper は `errdefer a.deinit();` を並べる |
| union payload / struct field に local binding 由来の `[]u8` view を置けない(literal と literal 由来の戻り値だけ) | llvm の `Arg::Lit`(Go の string 引数) | 生成 text は `Name` に intern(`Arg::Str`)か owned `String`(`Arg::Owned`)で渡し、`[]u8` を返す helper(`llvm_binary_op` など)は `&NameTable` + `Name` を受けて literal だけ返す |
| closure / function value が無いので callback を取る Go 関数(`collectModuleTypeNames(collect func)`、`writeContainerNew(isResultType func)`)を写せない | llvm の header / container | 呼び分けを enum(`TypeCollector`、`ContainerKind`)で受け、中で match する |
| `typ::walk` の visitor は `&Node` しか受けず、部分木を render する `Type` handle を持てない | llvm の `collectErrorUnionName`(`typ.Walk` で ErrorUnion node を `String()` する) | `root_node` / `child_type` で明示的に再帰する |
| 子 process の出力 capture が無い(`spawn_wait8` は stdout / stderr 継承) | native の `runClang` / `compileRuntime`: Go は clang の CombinedOutput を成功時は捨て、失敗時は error に載せる。selfhost では clang の `-Woverride-module` warning が毎回 stderr に流れ、失敗出力を message に運べない | 失敗 message は「native error: clang failed: exit status N」の行だけ合わせる。`TestSelfhostNative` は selfhost 側 stderr から toolchain noise を落とし、Go の build が clang で失敗する case は比較から除外 |
| fs に `os.MkdirTemp` / `RemoveAll` / `MkdirAll` 相当が無い | native の一時 build directory 管理 | `TempDirs`(TMPDIR + `kizu-native-<monotonic_millis>-<連番>`)と `create_dir_all` / `clean_build_dir`(既知 file の削除 + rmdir)を module 内に書いた。buildcache module でも要るなら std gap として切り出す |
| `spawn_wait8` は引数 8 個の固定形 | clang の link argv は `--triple` 付きでちょうど 8。`run` の child args は exe + 7 個まで | clang は triple 有無で 2 つの呼び出し形に分岐。8 個を超える構成が要る場合は止めて報告する |
| `std::json` は `<` `>` `&` を `\u003c` 形に escape しない(encoding/json の HTML escape 差) | native の build metadata(`write_metadata`) | metadata の値にこれらが入るのは path だけで、`TestSelfhostNative` は絶対 path を正規化して比較する |
| generic 関数の呼び出しは static 引数の明示が必須(推論されない) | native の `sorted_keys<V>` | call site で `sorted_keys<i64>(...)` と書く |
| `runtime.GOOS` / `GOARCH` 相当が無い | native の toolchainKey(cache key の host 部) | claim した temp dir で `sh -c "PATH=/usr/bin:/bin uname -sm > file"` を実行して読む(spawn の出力 capture 不能の既知 gap の続き)。未知の host は guess せず error で止める |
| `os.CreateTemp` 相当が無い(乱数源も無い) | buildcache の scratch file(半端な artifact を key の名で見せないための build 先) | `fsutil::append_scratch_name`: `artifact-<key 先頭 16 桁>-<unix_millis>-<連番>`。並列プロセス間の distinctness は build 中の key が持ち、同じ key を同じ ms に 2 プロセスが build した場合だけ衝突して負けた側は明示的に失敗する。rename 前に fs error で止まると scratch が残る(Go は defer Remove が拾う) |
| `time` package 相当が無い(civil 変換・RFC3339・時刻比較) | buildcache の `Entry.CreatedAt`(Go は time.Time の JSON) | `compiler::internal::timestamp`(別記録): 書きは `std::process::unix_millis()` から civil 変換で RFC3339 UTC(fraction は RFC3339Nano と同じく trim)、eviction 順序は spelling の桁比較 `stamp_before`。offset 付き stamp は 0 扱い(両 CLI は Z しか書かない) |
