# ownership モデル

`internal/ownership` の borrow / move / deinit 検査を Quint で書いたモデルと、
そのモデルから作ったプログラムを checker に当てるモデルベーステストです。

```
spec/ownership/ownership.qnt        モデル: checker の判定、実行時の意味、プログラムの生成器、性質
spec/ownership/ownership_test.qnt   既知の形に対するモデルの判定(quint test)
scripts/ownership-mbt/main.kizu     quint run の trace を層化して fixture に書く(Kizu)
compiler/tests/mbt/ownership.json   その fixture(checked in)
internal/ownership/mbt_test.go      fixture を Kizu にして checker に当て、モデルの予測と比べる
```

## 範囲

対象は **1 つの owned `String` `s` と、その借用の生存期間と、`s` を手放す操作の相互作用**
です。#1913(ループ内で読んだ view が後の `deinit` を借用中と誤判定)と、その周辺で
同じ解析が判定するものに絞りました。プログラムは次の文の列です。

| token | Kizu | モデルでの役割 |
| --- | --- | --- |
| `view N` | `let vN = s.as_bytes();` | 共有 view(§14.4)。最後の使用まで `s` を借用 |
| `use_view N` | `print(vN);` | view の使用 |
| `borrow N` | `let rN = &s;` | 共有 borrow binding(§9) |
| `use_borrow N` | `print(rN.len());` | borrow の使用 |
| `read` | `print(s.len());` | `s` の共有読み。借用と両立 |
| `mutate` | `try s.append_bytes(allocator, "x");` | 排他操作。`try` の error exit でもある |
| `deinit` | `s.deinit(allocator);` | 明示 consume |
| `move` | `sink(allocator, move s);` | move による consume(`sink` が deinit) |
| `defer` | `defer s.deinit(allocator);` | 出口での consume の登録(§6.3.1) |
| `unrelated N` | `var uN = string::new(allocator); defer uN.deinit(allocator);` | `s` と無関係な owner。判定を変えてはいけない(#1913 は変わっていた) |
| `loop N` / `while N` … `end` | `for 0..2 \|iN\| {` / `var iN = 0; while iN < 2 {` … `}` | ループ。深さ 2 まで |

入れなかったもの:

* **`&var` 借用と `as_mut_bytes`**。排他借用の衝突規則は既存の negative example で
  直接押さえられており、生存期間の解析は共有借用と同じ経路を通ります。
* **`if` / `match` の分岐**。分岐ごとの consume 一致(§8)は別の規則で、生存期間との
  相互作用より先に分岐合流のモデルが要ります。
* **field path の借用、struct への view 捕捉、関数境界を越える tie**(ADR-0098/0100)。
  place と署名のモデルが必要で、最小の集合を超えます。
* **`errdefer`、owner 2 つ以上の相互作用、`return` の途中脱出**。

## モデルの形

状態機械が 1 文ずつプログラムを組み立て、終わったところで 2 つを評価します。

* **checker モデル** `errorsOf(p)`: SPEC の規則から、checker が出してよい診断の
  クラス(`Borrowed` / `UseAfterMove` / `DoubleDefer` / `Leak` / `LoopConsume`)の集合。
  空集合は「受理しなければならない」。どの誤りを先に報告するかは SPEC が決めていない
  ので、モデルは違反する規則を全部集め、checker の最初の報告がその中にあればよいと
  します。
  - 生存期間(§9): binding は後続の使用まで生きる。ループの外で宣言して中で読む
    binding は、次のターンが読むので本体全体で生きる(囲むループのそれぞれについて)。
    本体内で宣言した binding はターンと共に終わる。
  - 借用中の `mutate` / `deinit` / `move` は `Borrowed`(§9, §14.4)。
  - consume 後の使用は `UseAfterMove`、ループ本体での consume は `LoopConsume`(§8)。
  - cleanup の無い `try` と、consume されないまま出る出口は `Leak`(§8)。
  - `defer` は出口で走るときに `s` が consume 済みなら `UseAfterMove`、借用中なら
    `Borrowed`、2 つ目の登録は `DoubleDefer`(§6.3.1)。
* **実行時モデル** `faultsOf(p)`: ループを 0/1/2 回まわしたときに実際に起きること。
  buffer の生存、`append` による再確保(世代)、各 view の指す世代、出口での defer の
  実行を追い、`UseAfterFree` / `DanglingView` / `DanglingBorrow` / `SharedMutated` /
  `DoubleFree` / `LeakAtExit` / `LeakOnError` を出します。

## 不変条件

モデルが受理したプログラムについて、実行時モデルが次を守ること
(`soundness` はこの 6 つの論理積)。

| 性質 | 意味 |
| --- | --- |
| `noUseAfterMove` | move / deinit した `s` は再び使われない |
| `noDanglingView` | view は owner の buffer より長生きせず、再確保も越えない(借用中は deinit されない) |
| `noDanglingBorrow` | borrow は解放後に読まれない |
| `noSharedMutation` | 共有 borrow の下で `s` は変わらない |
| `atMostOneDeinit` | buffer の解放はどの出口でも高々 1 回 |
| `noLeak` | どの出口でも解放は少なくとも 1 回(§8) |

`quint run --invariant soundness` は、規則を 1 つ外したモデル(ループ持ち越しの生存、
`try` の error exit のリーク)ではそれぞれ反例を見つけます。性質が空振りしていない
ことの確認です。

## モデルベーステスト

生成器には 4 つのモードがあり、どれも `--invariant soundness` を検査しながら走ります。

* `initGuided`: 規則を 1 つも破らない文だけを足す。受理されるプログラム、特に
  入れ子ループと持ち越し読みを含むものが多く出ます。
* `initOneMistake`: 規則違反をちょうど 1 回まで許す。診断が 1 クラスに決まる
  プログラムで、checker がどの規則を適用したかを最もよく区別します。
* `initFree`: 何でも足す。複数の誤りが重なる形。
* `initTargeted`: 最初に診断クラスの組(`TARGETS`、実現できる 23 通り)を 1 つ選び、
  その組の規則だけを破り、ちょうどその組で終わるプログラムだけを完成させる。
  半分の実行は単一クラス(`SINGLE_TARGETS`)から選びます。

`initTargeted` は結果の種類での層化です。他のモードでは結果の分布が「どの文を
足すと何が壊れやすいか」で決まり、`Leak` と `UseAfterMove` が大半を占めます。
seed `0x1` の 3,000 回で `Borrowed` だけ / `DoubleDefer` だけで終わる実行は `initFree` で
9 / 12、`initOneMistake` で 44 / 23、`initTargeted` で 111 / 112 です。sweep 全体の
相異なるプログラムでは、旧来の 2,049 本のうち `Borrowed` だけ 21、`DoubleDefer` だけ 15、
`Borrowed+DoubleDefer` 3、`DoubleDefer+Leak` 2 だったものが、506 / 417 / 26 / 34 に
なりました。

simulator は `any` の有効な枝を等しく選ぶので、重みは枝の構造で付けます。

* `step` は `any { aim, prepare, any { 通常の文 } }` の入れ子です。targeted の実行では
  `aim`(目標の規則を新たに破る文)と `prepare`(目標の規則が破れる前提: `defer`、
  view / borrow、ループ、consume)が、それぞれ通常の文すべてと同じ重みで選ばれます。
  他のモードでは 2 つは無効で、`step` は前と同じ一様な選択です。
* `initTargeted` の単一クラス / 全体の半々は `nondet roll` で付けます。

`scripts/ownership-mbt/main.kizu` は各モード × seed の trace から完成したプログラムを重複なく
集め、「予測された診断クラスの組 × 使っている構文(`loop` / `while` / `nested` /
`carried` / `defer` / `unrelated` / `mutate` / `short`)」の bucket に分け、bucket ごとに
1 本だけ残します。何千回の実行で埋まる bucket も、まれな分岐で 1 回だけ埋まる bucket も
同じ重みになります。`short` は 2 文以下のプログラム(生成器の境界)で、長いものと
同じ bucket にあると hash の選択でほぼ残らないので分けています。最後にモードごとの
完成数と、診断クラスの組ごとのプログラム数・bucket 数を表示します。

seed は 32 bit に散らして渡します。rust backend では隣り合う seed(`0x1` と `0x2`)の
実行列が数十本目から一致し、旧来の 3 seed は実質 1.5 seed 分しか探索していません
でした(`initGuided` の相異なるプログラムは同じ 3,000 回で 979 本 → 1,612 本)。
`--max-samples` を 10,000 にすると quint(node)が trace を書く前に 4 GB の heap を
使い切るので、広げるときは `-seeds` を増やします。trace は 1 回の実行ごとに読んで
消します。

`mbt_test.go` は各プログラムを Kizu にし、`kizu check` と同じ type check → ownership
check を in-process で走らせ、診断の文面をクラスに分類して比べます。不一致は

* モデルが受理なのに拒否 / 拒否なのに受理
* 報告されたクラスがモデルの集合に無い
* モデルの知らない診断(生成器か renderer の不備)

のどれかで、プログラムと Kizu ソースを添えて落ちます。`KIZU_MBT_BIN=<kizu>` で
外部の binary(`compiler/` から build したもの)に、`KIZU_MBT_FIXTURE=<file>` で別の
fixture に当てられます。

### 規模と結果(2026-11-08)

* 生成: 4 モード × 3 seed × 3,000 回(36,000 trace、旧 3 モード × 3 × 1,000 = 9,000)。
  相異なる 16,697 本、1,034 bucket(旧 2,049 本、490 bucket)。`just spec-ownership`
  全体で約 113 秒(旧 約 34 秒)。
* checked-in fixture: 1,034 本。in-process で約 5.4 秒(旧 490 本で約 2.6 秒)。
  食い違い **0**。
* 一回限りの広いスイープ(`-seeds 10 -per 1000000`): 相異なる 54,020 本、1,178 bucket。
  生成 約 310 秒、replay 約 253 秒。食い違い **0**。
* mutant 確認(一時的に壊して戻したもの。食い違いの本数は 旧 fixture → 新 fixture):

  | 壊したもの | 旧 | 新 |
  | --- | --- | --- |
  | checker: 2 つ目の `defer` を許す | 9 | 44 |
  | checker: `try` の error exit の leak を見ない | 17 | 41 |
  | checker: ループ内の consume を許す | 1 | 3 |
  | checker: 借用中の `move` を許す | 2 | 14 |
  | モデル: ループ本体の `defer` がターン終了時に借用中でも `Borrowed` にしない(F1) | 1 | 4 |

  最後のものは `soundness` では見つかりません(どのプログラムも `LoopConsume` で
  拒否されるので受理の集合が変わらない)。
* ハーネスの自己検証: #1913 の修正前(`eb407c46^`)の checker に同じ test を当てると、
  広いスイープで 230 本、checked-in fixture で 20 本、既知形で 5 本(#1913 の 4 形と
  入れ子の 1 形)が食い違います。どれもループ内の読みの後の `deinit` / `move` /
  `append` を借用中と誤って拒否するもので、#1913 そのものです。修正前の selfhost
  binary(3f3bbd7a)に `KIZU_MBT_BIN` で当てても同じ 20 本と 5 本です。

モデル側の訂正 1 件と、その根拠は `findings/ownership-quint-findings.md` にあります。

## 実行方法

```sh
just spec-ownership                                  # quint test、健全性の探索、fixture の再生成
go test ./internal/ownership -run TestOwnershipModel # fixture を checker に当てる(go test に含まれる)
KIZU_MBT_BIN=path/to/kizu go test ./internal/ownership -run TestOwnershipModel
```

生成器は `go run ./cmd/kizu run scripts/ownership-mbt/main.kizu -- -seeds 10 -per 1000000`
のように `--` の後に `-model` / `-out` / `-samples` / `-seeds` / `-per` / `-modes` を取ります。

`quint`(0.33、rust backend)が PATH に要ります。Java は要りません。`go test` は
Quint を動かさず、checked-in の fixture だけを読みます。モデルを変えたら
`just spec-ownership` で fixture を作り直してください。

## 残課題

* **selfhost compiler への定常的な適用**。CLI モードは用意したが、`compiler/` の
  checker をこの fixture に当てる経路は `go test` に入っていません(binary の build が要る)。
* **`&var` / `as_mut_bytes`、`if` / `match`、field path 借用、`errdefer`** への拡張(範囲の節)。
* **複数クラスの予測が多い**。誤りが重なるプログラムは受理 / 拒否しか鋭く区別しない
  ので、`LoopConsume` を外す mutant は 3 本でしか落ちません(ループ内の consume は
  次のターンの `UseAfterMove` を必ず伴い、checker はそちらを先に報告しうる)。
* **targeted の完成率**。目標ごとに 5〜77% で、`Borrowed+DoubleDefer` が最も低い。
  完成しない実行は MAX_LEN までに目標へ届かず止まるもので、数で補っています。
* **`quint verify`(Apalache)による網羅検査**。Java が無い環境ではランダム探索だけです。
