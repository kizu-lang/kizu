# std::array

明示 allocator capability を受け取る owned contiguous collection です。

```text
std::mem::page_allocator() -> Allocator
std::array::new<T>(allocator: Allocator) -> std::array::Array<T>
array.append(allocator: Allocator, value: T) -> std::mem::Error!void
array.append_bytes(allocator: Allocator, bytes: []u8) -> std::mem::Error!void   // Array<u8> のみ
array.len() -> i64
array.capacity() -> i64
array.reserve(allocator: Allocator, additional: i64) -> std::mem::Error!void
array.clone(allocator: Allocator) -> !Array<T>
array.pop() -> ?T
array.pop_or_panic() -> T
array.get(index: i64) -> ?T
array.get_or_panic(index: i64) -> T
array.at(index: i64) -> ?&T
array.at_mut(index: i64) -> ?&var T
array.set(index: i64, value: T) -> std::array::Error!void
array.swap(left: i64, right: i64) -> std::array::Error!void
array.remove(index: i64) -> std::array::Error!T
array.truncate(allocator: Allocator, length: i64) -> std::array::Error!void
array.clear(allocator: Allocator) -> void
array.deinit(allocator: Allocator) -> void
```

`std::array::new<T>()` のような hidden default allocator は使いません。
`clone` は copy 要素に限って、指定した allocator 上へ同じ順序の独立した
buffer を作ります。owner 要素は要素ごとの deep-copy が必要なので
`Array.clone` では扱いません(ADR-0124)。`[]u8` のような view は copy ですが、
view が指す backing bytes までは複製しません。
`array.get` は bounds check し、範囲外なら `null` を返します。
`array.get_or_panic` は testing や invariant-checked code 用の明示 trap variant です。
範囲外なら runtime error で停止するため、recoverable lookup には `get` を使います。
`while array.at(i) |elem|` は §6.10 の optional 条件 capture そのままなので、
non-copy element の iteration も `get` と同じ形になります。
`pop` は最後の initialized element を array から move して `?T` を返し、
empty array なら `null` を返します。
`pop_or_panic` も最後の initialized element を move して `T` を返し、
empty array なら runtime error で停止します。copy / non-copy のどちらにも使え、
recoverable な empty case を扱う場合は `pop` を使います。

`swap` は両方の index を検査してから initialized slot を交換し、範囲外なら
`std::array::Error::OutOfBounds` を返します。同じ index 同士の交換は成功します。
要素を copy・replace・cleanup せず storage 上の位置だけを入れ替えるため、
`String` のような owner element にも使えます。receiver は owned local または
`&var Array<T>` でなければならず、shared borrow 越しの呼び出しは拒否されます。

`remove` は index を検査してから、その要素を caller へ move し、後続要素を順序を
保ったまま 1 slot 前へ詰めます。要素を cleanup しないため owner element にも使え、
返された owner の cleanup 義務は caller が引き継ぎます。範囲外なら
`std::array::Error::OutOfBounds` を返し、length・順序・各要素を変更しません。

`truncate` は `0 <= length <= array.len()` を先に検査し、失敗時は collection を
変更しません。成功時は末尾から `length` までの各 owner element を cleanup して
length を縮めます。`clear` は同じ処理を length 0 まで行います。どちらも backing
buffer と capacity は保持するため、そのまま再利用できます。

要素の cleanup が allocator を名指す型なら `deinit(allocator)`、descriptor など
名指さない型なら `deinit()` を呼びます。non-owner element には cleanup がなく、
monomorphize 後は raw storage primitive 1 回になります。element type によって API の
形を変えないため、`truncate` / `clear` は常に allocator を受け取ります。

確保も解放も allocator を名指します。`Array<T>` の header は `{data, len, cap}`
の 3 word で、allocator を覚えません —— 確保に必要なものも解放に必要なものも
呼び出し側が既に持っており、値がその複製を運ぶ必要はないからです(SPEC §14.3)。
`new` が受け取る allocator は compile 時の provenance で、`append` / `reserve` /
`truncate` / `clear` / `deinit` が同じものを名指すことを checker が要求します。
全要素を consume してから buffer を解放し、要素が何も持たない場合は consume が
空になるだけで、生成されるのは buffer の解放 1 命令です。
cleanup の名前はこれ 1 つなので、要素型が決まっていない generic code も同じ
ものを書け、`Array<Array<String>>` のような入れ子もそのまま解放できます。

element borrow(`at` / `at_mut`)の消費規則、borrow が生きている間の禁止事項、
`deinit` の element cleanup 義務、element 型に置ける型の制限は checker が持つ
規則なので SPEC §14.4 にあります。
