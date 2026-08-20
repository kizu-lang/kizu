# std::arena

複数の owned value をまとめて保持し、copy handle で参照する arena です。

```text
std::arena::new<T>(allocator: Allocator) -> std::arena::Arena<T>
arena.add(value: T) -> std::arena::Handle<T>
arena.at(handle: std::arena::Handle<T>) -> local borrow-like T
arena.at_mut(handle: std::arena::Handle<T>) -> ?&var T
arena.deinit() -> void
```

`new` は明示的な allocator capability を読み取り、`add` は value を arena へ
move します。返る `Handle<T>` は raw pointer ではなく copy できる opaque ID です。
要素 storage が移動しても handle は ID のままなので、要素間の関係を handle field
として持てます。削除 API はありません。

`at` は arena に tied な local borrow-like value を返し、そこから owner を move
できません。`at_mut` は capture 条件でだけ開ける `?&var T` です。borrow 中の
`add` / `at` / `deinit` と handle provenance の規則は SPEC §10 にあります。

要素型 `T` は owner でもかまいません。`deinit` は initialized element をそれぞれ
consume してから arena storage を解放します。要素が owner でなければ要素 cleanup
の loop は生成されず、storage の解放だけになります。
