# ADR-0138: 接続には Exchange 越しに届く。渡しはしない

## Status

Accepted.

## Context

ADR-0136 の server は `accept` が `Exchange` を返し、`respond` が答えを送って
終わりでした。`Exchange` が `TcpStream` を private に持つので、caller が接続に
触れる道はありません。その結果、次のどれも書けませんでした。

- server-sent events —— head を送ってから書き続ける
- `101 Switching Protocols` / WebSocket —— head の後は別の protocol
- 大きな response —— body 全体を組み立ててから送るので、100 MB の答えに 100 MB
- 大きな request —— body を `max_body_bytes` の下で保持するので、upload は
  「読める」ではなく「上限まで」

client 側は既に開いていました(`write_request` / `read_response_from` は caller の
stream を取る)。閉じているのは server 側だけ、という非対称でした。

## Decision

`Exchange` に 3 種類の口を足し、接続そのものは渡しません。

**送る側**は `respond_head(io, allocator, framing)` が head だけを送り、body は
caller が `write_all` で書きます。`Framing` は head が「body がどこで終わるか」に
ついて何と言ったかで、`Buffered` / `Length(n)` / `UntilClose` / `Raw` の 4 つです。

**読む側**は `accept_head` が空行で止まり、body を接続に残します。caller は
`read_into` で読みます。`max_body_bytes` は掛かりません —— body を保持していない
ので、上限を掛ける対象がありません。

**接続そのもの**は private のままで、`write_all` / `read_into` /
`set_read_deadline` / `set_write_deadline` / `clear_*` だけが通ります。

## Consequences

`Length(n)` の `n` は caller の申告で、検証しません。見ていない byte は数えられず、
`Buffered` だけが実測です。申告と実際が食い違えば接続は desync しますが、これは
「body を自分で書く」の意味そのものです。

`Raw` は framing field を一切書かず、caller の header をそのまま出します。
std::http が組み立てなかった head はこの 1 語を grep すれば全部出ます(原理 3)。

`write_all` と `read_into` は deadline に触りません。phase の始まり
(`respond_head` / `accept_head`)が 1 回置き、1 phase より長く生きる caller は
`set_read_deadline` で自分で押し直します。触ると呼び出しごとに budget が復活して、
ADR-0137 が避けた形に戻ります。

keep-alive はまだ無いので、`UntilClose` は「close が終わり」で正しいままです。
chunked transfer encoding が入ると 5 つ目の framing になります。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `stream()` が `&var TcpStream` を返す | Kizu は struct field への borrow を返す method を書けない(`docs/language-gaps.md`)。`&var self.field` も暗黙 borrow も型 error |
| `stream` を public field にする | 書ける(型 system が `deinit` と move out を既に拒否する)が**罠**。head を読むと packet 単位で読むので次の byte は既に `Exchange` の中にあり、`exchange.stream.read_into` はそれを飛ばす。実際に test を書いていて踏んだ。Go の `Hijacker` が接続と buffered reader を**両方**返すのはこの理由 |
| `into_stream()` が Exchange を消費して stream を返す | 上と同じ残り byte の問題に加え、caller は答えていた request をまだ読みたい。所有権を手放す必要が実際には無い —— `Exchange` が生きている間 raw に読み書きできれば WebSocket の loop は書ける |
| body を書く前に head を必須にする | `Raw` の後の takeover には head の無い形もあり得る。`read_into` は head の前(`accept_head` 直後)にも要る |
| `accept_head` でも `Transfer-Encoding` を通す | framing を決めるのは request smuggling の入口。閉→開は additive(原理 8)なので、chunked が入ってから開ける |
| chunked transfer encoding を先に入れる | `Connection: close` framing で長さ不明の body は既に送れる。keep-alive が無い今、chunked が増やすのは「同じ接続で次を送れる」だけで、それは keep-alive の仕事 |
| `Framing` を渡さず、`Content-Length` header を caller が set したら尊重する | `is_framing_field` が落とす規則を条件付きにすることになり、「落とすのか尊重するのか」が header の中身で決まる。framing は message の性質なので引数で言う(原理 7) |
| `write_all` / `read_into` が呼ぶたび deadline を張り直す | 呼び出しごとに budget が復活する。ADR-0137 が SO_RCVTIMEO を却下したのと同じ理由 |
