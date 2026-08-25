# std::mem

allocator capability、read-only な byte helper、`Box<T>` を持ちます。

`Allocator` capability そのもの(`page_allocator` と `fixed_buffer`、tied
allocator の規則)は言語の一部なので SPEC §14.3 と §14.4 にあります。

`std::mem` は allocation-free な read-only byte helper から始めます。

```text
std::mem::page_allocator() -> Allocator
std::mem::allocator_header() -> std::mem::AllocatorHeader
std::mem::allocator_from<T>(
    state: &var T,
    alloc: unsafe fn(&var T, i64) -> ?ptr<u8>,
    free: unsafe fn(&var T, ptr<u8>, i64) -> void,
) -> Allocator
std::mem::limit_bytes(limit: std::mem::Limit) -> i64
std::mem::box<T>(allocator: Allocator, value: T) -> std::mem::Error!std::mem::Box<T>
std::mem::leak<T>(value: T) -> void
box.borrow() -> &T
box.borrow_mut() -> &var T
box.take() -> T
box.deinit() -> void
std::mem::len(bytes: []u8) -> i64
std::mem::byte_at(bytes: []u8, index: i64) -> ?u8
std::mem::equal_bytes(left: []u8, right: []u8) -> bool
std::mem::starts_with(bytes: []u8, prefix: []u8) -> bool
std::mem::slice(bytes: []u8, start: i64, end: i64) -> ?[]u8
std::mem::trim_ascii(bytes: []u8) -> []u8
std::mem::bytes_iter(bytes: []u8) -> std::mem::BytesIter
bytes_iter.next() -> ?u8
```

`std::mem::bytes_iter` は iterator protocol(§6.10)の std 綴りです。
`next() -> ?u8` が `while it.next() |byte|` を終端まで駆動し、終端は
失敗ではなく `null` です。cursor は view を capture する struct なので、
歩いている bytes より長生きできません(ADR-0100)。

`std::mem::page_allocator()` は安定 allocator capability factory です。
返された `Allocator` は copy 型であり、複数の owned container や arena の構築に
再利用できます。allocator を受け取る constructor は capability を読み取るだけで、
allocator binding を move しません。

`std::mem::allocator_from<T>` は利用者が書いた実装から `Allocator` を作ります
(ADR-0129)。allocator は言語がどこでも要求する capability なので、利用者が
自分で書けるものです。`alloc` は要求 byte 数か `null` を返し、`free` は `alloc`
が返したものを、要求されたときの size と一緒に受け取ります。どちらも
`unsafe fn` 宣言が名指す義務を負います。返る `Allocator` は渡した state に
tie され、state より長生きできません。`allocator_header` はその実装の第 1
field に置く zeroed header を返します。実例は `examples/user_allocator.kizu`
です。

`std::mem::Limit` は allocation の上限を綴ります。上限を付けないことも選択なので
`Unlimited` と書き、source に残ります。`limit_bytes` は runtime primitive 用に
`Bytes(n)` を `n`、`Unlimited` を `-1` として描きます。

`std::mem::Box<T>` は明示 allocator capability で 1 つの owned value を確保する
non-copy / move-only な indirection です。
`box.take()` は Box とその allocation を consume し、payload の所有権を caller に
戻します。receiver は local binding に限り、borrow 中は呼べません。payload を
使い続けるときは `take`、Box と payload をまとめて解放するときは `deinit` を使います。
`std::mem` の safe API は raw pointer を返しません。
`std::mem::slice` と `std::mem::byte_at` は境界外を `null` として返します
(lookup の不在は失敗ではなく答えであるため。基準は `docs/style.md`)。
checked index / slice syntax の実装後は、Kizu std source では
trap-on-bounds-failure の syntax と recoverable な `std::mem` API を用途で使い分けます。
allocator、mutable slice、byte copy / zero / fill は、`std::array::Array<T>` と
mutable slice の仕様後に実装します。

`Box<T>` の borrow 規則(束縛位置、provenance、borrow 中の move / take / deinit 禁止)、
`leak` と `Limit` が持つ「危険を明示語にする」役割は SPEC §14.3 と §14.4 にあります。
