# std::arena

複数の owned value をまとめて保持し、copy handle で参照する arena です。

```text
std::arena::new<T, M>(allocator: Allocator) -> std::arena::Arena<T, M>
arena.add(allocator: Allocator, value: T) -> std::mem::Error!std::arena::Handle<T, M>
arena.at(handle: std::arena::Handle<T, M>) -> &T
arena.at_mut(handle: std::arena::Handle<T, M>) -> ?&var T
arena.deinit(allocator: Allocator) -> void
```

`M` は **marker** で、この arena がどれかを型で名乗るためだけの、field を
持たない struct です。実行時には何も持たず、handle は i64 のまま、header も
24 byte のままです。marker が違えば型が違うので、同じ要素型の arena が 2 本
あっても、片方の handle をもう片方で読むのは型 error になります(ADR-0134)。

```kizu
struct Left {}
struct Right {}

var left = arena::new<User, Left>(allocator);
var right = arena::new<User, Right>(allocator);
let alice = try left.add(allocator, User { name: "alice" });
right.at(alice);
// error: `Arena.at` expects std::arena::Handle<User, Right>,
//        got std::arena::Handle<User, Left>
```

1 つの marker を名乗れる `arena::new` は program 全体で 1 か所です。2 つの
arena が同じ marker を名乗ると handle が交換可能に戻るため、2 回目は型 error
になります。

`new` は明示的な allocator capability を読み取りますが、header そのものを作る
だけで何も確保しません。だから失敗しようがなく、`!T` を返しません。storage を
買うのは最初の `add` で、失敗を言うのもそこです(ADR-0131)。`add` は value を
arena へ move し、storage を買う call なので allocator を receiver の次に取り
ます(ADR-0132)。allocator が断ったことは `std::mem::Error!` で返るので、呼び出し
側が扱いを決めます —— `mem::fixed_buffer` を使い切るのは正常系の分岐です。
返る `Handle<T, M>` は raw pointer ではなく copy できる opaque ID です。
要素 storage が移動しても handle は ID のままなので、要素間の関係を handle field
として持てます。削除 API はありません。

`at` は arena に tied な shared borrow `&T` を返します。式から直接 field / method /
match を読めるほか、local binding に束縛できます。binding が生きている間は同じ
arena の `add` / `deinit` を実行できず、borrow から owner を move できません。
borrow parameter 由来の arena なら `&T` を返せますが、local arena 由来の borrow は
function から escape できません。`at_mut` は capture 条件でだけ開ける `?&var T` です。
handle と marker の規則は SPEC §10 にあります。

任意の arena に対して動く helper は、marker を型パラメータにして書きます。

```kizu
fn read<M>(items: &arena::Arena<Item, M>, h: arena::Handle<Item, M>) -> i64 {
    return items.at(h).value;
}
```

struct は marker を具体的に名指します(SPEC §7: full generics を実装しない)。
handle を持つ struct は普通、特定の arena の handle を持つのでこれで足ります。

要素型 `T` は owner でもかまいません。`deinit` は initialized element をそれぞれ
consume してから arena storage を解放します。要素が owner でなければ要素 cleanup
の loop は生成されず、storage の解放だけになります。
