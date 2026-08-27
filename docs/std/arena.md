# std::arena

複数の owned value をまとめて保持し、copy handle で参照する arena です。

```text
std::arena::new<T>(allocator: Allocator) -> std::arena::Arena<T>
arena.add(allocator: Allocator, value: T) -> std::mem::Error!std::arena::Handle<T>
arena.at(handle: std::arena::Handle<T>) -> &T
arena.at_mut(handle: std::arena::Handle<T>) -> ?&var T
arena.deinit(allocator: Allocator) -> void
```

`new` は明示的な allocator capability を読み取りますが、header そのものを作る
だけで何も確保しません。だから失敗しようがなく、`!T` を返しません。storage を
買うのは最初の `add` で、失敗を言うのもそこです(ADR-0131)。`add` は value を
arena へ move し、storage を買う call なので allocator を receiver の次に取り
ます(ADR-0132)。allocator が断ったことは `std::mem::Error!` で返るので、呼び出し
側が扱いを決めます —— `mem::fixed_buffer` を使い切るのは正常系の分岐です。
返る `Handle<T>` は raw pointer ではなく copy できる opaque ID です。
要素 storage が移動しても handle は ID のままなので、要素間の関係を handle field
として持てます。削除 API はありません。

**handle は自分を作った arena instance を運びます。** だから別の arena に渡した
handle は読み取りが拒否し、`at` は runtime error で停止、`at_mut` は null を
返します。同じ `arena::new` を 2 回実行して作った 2 本も別 instance なので、
互いの handle は通りません(ADR-0134)。

```kizu
fn new_nodes(allocator: Allocator) -> arena::Arena<Node> {
    return arena::new<Node>(allocator);
}

var left = new_nodes(allocator);
var right = new_nodes(allocator);
let first = try left.add(allocator, Node { value: 1 });
right.at(first);
// runtime error: invalid arena handle
```

これは値の大きさを何も変えません。handle は i64 のまま、`?Handle<T>` の niche
(ADR-0133)もそのままです。header は 24 から 32 byte になり、`at` は引き算 1 つ
ぶんの命令が増えます。1 つの arena が保持できる要素は 2^32 - 1 個までで、
越える `add` は停止します。

`at` は arena に tied な shared borrow `&T` を返します。式から直接 field / method /
match を読めるほか、local binding に束縛できます。binding が生きている間は同じ
arena の `add` / `deinit` を実行できず、borrow から owner を move できません。
borrow parameter 由来の arena なら `&T` を返せますが、local arena 由来の borrow は
function から escape できません。`at_mut` は capture 条件でだけ開ける `?&var T` です。その null は、handle が
この arena のものでないときに開かないための答えでもあります。
handle provenance の規則は SPEC §10 にあります。

要素型 `T` は owner でもかまいません。`deinit` は initialized element をそれぞれ
consume してから arena storage を解放します。要素が owner でなければ要素 cleanup
の loop は生成されず、storage の解放だけになります。
