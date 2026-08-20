# ADR-0091: owner は consume されなければならない

Status: 採用

## 背景

deinit は source 上の明示契約だが、checker は履行を追わず、leak は compile
error にならない。実測では examples に leak が 5 file ある。また
`fs::read_file` / `io::read_stdin` は runtime が確保した buffer を view 型
`[]u8` で返しており、解放経路が型上存在せず、allocator 引数のない hidden
allocation として公理にも違反している。

## 決定

1. **consume 強制。** deinit を持つ型(owner 型)の owned binding は、
   すべての exit path で consume されなければ compile error。consume は
   move / `x.deinit()` / `defer x.deinit();` のいずれか。owner を値で受け取る
   param も同じ義務を負う。`deinit(self)` body 内の owner field にも同規則を
   適用する(完全性検査)。
2. **owner 性は推移する。** owner field を持つ struct / union は自身も owner で、
   deinit を必須とする。転送だけの deinit body は宣言 1 行で生成できる
   (構文は SPEC で定める)。公理の定義を明文化する: hidden control flow とは
   **呼び出しが source に見えないこと**。`defer x.deinit()` が見える限り、
   宣言された生成 body は公理内である。
3. **owner 要素の collection は compile 時に閉じる。** shallow `deinit()` を
   型 error にし、要素ごと consume する操作(`deinit_all()`)だけを consume と
   認める。要素数は runtime 値だが、正しい始末操作の強制は型で決まる。
   —— **ADR-0119 がこれを置き換える。** cleanup の名前は `deinit` 1 つにし、
   `deinit_all` を廃止する。`deinit` が値と値が保持するものを解放するので、
   漏れる操作そのものが無くなり、別名で気づかせる必要が消えた。目的
   (要素の leak を compile 時に閉じる)は保たれる。
4. **明示 leak を追加する。** `mem::leak(x)` を consume として認め、
   leak-on-exit(短命 process が解放を OS に任せる)を合法に書けるようにする。
5. **fs / io の確保 API を直す。** 確保版(allocator と確保上限を明示。
   上限なしも明示語で書く)と caller buffer 版の両方を提供する。確保版の
   返り型は `String`。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 自動 Drop(Rust) | 呼び出しが source から消える。公理違反 |
| 転送 deinit の全手書き(Zig) | 集約の数だけ定型が量産される。宣言 1 行と情報量が同じ |
| collection leak を runtime 検出に任せる | 検出が test の網羅性に依存する。型で閉じられる |
| 分解 consume(Austral) | 量産が定義側から全 call site へ移り悪化する |
| `OwnedBytes` 新設 | 型を増やしてユーザーに区別を課す(ADR-0090 と同じ却下理由) |

## 影響

- `internal/ownership`: scope 終端検査、deinit 完全性、collection 規則
- SPEC: 公理の定義、deinit 生成宣言、`mem::leak`、fs / io API
- 既存修正: examples 5 file(defer 追加)と fs / io の call site
- ADR-0092(allocator): testing_allocator の役割が縮むため改稿して確定する

## 再評価条件

- 上限なし明示語が形骸化したら、確保上限の要求水準を見直す
- field 単位の move out が入ったら、deinit 必須の代替として分解 consume を再検討
