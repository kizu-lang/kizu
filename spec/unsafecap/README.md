# unsafe マーカーモデル

SPEC §12 の `unsafe` マーカー規則 —— どの操作が未証明で、どの marker がそれを覆い、
何が足りなければ checker が拒否するか —— を Quint で書いたモデルと、そのモデルから
作ったプログラムを checker に当てるモデルベーステストです。

```
spec/unsafecap/unsafecap.qnt        モデル: checker の判定、実際に走る義務、生成器、性質、mutant
spec/unsafecap/unsafecap_test.qnt   既知の形に対するモデルの判定(quint test)
scripts/unsafecap-mbt/              quint run の trace を層化して fixture に書く
internal/unsafecap/testdata/unsafecap_mbt.json   その fixture(checked in)
internal/unsafecap/mbt_test.go      fixture を 2 file の Kizu module にして checker に当て、モデルの予測と比べる
```

## 範囲

`internal/unsafecap` 自体は操作の種類名と help 文の表です。判定は `internal/types`
(`unsafe.go` と `checker.go` の `underMark` / `requireUnsafeCapabilityAt` /
`requireUnsafeStructInvariant`)と `compiler/src/internal/types/body_unsafe.kizu` 他に
あります。モデルが扱うのはその判定の入力全体で、SPEC §12 から次を洗い出しました。

| 入力 | SPEC §12 の規則 | モデル |
| --- | --- | --- |
| 操作の種類 | 種類表の 11 種(`ptr_read` … `volatile`)が未証明。`ptr_of` / `null` / `c"..."` / field の読み / 安全な関数と `fn(...)` 経由の呼び出しは未証明でない | `Atom` と文の形。11 種すべてと、安全な見かけの 4 種(`1` / `sf()` / `fs()` / `b.n`) |
| marker の位置 | 式の前置。覆った式の中の**すべて**を覆う。使用は最も内側の marker に数える | 1 文に 4 つの位置(操作 / 式 / 左右の operand)、各 0〜2 個 |
| 代入の marker | `unsafe p.* = v` の marker は代入先だけを覆う | `DerefSet` / `FieldSet` は値の式を覆わない。`ptr_write` / `volatile_write` は覆う |
| 未使用 marker | 何も覆わない marker、2 つ重ねた外側は compile error | `Unused` |
| `// SAFETY:` | `unsafe` を含む文に要る。外側の文の comment は内側の文に届かない | 文ごとの有無。式の中の block の 2 文は別に持つ |
| 関数の種類 | `unsafe fn` の本体は暗黙に覆われない。呼び出しに marker が要る | 2 つの関数 `g` / `f` がそれぞれ `fn` か `unsafe fn`。`f` は `g` を呼べる |
| 呼び出し先 | marker は呼び出しを覆うが、呼び先の本体は覆わない | `g` の本体は `g` の marker だけで判定 |
| 関数 pointer | `fn` 経由は不要、`unsafe fn` / `extern "c" fn` 経由は要る。型は宣言どおり | 3 種の型と 3 種の関数の束縛。型が違えば `FnTypeMismatch` |
| `defer` | 登録する呼び出しの規則は通常の呼び出しと同じ | `defer` に `ev` / `uv` / `sv` |
| let 初期化子の形 | `view_from_ptr` と `&var p.*` は `let` の初期化子でだけ | `ViewS` / `DerefBorrow` |
| `unsafe struct` | 構築と field 書き込みに marker、宣言 file の外では marker があっても拒否。読みは自由 | 関数ごとの file(Home / Away)、`Build` / `FieldSet` / `FieldRead`、`&var b.n` の貸し出し |

入れなかったもの:

* **`///` の要求**(`unsafe fn` / `unsafe struct` の doc)。宣言 1 つで決まる検査で、
  marker の相互作用が無く、既存の negative example が直接押さえています。
* **raw pointer field を持つ struct の `unsafe struct` 必須、`pub` field 禁止、
  `extern "c"` の型制限、可変長引数**。型の構文検査で、文脈との組み合わせが無い。
* **`Function` static parameter**。std 専用で利用者が書けない(SPEC §13)。
* **method としての `unsafe fn`、generic の instantiation、`comptime`、loop**。
  marker の状態は関数本体の検査に入るたびに空から始まる(`checkBlock(.., nil)`)ので
  本体の種類で変わらず、関数 `g` / `f` の 2 つで代表させました。
* **`unsafe` の中でも残る型 / move / borrow の検査**(§12 末尾)。ownership 側のモデルの
  範囲で、既存の `o_unsafe_does_not_disable_*` が押さえています。

## モデルの形

状態機械が `g`、次に `f` の文を 1 つずつ足し、終わったところで評価します。2 つの層が
あります。

* **checker 層** `errorsOf(p)`: checker と同じく、各操作を最も内側の marker に数える。
  そこから `Missing(Cap)` / `Unused` / `NoSafety` / `Boundary` / `FnTypeMismatch` の集合を
  出します。空集合は「受理しなければならない」。どの誤りを先に報告するかは SPEC に
  無いので、checker の最初の報告がその集合にあれば一致とします(ownership と同じ)。
* **義務層** `obligationsOf(p)`: 実際に何が走るか。関数 pointer 経由の呼び出しは
  **束縛された関数**の種類で、`g` の呼び出しは `g` の本体の操作として、`&var b.n` の
  貸し出しは field の書き込みとして数えます。各義務は「同じ関数の中に、それを囲む
  marker が 1 つでもあるか」で果たされたかを決めます。「最も内側」という概念を使わない
  ので、checker 層とは別の言い方です。

## 不変条件

checker 層が受理したプログラムについて、義務層が次を守ること(`soundness` は 4 つの論理積)。

| 性質 | 意味 |
| --- | --- |
| `noUncoveredOp` | 走る未証明操作には、同じ関数の中に囲む marker がある |
| `noCalleeLeak` | 許可は伝播しない: 呼び出し側の marker は呼び先 `g` の操作を覆わない |
| `noLaundering` | 関数 pointer 経由の呼び出しは、型の主張でなく呼ぶ関数として覆われる |
| `invariantAudited` | `unsafe struct` の不変条件を変える操作は marker の下で、宣言 file の中でだけ起きる |

性質が空振りしていないことは mutant で確かめます。`unsafecap.qnt` の末尾の module は
同じモデルの規則を 1 つ壊したもので、`quint run --invariant soundness` がそれぞれ
反例を見つけます(`just spec-unsafecap` が確かめる)。

| module | 壊した規則 | 破れる性質 |
| --- | --- | --- |
| `mutantInherit` | 呼び出しの marker が呼び先の本体も覆う | `noCalleeLeak` |
| `mutantUnsafeFnBody` | `unsafe fn` の本体全体が覆われる | `noUncoveredOp` ほか |
| `mutantLooseFnTypes` | 関数 pointer の束縛で型を見ない | `noLaundering` |
| `asImplemented` | `&var b.n` の貸し出しを書き込みとみなさない(#1925 で直る前の checker) | `invariantAudited` |

`asImplemented` が反例を出すことが `findings/unsafecap-quint-findings.md` の F1 です。
`spec` が SPEC どおりのモデルで、fixture はここから作ります。`specBorrow` は `spec` に
`&var b.n` の生成を足したもので、checker が F1 を直したときの比較相手です。

## モデルベーステスト

生成器のモードは ownership と同じ 3 つで、どれも `--invariant soundness` を検査しながら
走ります。

* `initGuided`: 違反を 1 つも足さない。引いた文が違反するときは、必要な marker と
  `// SAFETY:` を補い、Away では struct の構築 / 書き込みを読みに替えた文(`repair`)を
  足します。marker の置き方は「式全体に 1 つ」と「operand ごと」を引きます。
* `initOneMistake`: 新しい違反をちょうど 1 回まで許す。
* `initFree`: 何でも足す。

違反を許すモードでは、引いた文をそのまま足す代わりに、修復した文から marker を
1 か所(または `// SAFETY:`)だけ落とした文(`slip`)も引きます。引いた文をそのまま
足すと違反が 2 つ以上重なることが多く、checker は最初の 1 つしか報告しないので、
1 つの規則を飛ばす checker が他の違反の陰に隠れます。`slip` が無かったときは
「marker の無い `ptr_read` だけ」のプログラムが 5544 本中 0 本で、`ptr_read` を
検査しない checker を fixture が見逃していました。

`scripts/unsafecap-mbt` は各モード × seed の trace から完成したプログラムを重複なく
集め、「予測クラス(1 クラスなら操作の種類まで、複数ならクラスの組) × 使っている構文の
うち最も珍しいもの(`calls-g` / `fnptr` / `block` / `away` / `unsafe-fn` /
`covering-op` / `sum` / `plain`)」の bucket に分け、bucket ごとに 2 本残します。

`mbt_test.go` は各プログラムを `home.kizu`(prelude と `unsafe struct Buf`、Home の関数)と
`away.kizu`(Away の関数)の 2 file の module にし、`kizu check` と同じ type check →
ownership check を in-process で走らせます。missing marker は help 行
(`unsafecap.Hint`)で種類まで分類します。test は 3 つです。

* `TestUnsafeModelKnownShapes`: `unsafecap_test.qnt` と同じ既知形 49 本。11 種すべての
  無印の操作が、その種類を名指す missing marker として拒否されること(`許可なしの
  unsafe 操作は必ず拒否として検出される`)を含みます。さらに各形を判定を反転させて
  もう一度当て、必ず食い違いとして出ることを確かめます。どの形も同じ理由で落ちる
  renderer や、何にでも一致する分類器はここで落ちます。
* `TestUnsafeModelKnownDivergences`: findings の F3 の形を、checker の今の答え
  ごと固定します。checker が直ってモデルと一致すると落ち、既知形へ移すよう促します。
* `TestUnsafeModelTraces`: fixture の再生。

`KIZU_MBT_BIN=<kizu>` で外部の binary(`compiler/` から build したもの)に、
`KIZU_MBT_FIXTURE=<file>` で別の fixture に当てられます。

### 規模と結果(2026-11-08)

* `quint test`: 19 本が通る。
* 健全性の探索: `spec` の 3 モード × 2000 trace(seed 0x1、12 step)で `soundness` の
  違反なし。witness は `initGuided` で受理が 100%、違反を許すモードで `Unused` /
  `Boundary` / `FnTypeMismatch` / `NoSafety` のそれぞれに届く。
* mutant: 4 つとも 3000 trace 以内に反例。破れる性質は上の表のとおりで、`specBorrow`
  は 4 性質とも反例なし。
* fixture: 3 モード × 3 seed × 1000 trace から重複なしで 6435 本、175 bucket、
  残すのは 328 本。11 種すべてに「その種類の missing marker だけ」を予測するプログラムが
  入る。checker との食い違い 0。
* 間引かない全 6435 本を `KIZU_MBT_FIXTURE` で当てても食い違い 0(約 80 秒)。
* `specBorrow` の全 6406 本では 514 本が食い違い、すべて `field_borrow` を含む(F1)。
  76 本が「モデルは拒否、checker は受理」、438 本が marker を書いた形の拒否 / 未使用。
  F1 以外に checker が許してしまう食い違いは無い。
* checker への変異注入(手元で 1 か所ずつ書き換えて戻す、checked-in ではない)24 種が
  すべて `TestUnsafeModelTraces` 単独で落ちる。内訳は findings の「変異による確認」。

## 実行方法

```sh
just spec-unsafecap                                   # quint test、健全性と mutant の探索、fixture の再生成
go test ./internal/unsafecap -run TestUnsafeModel     # fixture を checker に当てる(go test に含まれる)
KIZU_MBT_BIN=path/to/kizu go test ./internal/unsafecap -run TestUnsafeModel
```

`quint`(0.33、rust backend)が PATH に要ります。Java は要りません。`go test` は
Quint を動かさず、checked-in の fixture だけを読みます。モデルを変えたら
`just spec-unsafecap` で fixture を作り直してください(2 分ほど)。

## 残課題

* **F1 の fixture**。checker は #1925 で直り、F1 の 3 形は既知形へ移しました。
  checked-in の fixture はまだ `spec`(`&var b.n` を生成しない)から作ったもので、
  `specBorrow` で作り直すのが残りです。
* **selfhost compiler への適用**。この環境には clang が無く、`compiler/` の native binary
  を build できませんでした。F1 / F2 の `compiler/` 側はソースを読んでの確認です。
  `KIZU_MBT_BIN` の経路は seed の Go binary で動作を確かめてあります。
* **1 本あたり約 13ms**。大半は std の読み込みで、fixture を増やすと線形に伸びます。
* **入れていない入力**(範囲の節)、特に method としての `unsafe fn` と generic の
  instantiation は、本体の marker 状態が空から始まることをコードで確かめただけです。
* **`quint verify`(Apalache)による網羅検査**。Java が無い環境ではランダム探索だけです。
