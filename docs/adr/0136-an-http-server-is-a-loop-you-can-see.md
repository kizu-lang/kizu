# ADR-0136: HTTP server は見える loop で、handler を取らない

## Status

Accepted.

## Context

`std::net` と `std::http` を入れるにあたり、server の形を決める必要がありました。
参照する 2 つは、どちらも handler を取ります。

```go
http.HandleFunc("/items/", handleItems)   // Go
http.ListenAndServe(":8080", nil)
```

```zig
var request = try server.receiveHead();   // Zig: pull
try request.respond("hello", .{});
```

Go は登録簿に handler を入れ、runtime が選んで呼びます。Zig は
`receiveHead()` が request を返し、呼ぶ側が答えます。

**Kizu は Go の形を書けません。** 関数値(`fn(...) -> T`)は borrow parameter を
運べず、call site で `&x` が `x` として型検査されます。`Function` static
parameter(SPEC §13)は型検査だけあって lowering を持たず、呼べません。
つまり handler に `&Request` を渡す綴りが言語にありません。

書けたとしても、原理 2 が同じ答えを指します。**hidden control flow とは
「呼び出しが source に見えないこと」**で、登録簿から選ばれた handler の呼び出しは
呼び出し元の source にありません。

## Decision

### server は pull の loop

`accept` が Exchange —— 届いた request と、それに対して返す response —— を返して
戻ります。loop は呼ぶ側が書きます。

```kizu
var exchange = try server.accept(handle, allocator, 1048576);
defer exchange.deinit(allocator);
try exchange.respond_text(handle, allocator, 200, "text/plain", "hello");
```

**routing も同じです。** `route(allocator, pattern, method, path, params)` は
pattern 1 つを照合して答えるだけで、handler は保持しません。呼ぶ側の `if` の
連鎖が router です。pattern の綴りは Go 1.22 の `ServeMux`(`{name}` /
`{rest...}`)を借ります —— 答えの形は違っても、問いの綴りは既に良いものが
あるからです。

### response は組み立ててから送る

socket に何も届かないのは `encode` が走るまでです。それによって status line が
body の実際の長さを持つ `Content-Length` を運べます。代償は「response は body の
分だけ大きい」ことですが、代わりに chunked transfer encoding が要りません ——
それは今の実装が話せない framing です。

`Content-Length` / `Transfer-Encoding` / `Connection` は message の実体から
書き、caller が set したものは落とします。矛盾する 2 つを並べて送れないためです。

### socket の owner は deinit で消費される

`TcpListener` / `TcpStream` / `Server` / `Exchange` の `deinit` は `self` を
値で取ります。close の後に値が残らないので、**close 後の使用は型 error** です ——
runtime が「その descriptor は閉じている」と言うのではなく、書いた場所で
拒否されます(原理 5)。

### client は分解しても公開する

`get` / `post` / `fetch` は connect + write + read + close の 1 呼び出しですが、
書く側と読む側は `Connection` として別々にも呼べます(ADR-0143)。

理由は 2 つあります。`get` は read で待つので、同じプロセスの server に対しては
使えません(accept が同じ thread にある)—— 分解できることが、test が両端に
なれる唯一の道です。そしてこれは、まだ無い層 —— TLS、proxy —— が刺さる継ぎ目でも
あります。それらは stream を所有するのであって、client を置き換えるのでは
ありません。

### 拒否するものは名前で拒否する

| 入力 | 答え |
| --- | --- |
| folded header 行(`\r\n` の後の空白始まり) | `MalformedRequest` |
| `Transfer-Encoding` を持つ request | `UnsupportedEncoding` |
| header 名の空白 / コロン、値の CR / LF | `InvalidHeader` |
| `HTTP/` で始まらない version token | `MalformedRequest` |
| `HTTP/` で始まる未対応 version | `UnsupportedVersion` |
| `https://` の URL | `UnsupportedScheme` |

folding は RFC 9110 が protocol から外したもので、繋ぐのが 1 つの header の中に
別の header を密輸する道です。`Transfer-Encoding` を推測して読むのが 2 つの
request が 1 つになる道(request smuggling)です。`https` を平文で送るのは、
暗号化を頼まれたものを暗号化しないことです。

request を読めなかったとき、`accept` は**その場で status を返してから**失敗を
返します。client は status を受け取る権利があり、caller は parse すらしていない
request に対して status を送れません。

## 却下

| 案 | 却下理由 |
| --- | --- |
| Go 型の handler 登録簿(`HandleFunc` + `ListenAndServe`) | 言語が書けない(関数値が borrow を運べない)。書けたとしても、選ばれた handler の呼び出しが呼び出し元の source に無い(原理 2) |
| handler を `comptime Function` static parameter で取る(#1079 の当初案) | `Function` は型検査だけで lowering が無く、呼べない。入れるなら instantiation まで作る言語作業で、std の作業ではない。しかも 1 server = 1 handler になり、routing は結局 handler の中の `match` になる |
| `Router` に handler を登録する | 同じ理由。`array::new<fn(...) -> T>` は parser が static 引数として `fn` を受け付けず、struct field の `fn` は非 copy 扱いで `r.handler(x)` が method 呼び出しに読まれる |
| response を streaming にし、最初の `write` で head を送る | chunked transfer encoding が要る。それが無いまま streaming にすると、Content-Length を先に宣言させるか、嘘の長さを送るかしかない。buffering は正直な形 |
| keep-alive を入れる | 1 接続ずつしか捌けないので、1 人の client が idle のまま全員を止める。並行性が先(SPEC §15) |
| request の chunked を読む | 読むだけなら安全だが、書く側が無いので「受けられるが返せない」形が残る。framing の対称性は 1 つの判断として一緒に決める |
| `path_of` が percent 復号する | segment の中の `%2F` は区切りではない。復号してから分けるのが path traversal の通り道 |
| connection pool / redirect 追従を client に入れる | どちらも policy(socket をどれだけ保つか、別 host への 301 は同じ request か)で、library が選んだ policy は呼ぶ側から見えない policy |
| descriptor を `?i64` にして close 後を null にする | 型で閉じられる検査を runtime に落とす(原理 5)。`deinit` が消費すれば、close 後の値は存在しない |
| `Headers` を `Map` にする | HTTP は同名の繰り返しに意味を与える(`Set-Cookie`)。map にすると順序が「失わないもの」から「復元するもの」に変わる。message の field は数十で、走査は数回の比較 |
| header 値を wire bytes への borrow で持つ | 読み取り buffer は次の message のために再利用する。borrow なら次の read で dangling view になる |

## Consequences

- server は 1 接続ずつ。2 人目は listen backlog(128)で待つ。これは Zig の
  `std.http.Server` が今日出荷している形と同じで、並行性は呼ぶ側が持ち込む ——
  ただし Kizu の呼ぶ側はまだ何も持ち込めない。**milestone であって
  production server ではない**
- `docs/tutorial/web-server` が service を 1 つ書き切る。sample は conformance
  の case なので、動かなくなった tutorial はテストが落とす
- descriptor は safe Kizu に出ない。private field に持つので、socket に届く
  唯一の道はこの module が返した値
- `std::net::Error` と `std::http::Error` を足したので、std が assign する
  error code が後ろの set でずれる。ir / llvm corpus は再生成した
- std::http を書いたことで見つかった language gap は
  `docs/language-gaps.md` に記録した(関数 pointer の borrow parameter、
  `[]u8` の等値比較が LLVM に降りないこと)
