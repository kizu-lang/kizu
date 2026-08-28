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

**`max_requests` の既定は 1 です。** HTTP/1.1 の既定ではありません。

## Consequences

既定が 1 なのは protocol ではなくこの server の事情です。並行 API が無い
(SPEC §15)ので 1 接続ずつしか捌けず、2 通目のために接続を保持する peer は
他の全員に対して保持しています。keep-alive が節約するのは handshake 1 回で、
払うのは service 全体です。並行性が入ったら既定を見直す判断になります。

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
| `max_requests` の既定を無制限にする(HTTP/1.1 の既定) | 1 接続ずつしか捌けないので、1 人が接続を保持すると service が止まる。ADR-0137 で閉じた穴を別の入口から開け直すことになる |
| keep-alive を bool にする | 「何通まで」は上限であり、`Limits` はそれを持つ場所。数値なら 1 が今日の挙動と同じで、既定を変えずに開けられる |
| `Connection` header を caller に書かせる | 接続が続くかは server 側の 3 条件が決めるもので、caller が 1 つしか知らない。`is_framing_field` が落とす規則の例外を増やすことにもなる |
| 接続を表す別の型(`Connection`)を作り、`Exchange` に貸す | Kizu の struct は borrow を持てない(`docs/language-gaps.md`)。`Exchange` が接続を所有したまま次を読む形しか書けない |
| `next` が終わっていない exchange に false を返す | caller の bug が「接続が終わった」に化ける。原理 1 |
| `next` が未読の request body を自動で読み捨てる | 隠れた I/O。`accept_head` を選んだ caller は body を自分で扱うと言ったので、読み捨てるのは その宣言を無視すること |
| `UntilClose` を残さず `Chunked` に一本化する | HTTP/1.0 client は chunked を読めない。close framing はその相手への唯一の答えで、`Connection: close` を送る接続では今も正しい |
| chunked の size を小文字 hex にする(慣例) | chunk-size は `1*HEXDIG` で case-insensitive。percent-encoding が既に持っている大文字表を 2 つ目にする理由がない |
