# internal/ir の安全関連モデル

`internal/ir` のうち安全性に効く 3 つの判断を Quint で書いたモデルと、モデルから
作ったプログラムを lowerer / optimizer に当てるモデルベーステストです。構成は
ownership checker の検証(`spec/ownership/`)に揃えています。

```
spec/ir/slots.qnt, slots_test.qnt    local に storage を与える判定(slots.go)
spec/ir/bounds.qnt, bounds_test.qnt  index check の挿入 / 省略(bounds.go)
spec/ir/defer.qnt, defer_test.qnt    defer / errdefer の実行位置(defer.go, control.go)
scripts/ir-mbt/                      quint run の trace を層化して fixture に書く
internal/ir/testdata/*_mbt.json      その fixture(checked in)
internal/ir/slots_mbt_test.go        slots: lowered IR の local.slot をモデルと比べる
internal/ir/runtime_mbt_test.go      bounds / defer: wasm で実行し、出力をモデルと比べる
internal/ir/testdata/wasi_runs.mjs   その実行に使う node の WASI driver
```

## 範囲と観測手段

性質ごとに「何を見れば正しさが決まるか」が違うので、観測手段を分けました。

| 対象 | 性質 | 観測 | 見えるもの | 見えないもの |
| --- | --- | --- | --- | --- |
| slots | `&var` で渡す local だけに storage がある | `Lower` 直後の IR dump の `local.slot`(in-process) | どの local が slot を得たか | slot を誤って得たことの実行時の帰結(#1914 の match が payload を move する) |
| bounds | index が範囲外なら必ず止まる / `set` が失敗する | wasm32-wasi に `--opt` で build し node で実行 | 範囲外アクセスの結果(panic・error・続行) | native(LLVM)経路。消去の精度(何本答えたかは log にだけ出す) |
| defer | §6.3.1 の出口すべてで正しい cleanup が逆順に走る | 同上 | cleanup の実行列 | cleanup が consume する owner の扱い(ownership が持つ) |

* **slots は IR の形が性質そのもの**なので dump を読みます。判定は関数単位で 1 回なので、
  main の各 local の初期値を一意な定数にして、`local.slot` の引数から local を引きます。
* **bounds と defer は IR の形では決まりません**。消去が正しいかは「範囲外で止まるか」、
  cleanup が正しいかは「何がどの順で走るか」で、どちらも実行して初めて分かります。
  この環境には clang が無く native では走らないので、Go 実装の wasm backend
  (`ir.Optimize` → `wasm.LowerTarget`)まで in-process で通し、node の WASI で走らせます。
  wasm 経路は `ReshapeForEngine` で `EliminateBoundsChecks` を 2 回走らせ、
  `VersionLoopsForBounds` / `RotateLoops` も通るので、native より optimizer の面が広い
  一方、**native 固有の LLVM 側の振る舞いは見えません**。backend のバグも食い違いとして
  出ますが、それは IR と区別できません(今回は出ていません)。
* 入力は `process::arg` から 10 進で読みます。定数にすると `ConstantFold` / `Inline` が
  消去の判断より先に答えを出してしまうためです。
* `verify.go` は optimizer の後に毎回走るので、IR の不整合は build 失敗として観測されます
  (F1 はこれで見つかりました)。

入れなかったもの: slots の field path・generic・`match` の payload binding、bounds の
view(`[]T`)の index と `swap` / `remove`・2 段のループ・`VersionLoopsForBounds` の版分け
自体のモデル、defer の labeled `break`・`orelse` / `catch` の guard 出口・`move` で
retire される errdefer(ADR-0114、ownership の判定と一緒にモデル化が要る)。

## モデル

### slots(`slots.qnt`)

型 A/B/C がそれぞれ同名のメソッド `poke` / `note` を、receiver と引数の受け方
(`&` / `&var` / 値、`i64` / `&var i64`)を変えて宣言し、main が呼びます。
`Rule` で判定の仕方を切り替えます: `Identity` は呼び出しが届く宣言(#1914 以降の
slots.go)、`ByName` は同名の宣言すべて(#1914 以前)。

| 性質 | 意味 |
| --- | --- |
| `noLostWrite` | 届いた宣言が `&var` で受ける local には storage がある(無ければ callee の書き込みが消える) |
| `onlyReached` | storage を得るのは届いた宣言が `&var` で受ける local と、defer の receiver だけ |

`ByName` では `onlyReached` が破れる反例が出ます(`--init initByName`)。

### bounds(`bounds.qnt`)

`a: &var Array<i64>`(長さ 0..3)と入力 `k`(-1..3)に対する、`get_or_panic` /
`set` / `pop` / `append`、`if idx < a.len()` / `if idx >= 0` / 早期 return の guard、
`let n = a.len(); for 0..n` と `while i < a.len()` のループ(深さ 1)の列です。
`outcome` は実行時の意味(範囲外の `get_or_panic` は停止、`set` は `-7` を返す)、
`skippable` は「index が非負で長さ未満と分かっている、または同じ index を header が
変わってから検査済み」という省略の規則です。

| 性質 | 意味 |
| --- | --- |
| `skipSound` | 規則が省略するアクセスは、到達するすべての入力で範囲内 |
| `terminates` | どの入力でも step 予算内に終わる(`while` の中に `append` は生成しない) |

`Rule = Keep`(pop / append で事実を忘れない規則)では `skipSound` が破れます
(`--init initKeep`。例: `for` 本体の `pop` の後の `set(i)`)。`skippable` は実装の精度と
比べません。層化の bucket(`answered`)に使い、消去が起きそうなプログラムと起きては
いけないプログラムを両方 fixture に残すためのものです。`for 0..e` の上限を何回評価するかは
SPEC に書かれていないので、生成するプログラムは `let n = a.len();` で 1 回だけ読む形に
しています。

### defer(`defer.qnt`)

`defer note(id)` / `errdefer note(id)`、`if bit(x, b)`、`for 0..2`(深さ 2 まで)、
`bit(x, b)` で条件付きの `return` / `try fail(..)` / `break` / `continue` の列です。
`runOn` が §6.3.1 どおりに解釈し、出力と、登録・frame の出方・実行回数の台帳を残します。

| 性質(`exitsSound`) | 意味 |
| --- | --- |
| 出口 | 登録した frame はどれも出口を通る |
| 回数 | defer はちょうど 1 回、errdefer は frame を error return で出たときだけ 1 回 |
| 順序 | 1 つの frame の cleanup は登録の逆順 |

ジャンプで出るときに cleanup を飛ばす規則(`initFallthroughOnly`)と、errdefer を常に
走らせる規則(`initErrDeferAlways`)ではそれぞれ反例が出ます。

## モデルベーステスト

`scripts/ir-mbt` は各モデルを 3 seed × 1000 回走らせ(`--invariant soundness` 付き)、
完成したプログラムを重複なく集め、「予測 × 使っている構文」の bucket ごとに
slots / defer は 2 本、bounds は 1 本残します(bounds は 1 本あたり 20 入力で、停止する
入力ごとに wasm instance を作り直すので重い)。

* `slots_mbt_test.go`: 各プログラムを lower し、main の `local.slot` の集合をモデルと比べます。
* `runtime_mbt_test.go`: fixture 全体を 1 つの Kizu プログラム(case ごとに 1 関数)にして
  1 回 build し、node の 1 プロセスで全入力を走らせます。1 instance が停止するまで順に
  回し、停止した run だけ記録して次の instance で続けます。全体の build が落ちたら
  二分して build できない case を特定し、残りを走らせます。node が無ければ skip します
  (cmd/kizu の wasmtime テストと同じ扱い)。

`KIZU_MBT_BIN=<kizu>` で外部の binary(`compiler/` から build したもの)を使います
(slots は `<bin> ir`、bounds / defer は `<bin> build --target wasm32-wasi --emit wasm --opt`)。
`KIZU_MBT_FIXTURE=<file>` で別の fixture を当てます。

### 結果(2026-11-08、基点 24b0b09a)

* checked-in fixture: slots 283 本、bounds 252 本(4,980 回実行)、defer 187 本(2,992 回)。
  bounds では optimizer が case 関数のアクセスを 113 個答えています。
  `go test ./internal/ir -run 'Model|KnownDivergences'` で約 25 秒(この環境。
  `TestIRCorpus` 単独で 38 秒かかる環境です)。
* 一回限りの広いスイープ(8 seed、bucket の上限なし): slots 772 本、bounds 413 本
  (8,160 回)、defer 589 本(9,424 回)。
* **食い違い**: bounds で **F1**(wasm `--opt` で build が `ir verify error` になる。
  `findings/ir-safety-quint-findings.md`)。checked-in で 3 本、スイープで 5 本。
  実行結果の食い違い(範囲外で止まらない、cleanup の列が違う)は **0** です。
  F1 は findings に記録したまま判断していないので、テストは同じ署名の build 失敗を
  `knownDivergences` としてログに出し、`TestKnownDivergencesReproduce` が最小再現が
  まだ再現することを確かめます(直ったらそこが落ちるので、一覧から消します)。

### ハーネスの自己検証

* slots: #1914 修正前(`1c3db9a7^`)の lowerer に同じ test を当てると、checked-in の
  283 本のうち `name-differs` bucket の 100 本ちょうどが食い違い、それ以外は 0 本。
  既知形 `issue1914/shared-methods-lend-nothing` も落ちます。
* bounds / defer: 一時 worktree で本体に変異を入れて当てた結果(変異は commit していません)。

| 変異 | 内容 | 食い違うプログラム |
| --- | --- | --- |
| B1 | `leavesHeaders` が `array.pop` を header を変えないと扱う | 24 本 + 既知形 2 |
| B2 | `i < n` から `i >= 0` も学ぶ | 11 本 + 既知形 1 |
| D1 | `break` / `continue` が loop 本体の frame の cleanup を飛ばす | 6 本 + 既知形 1 |
| D2 | error return で errdefer を走らせない | 21 本 + 既知形 2 |

## 実行方法

```sh
just spec-ir                                   # quint test、性質の探索、fixture の再生成
go test ./internal/ir -run 'Model|KnownDivergences'
KIZU_MBT_BIN=path/to/kizu go test ./internal/ir -run 'Model'

# 性質が空振りしていないこと(どれも violation が出るのが正しい)
cd spec/ir
quint run slots.qnt  --init initByName          --backend rust --max-samples 2000 --invariant soundness
quint run bounds.qnt --init initKeep            --backend rust --max-samples 3000 --invariant soundness
quint run defer.qnt  --init initFallthroughOnly --backend rust --max-samples 2000 --invariant soundness
quint run defer.qnt  --init initErrDeferAlways  --backend rust --max-samples 2000 --invariant soundness
```

`quint`(0.33、rust backend)と、runtime の test には `node`(WASI preview1)が要ります。
Java は要りません。`go test` は Quint を動かさず、checked-in の fixture だけを読みます。

## 残課題

* **selfhost compiler への適用**。`KIZU_MBT_BIN` の経路は用意したが、この環境には clang が
  無く selfhost binary を build できないので、`compiler/` 側には当てていません。F1 と同じ
  形(`apply_bounds_plan` の `site` が呼び出しごとに 0 から)はコード上 selfhost にもあります。
* **native 経路**。clang のある環境で、同じ fixture を native build でも走らせる経路。
* 範囲の節で入れなかったもの。特に `VersionLoopsForBounds` の版分けと view の index。
* **`quint verify`(Apalache)による網羅検査**。Java が無い環境ではランダム探索だけです。
