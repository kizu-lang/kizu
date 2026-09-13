# ADR-0097: stack buffer `[N]T` — local 限定の固定長 buffer

Status: 採用

Issue: なし(ADR-0096 の後続。ユーザー決定「stack buffer を今回入れる」の仕様化)

## 背景

heap を使わない byte buffer を safe Kizu で作る手段がなかった。ADR-0096 で
mutable view(`&var []u8`)と `String.as_mut_bytes` が入ったが、view の供給源が
heap 上の owned buffer だけでは、割り当てなしの read/write buffer が書けない。
`fixed_buffer_allocator`(ADR-0092 決定 3)も内包する buffer の形を持たない。

## 決定

### 1. `var buf = [64]u8{};` — 型名 + 空 brace の zero 埋め literal

型は `[N]u8`(N は正の整数 literal)。生成は struct literal と同じ形の延長で、
空 brace が zero 埋めを明示する。初期化子のない宣言や undefined 初期化は
持たない(安全公理)。

### 2. view の入口は `as_bytes()` / `as_mut_bytes()`

String と完全に同じ規則: 束縛必須、`as_mut_bytes` は mutable binding 限定で
exclusive borrow、view が生きている間の binding 使用は borrow 規則が守る。
view の作り方が言語内で 1 つに揃う(原理 9)。buffer への書き込みは
`as_mut_bytes` の view 経由だけで、buffer への直接 indexing は持たない
(view で書けるものへの第二経路を作らない)。

### 3. local 限定・element は固定幅の数値型

- element は `u8 u16 u32 u64 i8 i16 i32 i64 f32 f64`。view は `[]T` で、
  index / slice は element 単位。`[N]u8` の view は `as_bytes` /
  `as_mut_bytes`、他の T は `as_slice` / `as_mut_slice`(名前が返るものを言う)。
  `[]u8` と `[]T` の間の reinterpret は持たない: byte 順が target 依存になり、
  「どの target でも同じ bytes」が崩れる。byte との変換は明示の読み書き
  (#1779: std::crypto の word 状態を byte から組み立てる経路が 3〜5 倍の差だった)
- **struct field・union payload・関数 parameter・返り値・container element に
  置けない**。関数境界は view(`[]u8` / `&var []u8`)で渡す
- `&buf` / `&var buf` の直接 borrow は不可(view method が唯一の入口)
- non-copy。binding 間 move は通常の move 規則
- deinit 不要(owner ではない。frame と共に消える)

制限はすべて additive に緩められる(原理 8)。逆に最初から開くと
「stack の値が frame より長生きする経路」を全部検査してからでないと
出せない。

### 4. 表現は「buffer = その storage」

IR/backend では buffer 値は alloca の pointer そのもので、view は
`{ptr, N}` を組むだけ。ADR-0096 の「可変性は checker 層だけが運ぶ」と
同じ帰結で、runtime 表現に可変性の区別はない。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `var buf: [64]u8;`(初期化子なし宣言) | 「初期化子のない宣言」という新規形を言語に入れ、他の型への波及を止める特例が要る |
| `mem::stack_bytes<64>()` factory | 戻り型が値依存(`[n]u8`)になり型検査の新機構が要る。stack 確保が関数呼びに見えるのも実態と乖離 |
| `&var buf[0..n]` slicing 構文 | `&var` + index の新しい合成を文法に足す。as_mut_bytes で同じものが既存規則のまま得られる |
| 直接 indexing `buf[0] = x` | view 経由で書けるものへの第二経路(原理 9)。必要なら additive に追加できる |
| `[]u8` を `[]u32` に reinterpret する view | byte 順が target 依存になり、同じ source が target ごとに違う bytes を出す |
| element を struct や owner にも開く | index は等幅の cell を前提にして成り立つ。owner を element にすると cleanup の所在が view に乗る |

## 影響

- SPEC §7.1: `[N]u8` の型と literal、view 入口、v1 制限
- `internal/typ`: `Buffer` node と `[N]T` の parse
- `internal/parser`: 型位置の `[N]T`、式位置の `[N]u8{}` literal
- `internal/types` / `internal/ownership`: literal の型付け、view initializer の
  受け入れ(String と共通経路)、制限の拒否(param / return / field /
  element / borrow)
- `internal/ir` / `internal/llvm`: `buffer.new`(alloca + zero store)と
  `buffer.as_bytes`(`{ptr, N}` 構築)。runtime 関数は増えない

## 再評価条件

- fixed_buffer_allocator(ADR-0092 決定 3)の設計時に、struct field への
  格納制限(決定 3)を再評価する
- element を数値以外に開くかは実需が出た時に検討する
