# std リファレンス

各 module が公開する API と、その振る舞いです。Kizu は将来的に厚めの標準
ライブラリを持ちます。

`SPEC.md` は言語の定義で、std のうち **compiler が知っている契約** —— capability
としての `Allocator`、storage 型に対する borrow / ownership の検査規則、`test`
宣言、`std::meta` / `std::target` の compiler-defined form —— だけを持ちます。
ここに書くのはその上の API です。

境界は 1 問で決まります: **利用者が自分で同じものを書けるか**。書けるなら
ここ、書けないなら SPEC です。`std::json` は `String` と `std::meta` の上の
普通の Kizu code なのでここに来ますが、`Array.at` が capture 条件でしか
消費できないのは checker の規則なので SPEC に残ります。

| module | 内容 |
| --- | --- |
| [io](io.md) | explicit stdout / stderr / stdin、evented な `Io` と `async` |
| [coro](coro.md) | 途中で止まれる呼び出し。並行性ではない |
| [mem](mem.md) | allocator capability、byte helper、`Box<T>`、`leak`、`Limit` |
| [arena](arena.md) | handle で参照する owned arena |
| [string](string.md) | owned byte buffer |
| [array](array.md) | owned contiguous collection |
| [sort](sort.md) | owner-safe in-place string sorting |
| [map](map.md) | owned symbol table |
| [fmt](fmt.md) | diagnostic 用の最小 formatting |
| [json](json.md) | JSON の encode と decode |
| [fs](fs.md) | `Io` 経由の file system 操作 |
| [net](net.md) | `Io` 経由の TCP listener と stream |
| [http](http.md) | HTTP/1 の server、message、routing |
| [path](path.md) | file system を見ない path text 操作 |
| [process](process.md) | 引数・環境変数・時刻・終了 status |
| [testing](testing.md) | assertion |

実装は `lib/kizu/std/src/` にあります。trusted primitive の境界は
[docs/stdlib.md](../stdlib.md) です。

## 新しい API を足すとき

`docs/stdlib.md` の「新しい std API を足すとき」に従います。この reference の
該当 module に節を足し、`examples/` に実例を、安全境界があれば
`examples/negative/` に拒否例を置きます。
