# C header import モデル

SPEC §12.2 の header import(`kizu import-c-header`)—— どの C 宣言をどの
`extern "c" fn` に変換し、何を拒否するか —— と、書き出した宣言を `kizu check` が
どう扱うか、その宣言を呼ぶ Kizu 関数が何を要求されるかを Quint で書いたモデルと、
そのモデルから作ったプログラムを importer と checker に当てるモデルベーステストです。

```
spec/cimport/cimport.qnt          モデル: importer の判定、checker の判定、C 型の真の対応、生成器、性質、mutant
spec/cimport/cimport_test.qnt     既知の形に対するモデルの判定(quint test)
scripts/cimport-mbt/main.kizu     quint run の trace を層化して fixture に書く(Kizu)
compiler/tests/mbt/cimport.json   その fixture(checked in)
compiler/src/cimport_mbt_test.kizu  fixture の header を compiler/ の importer で import し、出力と呼び出し側を check して、モデルの予測と比べる
```

## 範囲

| 入力 | 規則 | モデル |
| --- | --- | --- |
| 宣言の種類 | SPEC §12.2 の対応 / 非対応表 | prototype、`#define`(`;` が無く次の宣言を飲み込む)、`typedef`、`struct`、`enum`、変数宣言 |
| 引数列 | `(void)` / `()`、可変長 / 関数 pointer / 配列は拒否 | `void` / 空 / 1〜3 個。各引数は通常 / 配列 / `...` / 関数 pointer / 空 |
| 基本型 | §12.2 の表(`char` … `double`) | 表の 22 綴りと、表に無い 5 綴り(`long` / `_Bool` / `uintptr_t` / `short int` / `widget`) |
| 修飾子 | `const T*` → `ptr<const T>` | 基本型の前後の `const` / `volatile`、各 `*` の後の `const` / `volatile` / `restrict`、0〜3 段 |
| 引数名 | 無名の pointer 引数は `p<i>` | 普通の名前、Kizu の keyword(`fn` / `match`)、無名。無名の最後の語が名前に取られる規則は importer どおり |
| 戻り値 | 単純な pointer | `int* f` と `int *f`(後者は importer が拒否)、0〜2 段 |
| 宣言の数 | 1 つ目の拒否で import 全体が止まる | 1〜2 個。2 個目は別名、同名(重複)、非対応の種類 |
| checker | keyword の名前、`void` 引数、関数の重複 | 書き出した宣言に対して |
| 呼び出し側 | `extern "c" fn` の呼び出しには `unsafe`(§12) | marker の有無、各引数の型(C の型どおり / read-only / nullable / 幅違い / `[]u8`)、結果を non-null で受けるか nullable で受けるか |

入れなかったもの:

* **comment と空白の扱い**。生成する header に comment は無く、空白は 1 つ。
* **marker 規則そのもの**(`// SAFETY:`、未使用 marker など)。`spec/unsafecap/` の範囲です。
  ここでは呼び出しが marker 付き(`// SAFETY:` 付き)か無印かだけを持ちます。
* **`extern "c" struct`、関数 pointer 型、可変長引数の呼び出し**。importer が書き出さない形です。
* **`restrict` の意味**。Kizu に対応する型が無く、別名を作らない義務は呼び出しの
  marker が引き受けるものなので、真の対応では落としてよいものとしました(findings の F6)。
* **K&R の `int f()`**。真の対応でも引数無しとしています(findings の F5)。

## モデルの形

1 回の `init` で header と呼び出し側を全部引き、`step` は何もしません(`--max-steps 1`)。
3 つの層があります。

* **importer 層** `importOf(pol, h)`: importer と同じく header を語の列として読みます。
  引数の最後の語は `*` でなければ名前、残りの語から `*` と修飾子を除いたものを表で
  引きます。修飾子と pointer の結果をどう写すかは `Policy` が決めます。
* **checker 層** `checkErrs` / `useErrs`: 書き出した宣言と呼び出し側の診断クラス。
  型の一致は「同じ型か、non-null の pointer を同じ nullable の pointer の位置へ」だけ
  (実装の `sameType` と `fillsNullablePointer`)。どの誤りを先に報告するかは SPEC に
  無いので、実装の最初の報告がその集合にあれば一致とします(ownership、unsafecap と同じ)。
* **真の対応** `truthOf(t)`: 語ではなく宣言の構造から C の型を読みます。各 `const` /
  `volatile` がどの段の object に付くか、pointer の結果は NULL でありうること。

`Policy` の 3 つの規則は importer の今の振る舞いです。

| 規則 | `spec`(忠実な写像) | importer(`asImplemented`) |
| --- | --- | --- |
| `const` | 各段の pointee にそのまま付ける。最外の pointer 自身の `const` は値渡しなので落とす | どこかに `const` があれば最内の pointee にだけ付ける(F1) |
| pointer の結果 | `?ptr<T>` | `ptr<T>`(F2) |
| pointee の `volatile` | 対応する型が無いので拒否(`Import:volatile`) | 落とす(F3) |

`spec` の欄は性質を満たす写像の 1 つで、仕様の決定ではありません(findings の「判断を要すること」)。

## 不変条件

importer と checker が受理したプログラムについて(`soundness` は 4 つの論理積):

| 性質 | 意味 |
| --- | --- |
| `declsTrue` | 書き出した宣言は C の型どおり: 同じ基本型、各段の同じ `const`、pointer の結果は nullable、pointee に `volatile` が無い。引数は non-null でよい(NULL を渡せない呼び出し側は C に求めるものが少ない) |
| `noWriteThroughConst` | read-only の memory が、C が書き込みうる引数に届かない |
| `noNullLaundering` | NULL でありうる C の pointer の結果が non-null として受け取られない |
| `noUnmarkedCall` | C の呼び出しはすべて marker の下 |

性質が空振りしていないことは mutant で確かめます(`just spec-cimport`)。

| module | 壊した規則 | 破れる性質(`initFree`、3000 trace) |
| --- | --- | --- |
| `asImplemented` | F1 + F2 + F3(**今の importer**) | `declsTrue`、`noNullLaundering` |
| `mutantConstCollapse` | F1 | `declsTrue`(`noWriteThroughConst` は `initReadonly` で数百 trace、`initFree` では 2 万 trace でも届かない。`constPointerTest` が直接押さえる) |
| `mutantRetNonNull` | F2 | `declsTrue`、`noNullLaundering` |
| `mutantDropVolatile` | F3 | `declsTrue` |
| `mutantUnmarked` | 呼び出しに marker が要らない | `noUnmarkedCall` |
| `mutantSign` | 表の `unsigned int` を `i32` に | `declsTrue` |

`spec` では 4 性質とも反例がありません。

## モデルベーステスト

生成器のモードは 4 つで、どれも `--invariant soundness` を検査しながら走ります。

* `initGuided`: 誤りを入れない。修飾子は自由(`volatile` を除く)、呼び出し側は C の型どおり。
* `initOneMistake`: 20 の slot(各型の基本型、名前、引数の形、引数列、2 個目の宣言、
  呼び出し側の各型、marker、関数名、`volatile`)のうち 1 つにだけ誤りを入れる。
* `initFree`: 何でも。
* `initReadonly`: 誤りは呼び出し側だけで、引数ごとに C の型どおりか read-only の
  pointer を渡す。F1 が呼び出しまで届く形(`char *const p` に read-only を渡す)は
  他のモードでは 1 万本に 1 本で、fixture に入りませんでした。

各プログラムは 2 つの予測を持ちます。`spec`(忠実な写像)と、importer が違うところでは
`implemented` とそれを説明する所見(`F1` / `F2` / `F3`、各規則を 1 つずつ importer の
ものにして結果が変わるか)。`scripts/cimport-mbt/main.kizu` は 4 モード × seed の trace から
重複なく集め、「`spec` の予測クラス × `implemented` の予測クラスと所見 × 使っている構文の
うち最も珍しいもの(呼び出し側が C の型と違えて渡す引数の種類 `arg-slice` / `arg-nullable` /
`arg-wide` / `arg-readonly`、次に `two-decls` / `deep-pointer` / `unnamed` / `call-2` /
`ptr-ret` / `call` / `pointer` / `plain`)」の bucket に分け、bucket ごとに 3 本残します。

replay は `compiler/src/cimport_mbt_test.kizu` で、`compiler/` の importer と checker を
見ます。header を importer(`cimport::import_header`)に通し、
書き出した宣言の文字列をモデルの予測と比べ、宣言と呼び出し側を 1 file の module にして
`kizu check` と同じ type check → ownership check を in-process で走らせます。比べる相手は
`implemented` があればそれ、無ければ `spec` です。`implemented` に合わず `spec` に合えば
「直った」と報告して落ちます(fixture と findings の更新を促す)。test は次の 3 つです。

* `C import model harness agrees on the known shapes and notices each flipped verdict`:
  `cimport_test.qnt` と同じ既知形 40 本(F1〜F3 の形を含む)。
  各形を判定を反転させてもう一度当て、必ず食い違いとして出ることも確かめます。
* `C import model known shapes cover every class the classifier knows`: 分類器が知っている
  16 クラスのすべてを既知形が使うこと。
* `C import pipeline agrees with the sampled model programs`: fixture の再生。

`KIZU_MBT_FIXTURE=<file>` で別の fixture に当てられます。

### 規模と結果(2026-11-08)

* `quint test`: 21 本が通る。
* 健全性の探索: `spec` の 4 モード × 3000 trace(seed 0x1)で `soundness` の違反なし。
  `initGuided` は受理が 98%、`initOneMistake` で checker の 3 クラスと呼び出し側の
  型違いに届く。
* mutant: 6 つとも 3000 trace 以内に反例(上の表)。
* fixture: 4 モード × 3 seed × 1000 trace から重複なしで 6976 本、358 bucket、残すのは 837 本。
  うち 516 本が importer の食い違い(F1 261 / F2 284 / F3 172、重複あり)を持ち、24 本が
  「モデルは拒否、importer + checker は受理」(F3 11、F2 7、F1 6。F1 の 6 本のうち 3 本は F2 も含む)。
  Go の importer + checker との説明のつかない食い違い 0(約 1.5 秒)。
* 間引かない全 6976 本を `KIZU_MBT_FIXTURE` で当てても 0(約 16 秒)。
* `compiler/` から build した binary を外部 binary として Go の replay に当てても既知形 40 本と
  fixture 837 本が 0、全 6976 本も 0(約 4 分)。
* importer / checker への変異注入(手元で 1 か所ずつ書き換えて戻す、checked-in ではない)
  29 種のうち 27 種が Go 実装の fixture の再生単独で落ちた。残り 2 種は振る舞いの
  変わらない変異です。同じ 29 種を `compiler/` の importer / checker に入れても、`compiler/` 側の
  fixture の再生で本数まで Go と同じに出る(2026-11-11)。内訳は findings の「変異による確認」。
* `compiler/` 側の replay(2026-11-11): 既知形 40 本と fixture 837 本が 0、間引かない全
  6976 本も 0(約 56 秒)。`kizu test compiler` に約 5 秒足す。
* モデル側の変異(2026-11-11): Kizu の生成器で fixture を作って Go と `compiler/` の replay に
  当てると、食い違いの本数は両者で同じだった。

  | fixture の元 | 本数 | 食い違い |
  | --- | --- | --- |
  | `IMPL` の `constCollapse` を `false` にしたモデル | 648 | 162 |
  | `IMPL` の `dropVolatile` を `false` にしたモデル | 742 | 24 |
  | `IMPL` の `retNonNull` を `false` にしたモデル | 799 | 359 |
  | `asImplemented` / `mutantConstCollapse` / `mutantDropVolatile` / `mutantRetNonNull` | 519〜755 | 0 |
  | checked-in fixture の 2 本の判定を反転したもの | 837 | 2 |

  mutant module は `expected` の写像だけを変え、`implemented`(`IMPL`)は変えないので、
  replay は食い違いません。`IMPL` を変えたモデル(importer の今の振る舞いを誤って書いた
  モデル)は replay が落とします。`mutantSign` / `mutantUnmarked` は所見で説明できない
  食い違いを生成器が見つけて止まります。

## 実行方法

```sh
just spec-cimport                                   # quint test、健全性と mutant の探索、fixture の再生成
just selfhost                                       # compiler/ の importer と checker に当てる(kizu test compiler に含まれる)
```

`quint`(0.33)が PATH に要ります。Java は要りません。backend は TypeScript です。
rust の evaluator は初回に GitHub から取得するもので、取得できない環境では動かないためです。
TypeScript の simulator は operator を引数に取る定義を入れ子に呼ぶと外側の operator を
見るので、入れ子の走査は `foldl` で直接書いています(`cimport.qnt` の `lexists` の comment)。
`kizu test compiler` は Quint を動かさず、checked-in の fixture だけを読みます。モデルを変えたら
`just spec-cimport` で fixture を作り直してください(1 分ほど)。生成器は
`kizu run scripts/cimport-mbt/main.kizu -- -main asImplemented -invariant none`(`kizu` は
`just stage1` で build したもの)のように `--` の後に `-model` / `-main` / `-out` /
`-samples` / `-seeds` / `-per` / `-invariant`(`none` で検査なし)を取ります。

## 残課題

* **F1〜F3 の判断**(findings)。直したら fixture を作り直すと `implemented` が消えます。
* **`quint verify`(Apalache)による網羅検査**。Java が無い環境ではランダム探索だけです。
  状態は 1 手で、入力の空間は有限なので、網羅検査には向いた形です。
* **comment / 空白 / 改行の位置**は生成していません。
