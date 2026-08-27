# std::http

`std::http` は HTTP/1 を `std::net` の上に Kizu で書いたものです。

**server は見える loop です。** handler の登録簿も callback もありません。
`accept` が Exchange を 1 つ —— 届いた request と、それに対して返す response ——
を返して戻ります。request に何をするかは、request を求めた場所のソースに書きます。

```kizu
var server = try http::listen(handle, "127.0.0.1:8080");
defer server.deinit();
var exchange = try server.accept(handle, allocator);
defer exchange.deinit(allocator);
try exchange.respond_text(handle, allocator, 200, "text/plain", "hello");
```

Go と Zig はどちらも handler を取りますが、それはどちらも handler を**渡せる**
からです。Kizu の関数値は borrow を運べないので、handler に request を渡せません。
残るのは pull の loop で、それは control flow が見えたままになる形でもあります
(`docs/principles.md` §2)。

**1 接続ずつです。** Kizu に並行 API はありません(SPEC §15)ので、2 人目の
caller は 1 人目が答え終わるまで listen backlog で待ちます。

## API

```kizu
pub fn listen(io: Io, address: []u8) -> std::http::Failure!std::http::Server
pub fn listen_with(io: Io, address: []u8, limits: Limits)
    -> std::http::Failure!std::http::Server

fn (self: &Server) local_port() -> std::http::Failure!i64
fn (self: &var Server) accept(io: Io, allocator: Allocator)
    -> std::http::Failure!std::http::Exchange
fn (self: Server) deinit() -> void

fn (self: &var Exchange) respond(io: Io, allocator: Allocator)
    -> std::http::Failure!void
fn (self: &var Exchange) respond_text(
    io: Io, allocator: Allocator, status: i64, content_type: []u8, body: []u8,
) -> std::http::Failure!void
fn (self: &Exchange) is_answered() -> bool
fn (self: Exchange) deinit(allocator: Allocator) -> void
```

`Exchange` は `request` と `response` を public field に持ちます。
`exchange.request.headers` を読み、`exchange.response.write(...)` を書きます。

## Request

```kizu
pub struct Request {
    pub method: std::string::String,
    pub target: std::string::String,
    pub version: std::http::Version,
    pub headers: std::http::Headers,
    pub body: std::string::String,
}
```

method・target・header は**所有した copy** です。読み取り buffer は次の message の
ために再利用されるので、そこを borrow していたら次の read で dangling view に
なります。1 field あたり 1 copy を払って、request が wire bytes より長生きします。

path と query は field ではありません。どちらも target の中の run で、`String` への
view はその field の borrow(ADR-0111)なので、`path_of` / `query_of` を
`request.target.as_bytes()` に対して使います。

```kizu
let target = request.target.as_bytes();
let path = http::path_of(target);     // "/items"
let query = http::query_of(target);   // "id=7"
```

`path_of` は decode しません。path segment の中の `%2F` は区切りではなく、
それを区切りに畳むのが path traversal の通り道だからです。

## Headers

```kizu
pub struct Field {
    pub name: std::string::String,
    pub value: std::string::String,
}

pub fn headers_new(allocator: Allocator) -> std::http::Headers

fn (self: &Headers) count() -> i64
fn (self: &Headers) at(index: i64) -> ?&std::http::Field
fn (self: &Headers) find(name: []u8) -> ?&std::http::Field
fn (self: &Headers) has(name: []u8) -> bool
fn (self: &var Headers) add(allocator, name, value) -> std::http::Failure!void
fn (self: &var Headers) set(allocator, name, value) -> std::http::Failure!void
fn (self: &var Headers) remove(allocator, name) -> void
fn (self: &var Headers) clear(allocator) -> void
```

順序は保ちます —— HTTP は同名の繰り返しに意味を与えます(`Set-Cookie` など)。
lookup は ASCII の A-Z だけを畳みます。`find` が返すのは field への borrow なので、
値は `let value = field.value.as_bytes();` で読みます。

map ではなく list です。message が持つ field は数十で、走査は数回の比較です。
map にすると順序が「失わないもの」から「復元するもの」に変わります。

`add` / `set` は名前と値の byte を検査します。名前に空白やコロン、値に CR / LF が
あれば `Error::InvalidHeader` です —— それが response splitting の通り道です。

## Response

```kizu
pub fn response_new(allocator: Allocator) -> std::http::Response

fn (self: &Response) status() -> i64
fn (self: &Response) body_len() -> i64
fn (self: &Response) is_finished() -> bool
fn (self: &var Response) set_status(status: i64) -> std::http::Failure!void
fn (self: &var Response) header(allocator, name, value) -> std::http::Failure!void
fn (self: &var Response) write(allocator, bytes) -> std::http::Failure!void
fn (self: &var Response) finish() -> void
fn (self: &var Response) reset(allocator) -> void
```

response は**全部組み立ててから送ります**。socket に何も届かないのは `encode` が
走るまでで、それによって status line が body の実際の長さを持つ Content-Length を
運べます。代わりに chunked transfer encoding が要らなくなります —— それは今の
実装が話せない framing です。

代償は「response は body の分だけ大きい」ことです。大きな body を stream する
server には chunked encoding が要ります。それが入るまで、buffering が正直な形です。

`finish` の後は `set_status` / `header` / `write` すべてが
`Error::ResponseFinished` を返します。

`Content-Length` / `Transfer-Encoding` / `Connection` は caller が set しても
落とします。message が実際に何であるかから書くもので、矛盾する 2 つを並べて
送るわけにはいきません。1xx / 204 / 304 は body を持たないので Content-Length も
付きません。

## Limits

```kizu
pub struct Limits {
    pub max_head_bytes: i64,   // default 8192
    pub max_headers: i64,      // default 64
    pub max_body_bytes: i64,   // default 1048576
}
pub fn default_limits() -> std::http::Limits
```

上限は caller のものです。proxy の後ろの server と、公開 internet の server が
同じ上限を欲しがるとは限らず、名指せない上限は誰にも上げられません。

## 読めなかった request

`accept` が request を読めなかったとき、`accept` は**その場で status を返して**
から失敗を返します。client は status を受け取る権利があり、caller は parse すら
していない request に対して status を送れないからです。

| 失敗 | status |
| --- | --- |
| `MalformedRequest` / `Incomplete` / `InvalidHeader` | 400 |
| `HeadTooLarge` | 431 |
| `BodyTooLarge` | 413 |
| `UnsupportedVersion` | 505 |
| `UnsupportedEncoding` | 501 |
| その他 | 500 |

## 今は話さないこと

- **keep-alive**: 1 接続 1 request で、response は `Connection: close` を送ります
- **chunked transfer encoding**: request に `Transfer-Encoding` があれば
  `Error::UnsupportedEncoding` です。推測して読むのが request smuggling の
  通り道なので、拒否します
- **HTTPS / TLS**、**HTTP/2**、**HTTP/3**
- **header folding**: RFC 9110 が protocol から外したもので、繋ぐのではなく拒否します

## エラー

`std::http::Error` は `MalformedRequest`、`HeadTooLarge`、`BodyTooLarge`、
`UnsupportedVersion`、`UnsupportedEncoding`、`Incomplete`、`InvalidStatus`、
`InvalidHeader`、`InvalidUrl`、`ResponseFinished`、`InvalidEncoding` を持ちます。

`std::http::Failure` はその和 —— `Error or std::net::Error or std::mem::Error` ——
です。どれも変換されないので、`match` した caller はどの層が拒否したかを見ます。
