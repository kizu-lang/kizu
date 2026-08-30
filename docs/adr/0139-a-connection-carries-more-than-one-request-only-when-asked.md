# ADR-0139: 接続が 2 通目を運ぶのは、頼まれたときだけ

## Status

Accepted.

## Context

ADR-0138 で body を caller が書けるようになりましたが、長さの分からない body を
送る手段は `UntilClose` —— close が終わり —— だけでした。close が終わりである以上、
その接続は次を運べません。

同時に、全 response が `Connection: close` を書いていました。1 接続 1 request です。
これは HTTP/1.1 の既定ではありません。

2 つを一緒に決める必要がありました。**keep-alive を入れるか**と、
**長さ不明の body をどう終わらせるか**は同じ問いだからです。persistent な接続では
close が来ないので、`UntilClose` は成立しません。

## Decision

`Framing::Chunked` を足しました。1 write が 1 chunk で、size を hex で前置きし、
`finish_body` が terminator を書きます。長さを知らないまま、接続を閉じずに body を
終わらせる唯一の方法です。

`exchange.next(io, allocator)` が同じ接続の次の request を読み、あったかを返します。
それで終わる loop が接続の寿命で、ADR-0136 の「見える loop」の 1 段外側です。

接続が次を運ぶのは 3 つが揃うときだけです —— この server が運ぶと言っている
(`served + 1 < max_requests`)、framing が終わりを示せる、request が許している。

**`max_requests` の既定は有限の 100 です。** HTTP/1.1 の無制限な既定をそのまま
server policy にはしません。

## Consequences

当初の 1 は逐次 server で 1 connection が service 全体を保持しないための policy
でした。TaskSet でその制約が外れた後、2026-08-30 に Apple M4 Max / macOS 26.3 の
native build で測りました。単純な 2000 request を 5 回流した中央値は keep-alive
44,048 req/s、毎回接続 14,927 req/s で 2.95 倍です。128 本を idle にすると RSS は
1.8 MiB から 35.4 MiB、約 269 KiB/connection になり、既定の 5 秒後に全て閉じました。
128 本の無通信接続を抱えた別 request は中央値 0.062 ms、100 request を pipeline
した worker と競合した別 request は中央値 0.455 ms、最大 0.552 ms でした。

100 は handshake と worker stack を再作成する費用を償却しつつ、ready な 1 worker
が I/O で止まらず進める量をこの測定で約 0.5 ms に留める有限値です。idle retention
は別の `idle_millis = 5000` が閉じます。接続数そのものの上限は別の policy です。

`accept_head` の後の `read_into` は `Content-Length` を超えて読まなくなりました。
超えて読んだ分は次の request だからです。この境界は keep-alive のために要りますが、
keep-alive でなくても正しいので無条件です。

`next` は終わっていない exchange を false ではなく `Error::ExchangeUnfinished` で
拒否します。答えていない / body を閉じていない / request body を読み切っていない
のはどれも caller の bug で、接続が黙って終わるとそれが隠れます(原理 1)。

`Request` に `reset` が要りました。owner field への代入ができないので、次の
request を新しく作って差し替えることができず、同じ Request に parse し直します
(`parse_request_head_into`)。結果として storage を再利用するので、keep-alive の
目的にはむしろ合っています。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `max_requests` の既定を無制限にする(HTTP/1.1 の既定) | connection retention と、ready な 1 worker が譲らず進む量を無制限にする。実測で選んだ有限の 100 と 5 秒の idle deadline を既定にする(原理 8) |
| keep-alive を bool にする | 「何通まで」は上限であり、`Limits` はそれを持つ場所。数値なら有限の既定を持ち、caller が service ごとに下げたり上げたりできる |
| `Connection` header を caller に書かせる | 接続が続くかは server 側の 3 条件が決めるもので、caller が 1 つしか知らない。`is_framing_field` が落とす規則の例外を増やすことにもなる |
| 接続を表す別の型(`Connection`)を作り、`Exchange` に貸す | Kizu の struct は borrow を持てない(`docs/language-gaps.md`)。`Exchange` が接続を所有したまま次を読む形しか書けない |
| `next` が終わっていない exchange に false を返す | caller の bug が「接続が終わった」に化ける。原理 1 |
| `next` が未読の request body を自動で読み捨てる | 隠れた I/O。`accept_head` を選んだ caller は body を自分で扱うと言ったので、読み捨てるのは その宣言を無視すること |
| `UntilClose` を残さず `Chunked` に一本化する | HTTP/1.0 client は chunked を読めない。close framing はその相手への唯一の答えで、`Connection: close` を送る接続では今も正しい |
| chunked の size を小文字 hex にする(慣例) | chunk-size は `1*HEXDIG` で case-insensitive。percent-encoding が既に持っている大文字表を 2 つ目にする理由がない |
