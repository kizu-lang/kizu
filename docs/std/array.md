# std::array

明示 allocator capability を受け取る owned contiguous collection です。

```text
std::mem::page_allocator() -> Allocator
std::array::new<T>(allocator: Allocator) -> std::array::Array<T>
array.append(value: T) -> !void
array.len() -> i64
array.capacity() -> i64
array.reserve(additional: i64) -> !void
array.pop() -> ?T
array.pop_or_panic() -> T
array.get(index: i64) -> ?T
array.get_or_panic(index: i64) -> T
array.at(index: i64) -> ?&T
array.at_mut(index: i64) -> ?&var T
array.set(index: i64, value: T) -> !void
array.deinit() -> void
```

`std::array::new<T>()` のような hidden default allocator は使いません。
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

element borrow(`at` / `at_mut`)の消費規則、borrow が生きている間の禁止事項、
`deinit` の element cleanup 義務、element 型に置ける型の制限は checker が持つ
規則なので SPEC §14.4 にあります。
