# ADR-0123: `Map` の owner value は `Array` の owner element と同じ規則で扱う

Status: 採用(実装済み)

## 背景

`std::map::Map<[]u8, V>` の `V` は copy 型限定だった。

```
$ kizu check           # decode<map::Map<[]u8, string::String>>
error: type error: std::map::Map value type must be copy
```

`map.get(key) -> ?V` が値返しで取り出すので、owner を copy すると持ち主が 2 つに
なる。それを避けるための消極的な縛りで、`Array<T>` には無い。`Array` は owner
element を持てて、copy しない取り出し方(`at` / `at_mut` の借用、`pop` の
move-out)を備えている。

縛りの代償は「値が string の object」と「入れ子の object」で、JSON では最も
普通の形である。`std::json` は map を encode / decode できるのに、この 2 つが
通らなかった。

## 決定

**owner value の扱いは `Array<T>` が既に確立した規則をそのまま写す。**map 専用の
規則を作らない(原理 6、原理 9)。`Map<K, V>` 自身が owned 型であることは変えず、
撤廃するのは `V` に対する copy 制約だけである。

| 操作 | copy value | owner value |
| --- | --- | --- |
| `get` | 値を copy して返す | compile error(`Array.get` と同じ) |
| `at` / `at_mut` | 借用 | 同じ |
| `insert`(新しい key) | 値を書く | 値を move する |
| `insert`(既存の key) | 上書き | trap |
| `deinit` | table と key を解放 | 値を 1 つずつ解放してから table と key |

owned key は別に扱う(ADR-0044)。

## 既存 key への insert

この ADR の初版は「退避される旧値を deinit してから上書きする」と決めていた。
**取り下げる。**source に無い `deinit` 呼び出しで、原理 2 に反する。

初版の後、`Array` が同じ状況に答えを出した。

```
$ kizu check           # array::Array<string::String>, a.set(0, t)
error: type error: `Array.set` would leak the replaced `std::string::String` element
```

落ちる側の持ち主は他にいないので、置き換えは拒否する。map も同じ答えにする。
`Array.set` が compile error なのに `Map.insert` が trap なのは、key が占有済み
かは実行時にしか分からないからで、規則が違うわけではない。

trap であって error でないのは、これが「この API はそれをしない」の表明だから
である(ADR-0112)。後から error に緩めるのは additive、逆は breaking なので、
閉じた側から出す(原理 8)。置き換えは `at_mut` で in-place に行う。

## deinit の cascade

`Array.deinit` と同じく、値を解放する loop は runtime op ではなく std の wrapper
本体にある。`deinit` の呼び出しが source にあり、その中の `value.deinit()` も
source にある(原理 2)。copy value では loop が生成されず、runtime op 1 つに戻る。

loop は key ではなく挿入位置で値を取る: key を読むと map を借用し、直後の
take がそれを返してもらう必要がある。そのための
`std::internal::builtin::map_take_value_at` は std 専用で、この loop だけが呼ぶ。
公開 API に move-out を足していないのは、`remove` を要る場面がまだ無いためで、
要ったときに additive に足せる。

## 影響

`Map<[]u8, String>`、`Map<[]u8, Map<[]u8, V>>`、`Map<[]u8, Array<T>>` が通る。
copy 制約がそれらを masking していただけで、撤廃の副作用として通った。

`std::json` の generic 経路は変更なしで受け取った。`encode_entries` は既に
`get` ではなく `at` を使い、`decode_map` は `contains` で guard していた。

`std::json::Value` の `Obj` は copy 制約を避けるために `Array<Entry>` だった。
制約が消えたので map にした。`Entry` 型が 1 つ減り、重複 key の検査が線形走査
から `contains` になり、key lookup が O(1) になる。key の順序は map が挿入順で
反復するので変わらない(ADR-0088)。`Obj(entries) => entries.len()` のような
walk はそのまま通る。
