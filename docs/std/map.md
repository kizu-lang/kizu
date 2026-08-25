# std::map

symbol table と scope lookup のための最小 owned map です。

```text
std::map::new<K, V>(allocator: Allocator) -> std::map::Map<K, V>
map.insert(key: K, value: V) -> std::mem::Error!void
map.get(key: K) -> ?V
map.at(key: K) -> ?&V
map.at_mut(key: K) -> ?&var V
map.key_at(index: i64) -> ?K
map.contains(key: K) -> bool
map.len() -> i64
map.deinit() -> void
```

key type `K` に書けるのは `[]u8` と整数型
(`i8` / `i16` / `i32` / `i64` / `u8` / `u16` / `u32` / `u64` / `isize` / `usize`)です。
key は占めている byte 列として hash・比較されるので、byte 表現が値と 1 対 1 に
対応する型だけが key になれます。`f32` / `f64` はそうではありません ——
`0.0` と `-0.0` は byte が違うのに等しく、NaN は自分の byte と等しくないためです。
struct と enum は padding と表現の取り決めが要るので、今は持ちません。
`insert` は key bytes を owned map 内に copy するため、source key を move しません。
`get` は missing key を `null` として返します(docs/style.md)。
in-place 更新は 1 回の lookup で書けます(ADR-0104)。

```kizu
if m.at_mut(key) |v| {
    v.* = next;
} else {
    try m.insert(key, next);
}
```

`insert` / `get` / `at` / `at_mut` / `contains` は amortized O(1) です。
**map は挿入順で反復します。** 未定義の順序は露出しません。
`key_at` は挿入位置 index の key を返し、末尾を越えたら `null` を返すので、
`while m.key_at(i) |key|` が挿入順の iteration です。`[]u8` key は map storage への
view なので capture 限定で、capture が生きている間 map は共有借用されます
(§7)。整数 key は値として返るため、その制限は付きません。
value type は owner でもかまいません(`Map<[]u8, std::string::String>`、
`Map<[]u8, Map<[]u8, i64>>`)。規則は `Array<T>` が owner 要素に対して持つもの
と同じで、map 専用の規則はありません(ADR-0123)。

| 操作 | copy value | owner value |
| --- | --- | --- |
| `get` | 値を copy して返す | **compile error**。copy すると持ち主が 2 つになる |
| `at` / `at_mut` | 借用 | 同じ |
| `insert`(新しい key) | 値を書く | 値を map へ move する |
| `insert`(既存の key) | 上書き | **trap**。上書きは既にある値を落とすため |
| `deinit` | table と key を解放 | 値を 1 つずつ解放してから table と key |

既存の key への `insert` が trap なのは、map がその値の唯一の持ち主だからです。
上書きすると誰も知らないまま消えます。置き換えは `at_mut` で in-place に行うか、
`contains` で先に分岐します。占有しているかは実行時にしか分からないので、
`Array.set` が同じ状況を compile error にするのに対してこちらは trap です。

deletion、custom hash/equality、owned key、struct / enum key は後続で扱います。
`std::map::new<K, V>()` のような hidden default allocator は使いません。

value borrow(`at` / `at_mut`)の消費規則、borrow 中に待つ操作、`key_at` が
返す view の capture 限定、`deinit` 後の使用禁止は checker が持つ規則なので
SPEC §14.4 にあります。
