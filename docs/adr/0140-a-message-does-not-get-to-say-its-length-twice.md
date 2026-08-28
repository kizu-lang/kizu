# ADR-0140: message は自分の長さを 2 回言えない

## Status

Accepted.

## Context

ADR-0139 で送る側の chunked が入りましたが、受ける側は `Transfer-Encoding` が
あれば拒否したままでした。長さを事前に知らない client —— 生成しながら送る、
pipe から読む —— は `Content-Length` を書けないので、拒否は「そういう client を
相手にしない」と同じでした。

chunked を読むということは、**HTTP/1 で request smuggling が住んでいる場所**を
実装するということです。決めるべきは decode の手順ではなく、拒否する形でした。

## Decision

`Transfer-Encoding: chunked` を request でも response でも decode します。
decoder は buffer の上の state machine 1 つで、caller が buffer を詰め直します
—— body を丸ごと読む経路も、1 片ずつ渡す経路も同じものを使います。

拒否するのは 4 つです。

| 入力 | 答え |
| --- | --- |
| `Content-Length` と `Transfer-Encoding` の両方 | `ConflictingFraming` |
| `chunked` 以外の coding、または list | `UnsupportedEncoding` |
| size が hex でない / chunk の後ろが CRLF でない | `MalformedChunk` |
| decode 後の合計が上限超え | `BodyTooLarge` |

1 つ目が中心です。RFC 9112 は encoding が勝つと言いますが、**前段の proxy が
逆に判断したときに 1 つの request が 2 つになります**。どちらかを選ぶのではなく、
自分の長さについて 2 つのことを言う message を読みません。

decoder の上限は `std::mem::Limit` です。`accept` は `Bytes(max_body_bytes)`、
`accept_head` は `Unlimited` —— 保持していないものに上限は掛かりません。

## Consequences

`read_into` の 0 が「body の終わり」であることは framing に依らなくなりました。
`Content-Length` が尽きても terminator が来ても 0 なので、読む側の loop は
どちらだったかを知らずに書けます。

chunk extension は読み飛ばします。framing を変えるものが無いので、理解できない
値は message を拒否する理由になりません。trailer は消費して捨てます。

`MalformedChunk` は `MalformedRequest` / `MalformedResponse` と別の member です。
chunk の framing は**どちら向きにもある**もので、同じ decoder が両方を読むので、
どちらかの名前を借りると片方で嘘になります(原理 7)。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `Content-Length` と `Transfer-Encoding` の併記で encoding を優先する(RFC 9112 の文面) | 文面通りでも安全にならない。前段が逆に解釈した瞬間に 1 request が 2 つになる。RFC は「どう読むか」を決めているが、決めないことも許している |
| 併記を `Content-Length` 優先で読む | 同じ穴の向きが変わるだけ |
| `Transfer-Encoding: gzip, chunked` を受ける | chunked は最後に来る規則なので framing 自体は読めるが、gzip を decode できない以上 body を渡せない。読めない message を受けたことにしない |
| chunk extension を拒否する | framing を変えるものが無い。理解できない値で message を落とすのは、読める message を読まないこと |
| trailer を request の header に足す | `Trailer` で予告されたものだけを足す規則が要り、予告なしの trailer を header に混ぜると head を読み終えた後に header が増える。読む側の前提が壊れる |
| decoder の上限を常に `max_body_bytes` にする | `accept_head` は body を保持しないので、その上限には意味がない。実際 example が 64 byte 上限で 5000 byte の stream を読めなくなった。`mem::Limit` で「保持しない = Unlimited」と書ける |
| decoder を 2 つ書く(丸ごと用と streaming 用) | 同じ規則が 2 箇所になり、片方だけ直る。buffer を caller が詰め直す 1 つの state machine で両方が書ける |
| `ChunkState` を `&var` で借りたまま buffer も借りる | `self` の 2 重借用になって checker が拒否する。state は数値 6 つなので値で出して戻す |
