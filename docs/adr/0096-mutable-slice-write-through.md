# ADR-0096: 可変性は borrow が運ぶ — `&var []u8` の write-through

Status: 採用

Issue: なし(#538 の後続。ADR-0092 決定 3 の前提を解除する)

## 背景

safe Kizu には mutable な byte view が存在せず、`fn read_into(buf: &var []u8)`
の中の `buf[0] = 65` は `invalid assignment target` だった。SPEC:1899 は
「safe Kizu に mutable backing slice は公開しません」と明文で閉じており、
`std::bytes::Buffer` / `OwnedBytes`(SPEC:1901)、`fixed_buffer_allocator`
(ADR-0092 決定 3)、http/net の socket read(#1079〜)がすべてこの未仕様で
塞がっていた。std は `read_stdin_into(out: &var String)` の owned method 経由で
回避している。

ユーザー決定 3 点: (1) mutable view は `&var` 合成で表す、(2) owned container
の mutable backing 公開を実需ありと認定する、(3) stack buffer(固定長配列型)
を導入する。本 ADR は (1)(2) を仕様化する。(3) は後続 PR で `[N]T` として
入れる(本 ADR の決定 2 が供給源の枠を先に定める)。

## 決定

### 1. 可変性は型でなく borrow が運ぶ

view 型は `[]u8` の 1 つだけを保つ。書き込み権は `&var []u8` — mutable
borrow として view を持つこと — が運ぶ。`&var []u8` の保持者は要素書き込み
`buf[i] = x` ができる(bounds は既存の index 規則どおり trap)。

`[]var u8` のような可変 view 型は作らない。可変 view を値として field に
保存できるようにすると誰も追跡しない aliasing が構造体に入り(Zig の形)、
保存を禁じるなら borrow 規則の二重実装になる(原理 6・7)。`&var` 合成なら
排他・escape 禁止・field 保存禁止・boundary の `borrows` 明示が既存規則の
まま適用される。帰結として **可変 view は保存できない**。保持する形
(fixed_buffer 等)は owned buffer を内包する設計に寄せる。

`&var []u8` が許すのは**要素書き込みだけ**である。`buf.* = other` による
view の差し替えは許さない: 書き込みは view 自身の fat pointer を通って
bytes に届くのであって、caller の binding には届かない。この帰結として
IR 表現は flat な `[]u8` のままでよく(共有 borrow と同じ)、可変性は
checker 層だけが運ぶ。`&var i64` の `.* =` が本体そのものの書き込みで
あるのとは対照的に、view の差し替えは別の値の書き込みであり、混ぜない
(原理 7)。

### 2. 供給源は書き込み可能な place に限る

`&var []u8` を作れるのは次だけ。plain `[]u8`(literal 由来を含む)からは
作れない。

- `String.as_mut_bytes()`: mutable binding の String から。view が生きて
  いる間、String 全体が exclusive borrow(mutating method・deinit・共有 view
  はすべて拒否)
- `&var []u8` 引数の再貸し(既存の borrow 伝播)
- (後続)`[N]T` の var local からの slicing

`&var []u8` parameter へ渡せる引数も writable view binding だけである。
`var bytes = "AB"` のような plain slice local は、mutable binding であっても
backing(literal 等)の書き込み可能性を保証しないため渡せない。

逆方向の貸与は開く: view binding は、**戻り値が view を運ばない関数**
(scalar / void 返し)の plain `[]u8` 引数に読み取り用として貸せる
(`mem::len(buf)` が書き込み関数の中で使えるのはこれ)。従来は全面拒否
だったが、拒否が守っていたのは「callee が view を返して provenance を
洗浄する」経路だけであり、scalar / void 返しにその経路はない。view を
返し得る関数への受け渡しは従来どおり escape として拒否する。

### 3. index 構文の対象は builtin layout に限定する

`buf[i] = x` の書き込み対象は slice(と後続の `[N]T`)。`Array<T>` への
直接 indexing は SPEC:965 の延期を維持する。Array は std の kizu ソースで
定義された struct であり、言語組み込みの indexing を与えると IR が std の
内部 layout に結合するか、source に見えない `at()` 呼び出しへの desugar
(原理 2 違反)になる。`&items[i]` の indexed borrow も SPEC:1141 の延期を
維持する。Array の書き込みは従来どおり `at_mut` + `.*` / `set`。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `[]var u8` 型の新設 | field 保存を許せば安全性の穴、禁じれば borrow 規則の二重実装。同じ意味に 2 つの形(原理 7) |
| method 経由のみ(`buf.set(i, x)`) | slice への直接書き込みを永久に持たない言語になり、systems language の基本操作が API 越しになる |
| `nums[i]` を `at()` へ desugar | 呼び出しが source に見えない(原理 2)。trap と recoverable の意味差(§7.1)も潰れる |
| 推論で全 `&var` slice 引数を書き込み可能扱い | 供給源の追跡が契約から消える。明示性の公理と矛盾 |

## 影響

- SPEC §7.1: mutable indexed assignment を「writable slice place に対して定義」
  へ移動。§9: `&var []u8` の write-through 規則。§16 string: `as_mut_bytes`
  追加、「mutable backing slice は公開しません」を削除
- `internal/types`: assignment target に IndexExpr を追加(base が mutable
  slice place のときだけ)。`as_mut_bytes` の method 検査
- `internal/ownership`: `as_mut_bytes` は exclusive borrow を activate。
  write-through は borrow 中の store であり move を伴わない(要素は非 owner)
- `internal/ir` / `internal/llvm`: checked index store の lowering(読みと同じ
  bounds cond_fail + store)。`array_as_mut_bytes` builtin は既存
  `array_as_bytes` と同じ runtime 関数・同じ view 値で、宣言型だけが異なる
- ADR-0092 決定 3(fixed_buffer)の前提「mutable slice provenance 未仕様」が
  解消される。設計は `[N]T` 導入後に行う

## 再評価条件

- 可変 view の保存(view struct)の実需が出た場合。それは穴 3(borrow field)
  の再評価であり、本 ADR の決定 1 は「保存は owned で」の方針ごと見直しになる
- `[]T`(u8 以外)の mutable slice が必要になった時、決定 2 の供給源を
  generic に広げる(additive)
