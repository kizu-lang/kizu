# ADR-0143: body は保持するものではなく読むもの

## Status

Accepted.

## Context

`std::http::Request` は `body: String` を持っていました。`accept` が読み切って
から返すので、caller は `request.body` を読むだけで済みます。代償は 2 つです。

- 2 GB の upload は `accept` では受けられない。ADR-0138 が `accept_head` を
  開けたのはこのためで、経路が 2 つになった
- 上限が `Limits.max_body_bytes` という **1 つの数**になり、`/upload` と
  `/api/users` が同じ数を共有する

先例は 3 つとも同じ形でした。Go の `r.Body` は `io.ReadCloser`、Zig の
`request.reader()`、hyper の `Incoming` —— **body を保持して返す server API は
どれにも無い**。Go に `MaxBodyBytes` が無いのもこの帰結で、上限は
`http.MaxBytesReader(w, r.Body, n)` を handler が被せるもの、`MaxHeaderBytes`
だけが server にあります。Zig は `readAllAlloc(allocator, max)` —— **max を
渡さないと呼べない**。

## Decision

`Request` から `body` を落とします。body は接続を持つ `Exchange` から読み、
**上限は byte を求める呼び出しの引数**です。

```kizu
var exchange = try server.accept(handle, allocator, 65536);   // exchange.body に入る
var exchange = try server.accept_head(handle, allocator);     // 接続に残る。上限なし
```

`Limits` から `max_body_bytes` が消えます。`max_head_bytes` は残ります ——
head の大きさは protocol の都合で全 endpoint 共通、body の大きさは endpoint が
何を受け取るかで決まるからです。

`accept` は残します。Go にも Zig にも無い関数ですが、Kizu には closure も GC も
無いので `accept_head` + 読み loop は 4 行で、便利関数の価値がそのぶん高い。

## Consequences

**畳むのは結果として起きました。** body を保持しなくなると blocking 側の
`read_request_into` / `read_request_body` / `read_chunked_body` が行き場を失い、
`accept` は `accept_head` + 読み loop に、`accept_head` は `step` + 待つ、に
なります。head→body の順序付けが 2 箇所にあった状態が 1 箇所になりました。

`advance` の `Progress::Request` は「**head が揃った**」になりました。body は
`read_ready_into` —— `read_into` の待たない版 —— で読みます。

framing を持たない request は body ゼロで確定します。以前は「上限なし」の扱いで、
`read_into` が次の request を読みに行ける状態でした。`respond_head(Raw)` が
その封を開けます —— 101 の後に来るものはこの message の body ではありません。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `Limits.max_body_bytes` を残し、`accept` はそこから読む | `accept` 1 つのためだけに残る field になり、`Limits` の他の field(全経路に効く)と性質が変わる。呼び出しを読んでも上限が見えない |
| `accept` を捨てて `accept_head` だけにする(Go / Zig の形) | Go は `io.ReadAll(r.Body)` が 1 行だから捨てられる。Kizu は String を作って `defer` を書いて loop を回して 4 行 |
| `accept` が caller の String に読む | `accept_head` + 読み loop と同じ行数になり、便利関数である理由が消える |
| `body` を `Request` に残し、`accept_head` のときだけ空にする | 「入っているか」が呼んだ関数で決まる field を、head だけを表す型に置くことになる。`Exchange` は接続を持つので、body がそこにあるのは読める |
| `read_into` が待たない版を持たない | poller の loop から body を読む道が無くなる。`advance` が head で止まる以上、body の読みにも待たない版が要る |
