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
fn (self: &var Server) accept_head(io: Io, allocator: Allocator)
    -> std::http::Failure!std::http::Exchange
fn (self: Server) deinit() -> void

fn (self: &var Exchange) respond(io: Io, allocator: Allocator)
    -> std::http::Failure!void
fn (self: &var Exchange) respond_text(
    io: Io, allocator: Allocator, status: i64, content_type: []u8, body: []u8,
) -> std::http::Failure!void
fn (self: &var Exchange) respond_head(
    io: Io, allocator: Allocator, framing: std::http::Framing,
) -> std::http::Failure!void
fn (self: &var Exchange) write_all(io: Io, bytes: []u8) -> std::http::Failure!void
fn (self: &Exchange) owes() -> i64
fn (self: &var Exchange) read_into(
    io: Io, allocator: Allocator, out: &var std::string::String, max: i64,
) -> std::http::Failure!i64
fn (self: &var Exchange) set_read_deadline(at: i64) -> void
fn (self: &var Exchange) set_write_deadline(at: i64) -> void
fn (self: &var Exchange) clear_read_deadline() -> void
fn (self: &var Exchange) clear_write_deadline() -> void
fn (self: &var Exchange) next(io: Io, allocator: Allocator)
    -> std::http::Failure!bool
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
    pub max_head_bytes: i64,     // default 8192
    pub max_headers: i64,        // default 64
    pub max_body_bytes: i64,     // default 1048576
    pub read_head_millis: i64,   // default 10000
    pub read_body_millis: i64,   // default 30000
    pub write_millis: i64,       // default 30000
    pub max_requests: i64,       // default 1
    pub idle_millis: i64,        // default 5000
}
pub fn default_limits() -> std::http::Limits
```

上限は caller のものです。proxy の後ろの server と、公開 internet の server が
同じ上限を欲しがるとは限らず、名指せない上限は誰にも上げられません。

### 時間の上限

`*_millis` は 1 message の各 phase に許す時間です。**duration** であって
時点ではありません —— policy は「どれだけ許すか」であり、「いつまでか」は
phase が始まったときに決まるからです(`std::net` の deadline がその時点です)。

phase は 3 つで、それぞれが**自分の deadline を、自分が始まるときに**作ります。

| field | 覆う範囲 |
| --- | --- |
| `read_head_millis` | 接続を受けてから、head の空行までを読み切るまで |
| `read_body_millis` | body の 1 byte 目から、`Content-Length` 分を読み切るまで |
| `write_millis` | `respond` が response を書き始めてから、書き切るまで |

body に head と別の deadline を与えているのは、共有すると大きな body が
「head が余らせた分」しか貰えないからです。`write` の deadline を `accept` では
なく `respond` で作っているのも同じ理由で、その間に caller が何をしていても
書き始めから測ります。

**0 は「deadline を置かない」**という意味です。相手が黙り込めばその phase は
待ち続けるので、これは選択であって既定ではありません。Go は timeout の既定が
0 で、忘れた server は 1 接続で止まります。ここでは既定を数値にしてあります。

phase の deadline は全体を覆うので、4 秒ごとに 1 byte 送る相手も head の
`read_head_millis` を使い切って落ちます。理屈は
[net の deadline](net.md#deadline) にあります。

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
| `net::Error::TimedOut` | 408 |
| その他 | 500 |

## body を自分で書く / 自分で読む

`respond` は message 全体を組み立ててから送ります。Content-Length を body の
実測値から書けるのがその見返りで、代償は **response が body の大きさになる**
ことです。100 MB の答えに 100 MB のメモリは払えませんし、終わりの決まっていない
stream には書ける length がありません。

`accept` も同じ形で、body を `max_body_bytes` の下で request に読み切ります。
form には正しく、upload には間違っています —— 上限があるのは body を保持して
いるからです。

その 2 つを開けるのがこの節です。

### 送る側: `respond_head` と `Framing`

```kizu
pub union Framing {
    Buffered,       // Response が持つ body。length は実測
    Length(i64),    // caller が head の後にちょうどこの数だけ書く
    UntilClose,     // close が body の終わり。length は無い
    Chunked,        // 1 write = 1 chunk。terminator が終わり
    Raw,            // framing field を一切書かない。head は caller のもの
}

fn (self: &var Exchange) respond_head(io, allocator, framing) -> Failure!void
fn (self: &var Exchange) write_all(io, allocator, bytes) -> Failure!void
fn (self: &var Exchange) finish_body(io, allocator) -> Failure!void
```

`respond_head` は head を送って止まります。その後の body は caller が
`write_all` で書きます。head が「body がどこで終わるか」について何と言ったかが
`Framing` です。

| framing | Content-Length | Connection | 用途 |
| --- | --- | --- | --- |
| `Buffered` | 実測値 | `close` | `respond` が使う既定 |
| `Length(n)` | `n` | `close` | 大きさの分かる file |
| `UntilClose` | 書かない | `close` | SSE、長さ不明の stream |
| `Chunked` | 書かない(`Transfer-Encoding: chunked`) | `close` | 長さ不明で、接続を閉じたくないとき |
| `Raw` | 書かない | **書かない** | 101 Switching Protocols |

`Length(n)` の `n` は **caller の申告**で、`Buffered` だけが実測です。ただし
申告した以上、**書きすぎは拒否します** —— `write_all` は全 byte を通るので
数えられます。

```kizu
try held.respond_head(handle, allocator, http::Framing::Length(6));
try held.write_all(handle, "abc");        // owes() == 3
try held.write_all(handle, "toolong");    // Error::ResponseOverrun
```

socket に届く前に拒否するので、残りの数は変わりません。超過分が線に乗ると、
peer はそれを次の message の始まりとして読みます。

**足りない**方は `finish_body` で拒否します —— caller が「書き終えた」と言った
瞬間が、短いことを言える唯一の場所です。

```kizu
try held.respond_head(handle, allocator, http::Framing::Length(10));
try held.write_all(handle, allocator, "short");
try held.finish_body(handle, allocator);   // Error::ResponseIncomplete
```

`finish_body` を呼ばずに `deinit` した場合は検出できません(`deinit` は失敗を
返せない)。その場合の残りは `owes()` が答えます。

```kizu
fn (self: &Exchange) owes() -> i64
```

`Length` 以外の framing は数を持たないので、`owes()` は常に 0 です。

### `Chunked`

`write_all` 1 回が 1 chunk で、size を hex で前置きします。長さゼロの write は
落とします —— 長さゼロの chunk は terminator で、それを送るのは
`finish_body` の仕事だからです。

```
8\r\npiece 0\n\r\n8\r\npiece 1\n\r\n0\r\n\r\n
```

`UntilClose` との差は **接続を閉じるかどうか**です。`UntilClose` は close が
body の終わりなので、その接続は次を運べません。`Chunked` は terminator が
終わりなので運べます —— [keep-alive](#keep-alive) が使えるのはこちらだけです。

`finish_body` は body を閉じます。`Chunked` は terminator を線に出し、
`Length` は数が合っているかを見ます。`UntilClose` と `Raw` は何も要りません
—— 前者は close が、後者は caller が終わりを決めます。2 度目の `finish_body` は
`Error::ResponseFinished` です。

`Raw` は caller の header をそのまま出し、framing field を落としもしません。
101 に `Connection: close` を書いたら間違った message になるからです。この 1 語を
grep すれば「std::http が組み立てなかった head」が全部出ます(原理 3)。

`respond_head` の後は answered なので、`respond` は `Error::ResponseFinished`
です。head は既に線の上にあり、2 通目はそれが framing していない message です。

### 読む側: `accept_head` と `read_into`

```kizu
fn (self: &var Server) accept_head(io, allocator) -> Failure!Exchange
fn (self: &var Exchange) read_into(io, allocator, out, max) -> Failure!i64
```

`accept_head` は空行で止まります。body は接続に残り、`max_body_bytes` は
**掛かりません** —— body を保持していないので、上限を掛ける対象がありません。
`read_into` は `std::net::read_into` と同じ契約(追記した byte 数、0 は終端)で、
完結を判断する loop は caller が持ちます。

`Transfer-Encoding: chunked` は decode します —— `read_into` が返すのは body で、
size と CRLF ではありません。**0 は body の終わり**で、それは
`Content-Length` が尽きたときと chunked の terminator が来たときの両方です。
だから読む側の loop は framing を知らずに書けます。

```kizu
var got = try held.read_into(handle, allocator, &var piece, 4096);
while got > 0 {
    seen = seen + got;
    piece.clear();
    got = try held.read_into(handle, allocator, &var piece, 4096);
}
```

### 接続そのものは渡しません

`Exchange` は `TcpStream` を private に持ち、`write_all` / `read_into` /
`set_read_deadline` / `set_write_deadline` / `clear_*` だけを通します。

理由は head 読みの**残り**です。head を読むと packet 単位で読むので、次に来る
byte は既に `Exchange` の中にあります。socket を渡すと caller はその残りを
飛ばします。Go の `Hijacker` が接続と一緒に buffered reader を返すのも同じ理由です。

### deadline は phase のもの

`write_all` と `read_into` は deadline に触りません。触ると呼び出しごとに budget が
復活して、[`std::net` が避けた形](net.md#deadline)に戻ります。

- `respond_head` が write phase の deadline を 1 回置く
- `accept_head` が read phase の deadline を 1 回置く
- 1 つの phase より長く生きる caller(101 の後の protocol など)は
  `exchange.set_read_deadline(net::deadline_in_millis(n))` で自分で押し直す

## chunked な request

長さを事前に知らない client は `Content-Length` を書けないので、body を
`SIZE CRLF DATA CRLF` の列にして最後に size 0 を送ります。`accept` は decode して
`request.body` に入れ、`accept_head` は `read_into` が decode します。

chunk の extension(`3;name=value`)は読み飛ばします —— framing を変えるものが
無いので、理解できない値は message を拒否する理由になりません。terminator の
後ろの trailer は消費して捨てます(RFC 9110 が許す扱い)。trailer は head と
同じ形なので `max_head_bytes` が上限です。

### smuggling を作らないための規則

ここは HTTP/1 で request smuggling が住んでいる場所なので、規則は「1 つの
message が 2 つになる」経路を閉じるためのものです。

| 入力 | 答え |
| --- | --- |
| `Content-Length` と `Transfer-Encoding` の両方 | `Error::ConflictingFraming` (400) |
| `Transfer-Encoding` が `chunked` 以外(`gzip, chunked`、`identity`、list) | `Error::UnsupportedEncoding` (501) |
| size が hex でない / chunk の後ろが CRLF でない | `Error::MalformedChunk` (400) |
| decode 後の合計が `max_body_bytes` 超え | `Error::BodyTooLarge` (413) |

1 つ目が肝です。RFC 9112 は「encoding が勝つ」と言いますが、**前段の proxy が
逆に判断したときに 1 つの request が 2 つになります**。自分の長さについて 2 つの
ことを言う message は、どちらかを選ぶのではなく読みません。

`max_body_bytes` が掛かるのは `accept`(body を保持する)だけです。`accept_head`
では掛かりません —— 保持していないので、上限を掛ける対象がありません。

送る側の chunked は [`Framing::Chunked`](#chunked) です。

## keep-alive

```kizu
fn (self: &var Exchange) next(io, allocator) -> Failure!bool
```

`next` は同じ接続の次の request を読み、**あったかどうか**を返します。false は
接続が終わったこと —— この server がもう運ばないと言った、peer が同じことを
言った、あるいは peer が閉じた —— なので、それで終わる loop が接続の寿命です。

```kizu
var held = try server.accept(handle, allocator);
defer held.deinit(allocator);
var more = true;
while more {
    try answer(handle, allocator, &var held);
    more = try held.next(handle, allocator);
}
```

### 既定は 1 request です

`max_requests` の既定は **1** で、これは HTTP/1.1 の既定ではありません。

理由は protocol ではなくこの server にあります。並行 API が無い(SPEC §15)ので
1 接続ずつしか捌けません。つまり 2 通目のために接続を開けたままにする peer は、
**他の全員に対して**開けたままにしています。keep-alive が節約するのは handshake
1 回、払うのは service 全体です。並行性が入るまでは、これは caller が上げる数値
であって、黙って継ぐ既定ではありません。

`idle_millis` は request と request の間に許す静けさです。`next` はこれを deadline
にして次の head を待ちます。

### 接続が続く条件

`respond` / `respond_head` が head を書く時点で 3 つを同時に見ます。

| 条件 | 満たさないと |
| --- | --- |
| `served + 1 < max_requests` | この server がもう運ばない |
| framing が終わりを示せる(`Buffered` / `Length` / `Chunked`) | peer が body の終わりを close でしか知れない |
| request が許している | HTTP/1.1 は `Connection: close` が無ければ許す。HTTP/1.0 は `Connection: keep-alive` が要る |

どれか 1 つでも欠ければ head に `Connection: close` を書き、`next` は false を
返します。続く場合、HTTP/1.1 には何も書きません(persistent が既定)。HTTP/1.0 には
`Connection: keep-alive` を書きます。

### 終わっていない exchange は失敗です

`next` は次の 3 つを `Error::ExchangeUnfinished` で拒否します。false ではなく
失敗なのは、どれも caller の bug で、接続が黙って終わるとそれが隠れるからです。

- まだ答えていない
- 自分で書いていた body を `finish_body` で閉じていない
- `accept_head` で受けた request body をまだ読み切っていない

3 つ目のために、`accept_head` の後の `read_into` は **`Content-Length` を超えて
読みません**。超えて読むと、それは次の request です。

## URL と percent 符号化

```kizu
pub struct Url {
    pub scheme: std::string::String,
    pub host: std::string::String,
    pub port: i64,
    pub target: std::string::String,
}

pub fn parse_url(allocator: Allocator, text: []u8) -> std::http::Failure!std::http::Url
fn (self: &Url) authority(allocator, out: &var String) -> std::http::Failure!void
fn (self: Url) deinit(allocator: Allocator) -> void

pub fn percent_decode(allocator, text, out: &var String) -> std::http::Failure!void
pub fn form_decode(allocator, text, out: &var String) -> std::http::Failure!void
pub fn percent_encode(allocator, text, out: &var String) -> std::http::Failure!void
pub fn form_value(allocator, query, name, out: &var String)
    -> std::http::Failure!bool
```

`parse_url` が読むのは `http://host[:port][/target]` です。**`https` は
`Error::UnsupportedScheme` で拒否します** —— TLS が要り、std は持っていません。
暗号化を頼まれたものを黙って平文で送るのが、出してはいけない唯一の答えです。

`target` は path と query をまとめて持ちます。request line に載るのがそれ
そのものだからで、分けるときは server 側と同じ `path_of` / `query_of` を
使います。

`percent_decode` は `%XX` だけを戻します。`form_decode` はそれに加えて
`+` を空白にします —— query と form body の中だけの規則で、path の `+` は
`+` だからです。2 つの名前なのは 2 つの規則だからです。

`%` の後に 16 進数 2 桁が続かないのは `Error::InvalidEncoding` です。literal の
`%` として扱いません: 推測する decoder は攻撃者に操縦できる decoder です。

`form_value` は `a=1&b=2` から名前を探して復号した値を追記し、あったかどうかを
返します。key 自体も percent 符号化されうるので、比較は 1 byte ずつ復号しながら
行います —— lookup が答えるために確保しません。

## Routing

routing は**登録する表ではなく、聞く質問**です。

```kizu
pub struct Param {
    pub name: std::string::String,
    pub value: std::string::String,
}

pub fn params_new(allocator: Allocator) -> std::http::Params
fn (self: &Params) count() -> i64
fn (self: &Params) at(index: i64) -> ?&std::http::Param
fn (self: &Params) find(name: []u8) -> ?&std::http::Param
fn (self: &var Params) clear(allocator: Allocator) -> void
fn (self: Params) deinit(allocator: Allocator) -> void

pub fn route(
    allocator: Allocator,
    pattern: []u8,
    method: []u8,
    path: []u8,
    params: &var std::http::Params,
) -> std::http::Failure!bool
```

pattern の綴りは Go 1.22 の `ServeMux` です。

```text
GET /items/{id}          method を固定し、1 segment を id として取る
/items/{id}              method は問わない
/files/{rest...}         残り全部を、その中の `/` ごと rest として取る
/                        root だけに一致する
```

`{name}` は**空でない 1 segment**に一致します。`{name...}` は最後にしか置けません。
捕捉した値は percent 復号します —— segment に分けた**後**なので安全です。1 つの
segment の中の `%2F` はその segment の text の中の `/` であって、新しい segment
ではありません。

`params` が捕捉を持つのは答えが true のときだけです。呼ぶ前に clear するので、
1 つの `Params` が `if` の連鎖全体を通して使えます。

pattern が pattern でない(先頭の `/` が無い、capture 名が空、`{rest...}` が
最後でない)ときは `Error::InvalidPattern` です。

```kizu
if try http::route(allocator, "GET /items/{id}", method, path, &var params) {
    if params.find("id") |entry| {
        let id = entry.value.as_bytes();
        ...
    }
}
```

handler の登録簿は同じ質問を lookup の中で答えるだけで、選ばれた handler は
呼び出し元から見えません。`if` の連鎖なら、その path で何が動くかは path を
名指した場所に書いてあります。

## Client

```kizu
pub struct ClientResponse {
    pub status: i64,
    pub version: std::http::Version,
    pub reason: std::string::String,
    pub headers: std::http::Headers,
    pub body: std::string::String,
}

pub fn get(io, allocator, url) -> std::http::Failure!std::http::ClientResponse
pub fn post(io, allocator, url, content_type, body)
    -> std::http::Failure!std::http::ClientResponse
pub fn fetch(io, allocator, method, url, content_type, body)
    -> std::http::Failure!std::http::ClientResponse
pub fn fetch_with(io, allocator, method, url, content_type, body, limits)
    -> std::http::Failure!std::http::ClientResponse

pub fn write_request(io, allocator, stream, method, url: &Url, content_type, body)
    -> std::http::Failure!void
pub fn read_response_from(io, allocator, stream, method, limits)
    -> std::http::Failure!std::http::ClientResponse
pub fn parse_response_head(allocator, head, limits)
    -> std::http::Failure!std::http::ClientResponse
```

`get` は 1 呼び出しです —— connect、write、答えを丸ごと read、close。
`Connection: close` を送ったとおりに閉じます。

**connection pool も redirect 追従もありません。** どちらも policy(socket を
どれだけ保つか、別 host への 301 は同じ request か)で、library が選んだ policy は
呼ぶ側から見えない policy です。返るのは server が言ったことです。

`ClientResponse` は `Response` とは別の型です。片方は server がまだ組み立てて
いるもの、もう片方は client がもう持っているものだからです(原理 7)。

### 2 つに割れている理由

`get` は connect して write して **read で待つ**ので、同じプロセスの server に
対しては使えません —— 答えるはずの accept が同じ thread にあるからです。これは
並行性の欠落(SPEC §15)であって client の欠落ではありません。

だから `write_request` と `read_response_from` が公開されています。stream を
持つのは呼ぶ側で、`fetch` はその 2 つを connect で挟んだものです。これは同時に、
まだ無い層が刺さる継ぎ目でもあります —— TLS も proxy も、client ではなく stream を
所有します。

### 答えの framing

`Content-Length` があればその長さちょうど。無ければ **close が framing** です ——
`Connection: close` を送ったので、end of stream が body の終わりです。推測では
なく、protocol がそう言っています。

`Transfer-Encoding` のある答えは `Error::UnsupportedEncoding` です。HEAD の答えと
1xx / 204 / 304 は body を取りません。

## Cookie

```kizu
pub enum SameSite { Unset, Strict, Lax, None }

pub struct Cookie {
    pub name: std::string::String,
    pub value: std::string::String,
    pub path: std::string::String,
    pub domain: std::string::String,
    pub max_age: i64,
    pub secure: bool,
    pub http_only: bool,
    pub same_site: std::http::SameSite,
}

pub fn cookie_new(allocator, name, value) -> std::http::Failure!std::http::Cookie
fn (self: &var Cookie) set_path(allocator, path) -> std::http::Failure!void
fn (self: &var Cookie) set_domain(allocator, domain) -> std::http::Failure!void
fn (self: &Cookie) append_set_cookie(allocator, out: &var String)
    -> std::http::Failure!void
fn (self: Cookie) deinit(allocator) -> void

fn (self: &var Response) set_cookie(allocator, cookie: &Cookie)
    -> std::http::Failure!void
fn (self: &Request) cookie(allocator, name, out: &var String)
    -> std::http::Failure!bool
pub fn cookie_value(allocator, header, name, out: &var String)
    -> std::http::Failure!bool
```

`set_cookie` は **add** です(replace ではありません)—— browser は
`Set-Cookie` 1 行につき cookie 1 つを読むので、2 つ set すれば 2 行です。

**値は opaque な byte 列です。** percent 復号も base64 復号もしません。
cookie が運ぶのは、それを set したプログラムが入れたものだからです。

`max_age` は **負なら書きません**(session cookie)。**0 は書きます** ——
`Max-Age=0` が cookie を消す綴りだからです。`SameSite::Unset` は attribute を
書かず、`SameSite::None` は書きます。「言わない」と「None と言う」は違う
ことなので、値も違います。

`Expires` は持ちません。`Max-Age` が同じことを date 形式なしで言い、暦を
持たない std が date を正直に書けないためです。

cookie の名前は token、値は空白・カンマ・セミコロン・バックスラッシュ・
制御 byte を拒否します —— それが 2 つ目の cookie(や 2 つ目の header)を
密輸する道です。

## Content type

```kizu
pub fn content_type_for(path: []u8) -> []u8
```

拡張子だけを読みます。byte を覗いて推測(sniffing)しません —— それが誰かの
upload を HTML として配ってしまう道です。知らない拡張子は
`application/octet-stream` で、これは「型を誰も言っていない byte 列」という
正直な答えです。

directory 名の中のドットは拡張子ではなく、先頭のドットは名前です
(`.gitignore` は拡張子なし)。

## 今は話さないこと

- **pipelining**: 先読みした request は順に処理しますが、答えを重ねて送る
  ことはしません。1 つ答えてから次を読みます
- **trailer を読むこと**: terminator の後ろの trailer は消費して捨てます。
  `Trailer` header で予告されたものを request に足す仕組みはありません
- **compression**: `Content-Encoding` は素通しで、decode しません
- **HTTPS / TLS**、**HTTP/2**、**HTTP/3**
- **`Date` header**: std に暦がないので、書けないものを書きません
- **multipart / form-data**、**compression**
- **header folding**: RFC 9110 が protocol から外したもので、繋ぐのではなく拒否します

## エラー

`std::http::Error` は `MalformedRequest`、`HeadTooLarge`、`BodyTooLarge`、
`UnsupportedVersion`、`UnsupportedEncoding`、`Incomplete`、`InvalidStatus`、
`InvalidHeader`、`InvalidUrl`、`UnsupportedScheme`、`MalformedResponse`、
`ResponseFinished`、`InvalidEncoding`、`InvalidPattern`、`ResponseOverrun`、
`ResponseIncomplete`、`ExchangeUnfinished`、`ConflictingFraming`、
`MalformedChunk` を持ちます。

`MalformedChunk` が `MalformedRequest` / `MalformedResponse` と別なのは、
chunk の framing が**どちら向きにもある**ものだからです。同じ decoder が
request の body と response の body の両方を読みます。

`BodyTooLarge` と `ResponseOverrun` は似て非なるものです。前者は request 側 ——
この server が決めた上限を入力が超えたので、入力を拒否して回復します。後者は
response 側 —— message が自分の framing と矛盾しており、回復するものがないので
接続に届く前に拒否します(原理 7)。

`std::http::Failure` はその和 —— `Error or std::net::Error or std::mem::Error` ——
です。どれも変換されないので、`match` した caller はどの層が拒否したかを見ます。
