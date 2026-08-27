# ADR-0132: 解放は allocator を名指す

## Status

Accepted.

## Context

`std::mem::Box<T>` の cell は payload の前に 2 word の header を持っていました。
確保に使った `Allocator` と、cell の大きさです。解放はその 2 つを header から
読みます。

その代償は allocation そのものです。

| | 要求 byte | malloc の block |
| --- | --- | --- |
| pointer 2 本の payload | 16 | 16 |
| header 2 word + その payload | 32 | 32 |

malloc の粒度は 16 byte なので、**header は payload を 1 つ上の size class へ
押し上げます**。pointer 2 本を持つ節点で確保が倍になり、binary-trees(節点
1000 万個)の実測で RSS が 61MB —— 同じプログラムの C 版 23MB、Zig 版 18MB に
対して 2.6 倍です。CPU も 1.65s 対 0.89s / 0.85s でした。

`std::array::Array<T>` も同じものを持ちます。header の 4 word のうち 1 word が
`allocator` で、これは ADR-0131 で header を値にしたときに残した最後の 1 word
です。

header が allocator を覚える理由は 1 つだけです —— **解放が呼ばれる場所で
allocator が分からないから**。

## Decision

**owner の確保と解放は allocator を名指す。**

```kizu
fn (self: std::mem::Box<T>) deinit<T>(allocator: Allocator) -> void
fn (self: std::array::Array<T>) deinit<T>(allocator: Allocator) -> void
fn (self: &var std::array::Array<T>) append<T>(allocator: Allocator, value: T) -> !void
fn (self: &var std::array::Array<T>) reserve<T>(allocator: Allocator, n: i64) -> !void
fn (self: &var std::map::Map<K, V>) insert<K, V>(allocator: Allocator, key: K, value: V) -> !void
fn (self: &var std::arena::Arena<T>) add<T>(allocator: Allocator, value: T) -> Handle<T>
```

`sizeof(T)` を compile 時の値として渡すのと同じ扱いです。確保にも解放にも必要な
ものは呼び出し側が既に持っており、値がその複製を運ぶ必要はありません。解放側は
原理 #4「確保は明示 allocator。hidden allocation を持たない」の裏側で
**hidden deallocation も持たない**ということ、確保側は原理 #4 そのものです ——
`values.append(x)` は allocator を受け取らずに確保していました。

`std::array::new<T>(allocator)` は引数を保ちますが、何も確保しないので runtime
には届きません。compile 時の provenance として残り、後続の確保・解放が同じ
allocator を名指すことを checker が要求します(原理 #5)。

### 導出 deinit も allocator を取る

owner field を持つ型の `deinit` は導出されます(原理 #10)。導出されたものも
allocator を受け取り、field へそのまま渡します。

```kizu
// struct Node { left: ?Box<Node>, right: ?Box<Node> } に対して
fn (self: Node) deinit(allocator: Allocator) -> void {
    if self.left |held| { held.deinit(allocator); }
    if self.right |held| { held.deinit(allocator); }
    return;
}
```

`deinit` を宣言する型も同じ形を書きます。**owner 型の `deinit` は 1 つの形しか
持ちません**: receiver と allocator。型ごとに引数が違うと、`defer x.deinit(...)`
の書き方が型を見ないと決まらなくなります。

### cleanup は引数を運ぶ

`defer` / `errdefer` が受け取る cleanup は receiver 以外の引数を持てます。
引数は **defer が書かれた場所で読まれ**、scope を抜けるときに走るのはその値です。

### tie 規則

解放に渡す `Allocator` は、その owner を作った allocator と同じ tie を持たな
ければなりません(SPEC §14.3)。`fixed_buffer` / `allocator_from` から作った
allocator は tied なので、取り違えは compile error です。tie を持たない
`page_allocator()` 同士は同じ allocator なので検査は要りません。

container の解放は要素の解放に自分の allocator を渡すので、要素と container が
違う allocator から来ていると取り違えになります。これに別の検査は要りません ——
tied allocator から作った owner は `move` できず(SPEC §14.3)、要素になれない
ためです。tie を持たない `page_allocator()` 同士は区別が付かないので、残るのは
検査できないし検査する必要もない場合だけです。

## 却下

| 案 | 却下理由 |
| --- | --- |
| `array::new<T>()` から allocator も落とす(Zig の `ArrayListUnmanaged`) | 値が持つ情報は減らないのに、その container がどの allocator のものかを compile 時に照合する足場が消える。tie 検査が Array に効かなくなり、原理 #5 に反する |
| header に allocator を残す | 解放のたびに読める代わりに、確保のたびに 1 word 払う。Box では size class 1 つ分、つまり allocation の倍化になる |
| 暗黙の global allocator を持ち、`deinit()` は引数なしのまま(Rust の `Box`) | 原理 #4 が禁じている。どこから確保したかが source に出なくなる |
| `Box` だけ allocator を取り、`Array` は header に残す | 解放の綴りが型ごとに変わる。owner field を持つ型の導出 deinit がどちらの形にもなり得て、`defer` の書き方が型を見ないと決まらない |
| allocator を持つ型と持たない型で `deinit` の引数を変える | 同上。field を 1 つ足しただけで signature が変わり、呼び出し側が壊れる |
| `deinit` を残しつつ `deinit_with(allocator)` を足す | 経路が 2 本になる(原理 #9)。どちらが正しいかを利用者が選ぶことになる |
| 解放時に allocator を推論する | 推論の材料が値の中にない。持たせれば header に戻る |

## Consequences

- `Box<T>` の cell は payload だけ。binary-trees(節点 1000 万個)の RSS は
  61MB から 46MB になった。残る差は `?Box<T>` が tag と pointer を別に持つこと
  で、それは別の判断
- `Array<T>` の header は `{data, len, cap}` の 3 word(24 byte)で、Rust の
  `Vec` と同じ。Array を持つ struct はすべて 1 word 縮み、compiler が自分自身を
  check する peak RSS は 564MB から 497MB になった
- 空の `Array<T>` の構築は命令 0 個になった。3 word の zero 値がそれ自体で
  答えなので、`insertvalue` すら要らない
- **owner を持つすべての型の `deinit` が allocator を取る。** 呼び出し側は
  `defer x.deinit(allocator)` と書く
- allocator binding は、それが作った owner より長生きしなければならない。
  `defer` は allocator を宣言したあとに書かれるので、通常の scope 規則で
  満たされる
- 取り違えは tied allocator でのみ compile error になる。untied な
  `page_allocator()` 同士は区別が付かず、区別する必要もない
- `std::mem::leak` は変わらない。解放しないので allocator を要らない
