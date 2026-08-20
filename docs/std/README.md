# std リファレンス

各 module が公開する API と、その振る舞いです。Kizu は将来的に厚めの標準
ライブラリを持ちます。

`SPEC.md` は言語の定義で、std のうち **compiler が知っている契約** —— capability
としての `Allocator`、storage 型に対する borrow / ownership の検査規則、`test`
宣言 —— だけを持ちます。ここに書くのはその上の API です。

境界は 1 問で決まります: **利用者が自分で同じものを書けるか**。書けるなら
ここ、書けないなら SPEC です。`std::json` は `String` と `std::meta` の上の
普通の Kizu code なのでここに来ますが、`Array.at` が capture 条件でしか
消費できないのは checker の規則なので SPEC に残ります。

| module | 内容 |
| --- | --- |
| [io](io.md) | explicit stdout / stderr / stdin |
| [mem](mem.md) | allocator capability、byte helper、`Box<T>`、`leak`、`Limit` |
| [arena](arena.md) | handle で参照する owned arena |
| [string](string.md) | owned byte buffer |
| [array](array.md) | owned contiguous collection |
| [map](map.md) | owned symbol table |
| [fmt](fmt.md) | diagnostic 用の最小 formatting |
| [json](json.md) | JSON encode |
| [testing](testing.md) | assertion |

実装は `lib/kizu/std/src/` にあります。移行計画と builtin registry は
[docs/stdlib.md](../stdlib.md) です。

## 新しい API を足すとき

`docs/stdlib.md` の Acceptance Rules に従います。この reference の該当
module に節を足し、`examples/` に実例を、安全境界があれば
`examples/negative/` に拒否例を置きます。
