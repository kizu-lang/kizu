# ADR-0144: 接続は返ってくる。返さなければ次は来ない

## Status

Accepted.

## Context

`accept_ready` + `advance` + `Poller` で 1 thread が多数の接続を捌けるように
なりました(ADR-0141)。ただし loop を書く人が毎回、poller、token 表、期限の
sweep を手で書きます。`examples/http_evented.kizu` がその全部です。

**書き忘れると穴が開きます。** sweep を書かなければ、繋いで黙る相手を server は
永遠に抱えます(ADR-0143 の後に足した `expired` は、書けば塞げるところまで)。

Go は `ListenAndServe(addr, handler)` で loop を持ちます。**Kizu でも書けます** ——
関数 pointer 型(`fn(...) -> ...`、SPEC §5)があり、top-level function の名前は
値になります。書けないのは handler の**形**の方です。

## Decision

`Server` が接続を持ち、**1 つ渡して 1 つ受け取ります**。

```kizu
var current = try server.first(handle, allocator, 1048576);
current = try turn(handle, allocator, &var server, move current);
```

`next(io, allocator, done, max)` は `Exchange` を**消費します**。返さずに次を得る
道が無いので、借りた接続を忘れることが型として書けません。accept も advance も
期限の sweep も `next` の中です。

`first_head` / `next_head` は body を接続に残す対で、`accept_head` と同じ分担です。

**`serve` はありません。** loop は caller のもので、止めるのは `break` です。

## Consequences

`http_evented.kizu` が手で書いていたものが std に入りました。書き忘れられるものが
無くなり、期限の掃除は「書けば塞げる」から「黙って塞がっている」に変わりました。

**loop の 1 周は関数になります。** 手渡しで再代入される変数に `errdefer` を
付けられないので、`Exchange` を所有する frame が答える frame でもある必要が
あります。所有と cleanup が同じ場所に来るので、結果的にこちらの方が正しい形
でした。

`http::Failure` に `std::array::Error` が入りました。接続を `Array` に持つからで、
`std::sort` が同じ理由で同じものを propagate します。到達する経路はありません
(index は array の長さから来る)が、変換すれば原因の発明、握り潰せば隠れた分岐に
なります(ADR-0128)。

`listen` が allocator を取り、`deinit` も取ります。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `serve(io, allocator, server, handler)`(Go の形) | ADR-0136 が同じ判断を既にしている —— 登録簿から選ばれた handler の呼び出しは呼び出し元の source に無く、それが原理 2 の言う hidden control flow。書けるかどうかの話ではない。Zig も関数 pointer を渡せるのに `receiveHead` の pull を選んでいる。そして B の形は「返し忘れ」を型で禁じられる —— handler の形にはこの性質が無い |
| `next_request()` が渡すだけで、`give_back()` が別にある | 返し忘れが新しい footgun になる。忘れられない形にできるなら、忘れられる形を出す理由が無い |
| `next` が `&var Exchange` を返す | struct field への borrow を返す method を書けない(`docs/language-gaps.md`)。ADR-0138 が `Exchange.stream()` を却下したのと同じ壁 |
| `stop()` を server の capability にする | 単一 thread で signal も無い今、呼べる場所は loop の中だけで、そこには既に `break` がある。呼べない capability を名乗らない |
| token を index ではなく安定 id にする | 表からの取り出しが線形探索になる。index なら swap-remove の後に 1 本だけ watch し直せば済む |
| `array::Error` を `http::Error` の member に変換する | 起きない失敗のために原因を発明することになる。ADR-0128 が変換を禁じている |
