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

**Kizu は Go の登録簿の形を書けません。** 関数値(`fn(...) -> T`)は borrow parameter
をまだ運べません。`Function` static parameter は呼べるようになりましたが、1
instantiation に 1 function を固定するもので、runtime の handler 登録簿では
ありません。

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

### 多数を扱うときも接続の手渡しを見せる

接続ごとの直線 code は、`accept_connection` が request をまだ読んでいない
`Exchange` を返し、caller がそれを TaskSet worker へ move します。worker は
`read_head` と `respond` を普通に呼びます。accept と spawn の両方が source にあり、
handler registry は増えません。server が接続表を持つ `first` / `next` も残します。

### response の framing は caller が選ぶ

小さな response は組み立ててから送り、実測した `Content-Length` を書きます。
stream する caller は `respond_head` で `Length` / `Chunked` / `UntilClose` / `Raw`
を明示し、その後の `write_all` と `finish_body` も source に書きます。

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
| handler を `comptime Function` static parameter で取る(#1079 の当初案) | 現在は呼べるが 1 server instantiation = 1 handler で、routing は結局 handler 内の `match`。accept / spawn / handler call が直接書けるので wrapper に隠す情報が無い |
| `Router` に handler を登録する | 同じ理由。`array::new<fn(...) -> T>` は parser が static 引数として `fn` を受け付けず、struct field の `fn` は非 copy 扱いで `r.handler(x)` が method 呼び出しに読まれる |
| `path_of` が percent 復号する | segment の中の `%2F` は区切りではない。復号してから分けるのが path traversal の通り道 |
| connection pool / redirect 追従を client に入れる | どちらも policy(socket をどれだけ保つか、別 host への 301 は同じ request か)で、library が選んだ policy は呼ぶ側から見えない policy |
| descriptor を `?i64` にして close 後を null にする | 型で閉じられる検査を runtime に落とす(原理 5)。`deinit` が消費すれば、close 後の値は存在しない |
| `Headers` を `Map` にする | HTTP は同名の繰り返しに意味を与える(`Set-Cookie`)。map にすると順序が「失わないもの」から「復元するもの」に変わる。message の field は数十で、走査は数回の比較 |
| header 値を wire bytes への borrow で持つ | 読み取り buffer は次の message のために再利用する。borrow なら次の read で dangling view になる |

## Consequences

- `accept` は逐次の最小 API のまま。production の 1-thread server は
  `accept_connection` + TaskSet で connection ownership を worker に渡すか、
  `first` / `next` で server に接続表を持たせる
- `docs/tutorial/web-server` が service を 1 つ書き切る。sample は conformance
  の case なので、動かなくなった tutorial はテストが落とす
- descriptor は safe Kizu に出ない。private field に持つので、socket に届く
  唯一の道はこの module が返した値
- `std::net::Error` と `std::http::Error` を足したので、std が assign する
  error code が後ろの set でずれる。ir / llvm corpus は再生成した
- std::http を書いたことで見つかった language gap は
  `docs/language-gaps.md` に記録した(関数 pointer の borrow parameter、
  `[]u8` の等値比較が LLVM に降りないこと)
