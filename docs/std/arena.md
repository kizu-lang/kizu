# std::arena

複数の owned value をまとめて保持し、copy handle で参照する arena です。

```text
std::arena::new<T>(allocator: Allocator) -> std::arena::Arena<T>
arena.add(value: T) -> std::arena::Handle<T>
arena.at(handle: std::arena::Handle<T>) -> &T
arena.at_mut(handle: std::arena::Handle<T>) -> ?&var T
arena.deinit() -> void
```

`new` は明示的な allocator capability を読み取り、`add` は value を arena へ
move します。返る `Handle<T>` は raw pointer ではなく copy できる opaque ID です。
要素 storage が移動しても handle は ID のままなので、要素間の関係を handle field
として持てます。削除 API はありません。

`at` は arena に tied な shared borrow `&T` を返します。式から直接 field / method /
match を読めるほか、local binding に束縛できます。binding が生きている間は同じ
arena の `add` / `deinit` を実行できず、borrow から owner を move できません。
borrow parameter 由来の arena なら `&T` を返せますが、local arena 由来の borrow は
function から escape できません。`at_mut` は capture 条件でだけ開ける `?&var T` です。
handle provenance の規則は SPEC §10 にあります。

要素型 `T` は owner でもかまいません。`deinit` は initialized element をそれぞれ
consume してから arena storage を解放します。要素が owner でなければ要素 cleanup
の loop は生成されず、storage の解放だけになります。
