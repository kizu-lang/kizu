# ADR-0092: Allocator は opaque capability のまま、実体化しない

Status: 採用

Issue: #549

## 背景

`Allocator` は SPEC 上「visible opaque capability type」だが、runtime の実体は
NULL token である(`internal/native/build.go` の `mem_page_allocator` は
`return NULL`)。Array / Arena / String は渡された allocator を無視して直接
malloc / realloc を呼ぶ。#549 はこの状態を解消するか
(user-defined allocator / testing allocator / fixed buffer)を問う。

本 ADR の初稿は「leak は compile error ではない(ADR-0090)。leak 検出の
受け皿は runtime 側にしか置けない」を前提に、allocator の handle 実体化と
`testing_allocator()` を提案していた。この前提は ADR-0091(owner consume
強制)で覆った。owner の放置は今は compile error であり、runtime まで
漏れ得るのは明示 `std::mem::leak()` と std / runtime 自体のバグだけである。

## 決定

### 1. user-defined allocator は当面入れない(opaque 維持)

この決定は ADR-0129 が改訂した。開く条件として下に置いた「実需」が出たため。

`Allocator` は user が実装できる contract にせず、std factory だけが作れる
opaque capability のまま保つ。理由は可逆性の非対称にある。

- 閉 → 開は additive(contract 化して開けば既存コードは全部通る)
- 開 → 閉は breaking(container が user state を参照する形が既成事実になり、
  field borrow と user state lifetime の将来設計を先取りで縛る)

「自作 allocator でなければ満たせない実需」(特殊メモリ、workload 特化戦略)は
現ユーザーに存在しない。pool 系は `std::arena` が、heap のない環境は
fixed_buffer(延期、決定 3)と freestanding backend(scope 外、決定 4)が
それぞれ受ける。

### 2. testing_allocator は取り下げ、handle 実体化は延期する

`testing_allocator()` の目的は test 終了時の runtime leak 検出だったが、
検出対象が消えた。leak は checker が静的に拒否し、残る 2 つは検出に
値しない: `std::mem::leak()` は source に grep できる意図した挙動であり、
std / runtime 自体のバグは Kizu ユーザーの test に負わせるものではない。
静的に保証済みの性質を runtime で二重に検査する経路は作らない
(docs/principles.md 5「型で閉じられる検査は compile 時に」、
9「経路は 1 本」)。

handle 実体化の主動機は testing_allocator の観測点だった。allocator の
種類が page 1 つしかない今、handle 化は allocation ごとの分岐を増やすだけで
何も観測しない。NULL token は「明示 allocator」の型検査上の契約を
損なわない — 契約は source 上の可視性であり、runtime 表現ではない。
第二の allocator 種を実際に入れる変更が、その時に handle を導入する
(閉 → 開は additive)。

### 3. `fixed_buffer_allocator(buf)` は延期する

copy 型の `Allocator` が user の buffer を borrow する形になり、borrow field
禁止と衝突する。mutable slice / buffer provenance の仕様(SPEC が既に別途
延期中)が決まってから設計する。

### 4. freestanding backend 差し替えは #549 の scope 外

kernel / `--libc off` 環境で必要なのは per-container の差し替えではなく、
`page_allocator()` の実体を build 単位で供給すること(Rust の
`#[global_allocator]` 相当)である。これは borrow field 問題を持たず、
freestanding build(SPEC §17)の設計に属する別機構として扱う。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `contract Allocator`(Zig 式全面開放) | borrow field 禁止と衝突し、user state の寿命を誰も追跡しないまま dangling allocator を許すことになる |
| handle 実体化 + `testing_allocator()`(本 ADR 初稿) | 前提「leak は runtime でしか検出できない」が ADR-0091 で消えた。静的保証の runtime 二重検査であり、page 1 種のままの handle は観測点として機能しない |
| hidden default / global allocator | 明示 allocator の公理(SPEC §15.3)と矛盾 |

## 影響

- 実装変更なし。runtime の NULL token と直接 malloc 経路を現状のまま維持する
- SPEC.md / docs/stdlib.md: 「#549 で別途仕様化します」の行を削除。閉じた状態は
  既に定義済み(opaque、user 実装不可)で、延期の経緯は SPEC でなく本 ADR が持つ

## 再評価条件

- field borrow または別の user state lifetime model が入った時、決定 1(contract 化)を再検討
- 第二の allocator 種(fixed_buffer、freestanding 供給)を入れる変更が、
  handle 実体化(決定 2 の延期分)を伴って行う
- mutable slice / buffer provenance の仕様化後、決定 3(fixed_buffer)を設計
- freestanding build の設計時に、決定 4 の backend 供給機構を仕様化
