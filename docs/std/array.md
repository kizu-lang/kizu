# std::array

明示 allocator capability を受け取る owned contiguous collection です。

```text
std::mem::page_allocator() -> Allocator
std::array::new<T>(allocator: Allocator) -> std::array::Array<T>
array.append(value: T) -> std::mem::Error!void
array.len() -> i64
array.capacity() -> i64
array.reserve(additional: i64) -> std::mem::Error!void
array.clone(allocator: Allocator) -> !Array<T>
array.pop() -> ?T
array.pop_or_panic() -> T
array.get(index: i64) -> ?T
array.get_or_panic(index: i64) -> T
array.at(index: i64) -> ?&T
array.at_mut(index: i64) -> ?&var T
array.set(index: i64, value: T) -> std::array::Error!void
array.swap(left: i64, right: i64) -> std::array::Error!void
array.deinit() -> void
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

`deinit` は全要素を consume してから buffer を解放します。要素が何も持たない
場合は consume が空になるだけで、生成されるのは buffer の解放 1 命令です。
cleanup の名前はこれ 1 つなので、要素型が決まっていない generic code も同じ
ものを書け、`Array<Array<String>>` のような入れ子もそのまま解放できます。

element borrow(`at` / `at_mut`)の消費規則、borrow が生きている間の禁止事項、
`deinit` の element cleanup 義務、element 型に置ける型の制限は checker が持つ
規則なので SPEC §14.4 にあります。
