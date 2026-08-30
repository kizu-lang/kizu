# チュートリアル: Kizu で web server を書く

このチュートリアルは、Kizu で HTTP service を 1 つ書き切ります。作るのは
「訪問数を数える service」で、3 つの経路を持ちます。

```text
GET  /                        service の名乗り
POST /users/{name}/visits     訪問を 1 つ記録する
GET  /users/{name}            記録された数を返す
```

完成品は [`web-server/`](web-server/) にあります。動かすには:

```console
$ kizu run docs/tutorial/web-server
HTTP/1.1 200 OK
visits service
HTTP/1.1 201 Created
alice has 1
...
```

このサンプルは conformance の一部です —— 末尾の case block が
「これを実行すると何が出るか」を宣言していて、テストがそれを確かめます。
**動かなくなった tutorial は嘘をつく文書**なので、信用ではなく実行で守ります。

---

## 0. 先に知っておくこと

Kizu の server は **見える loop** です。handler の登録簿も callback も
ありません。

```kizu
var exchange = try server.accept(handle, allocator, 1048576);   // 1 接続、1 request
defer exchange.deinit(allocator);
try exchange.respond_text(handle, allocator, 200, "text/plain", "hello");
```

Go は `http.HandleFunc` で関数を渡せるから渡します。Zig は関数 pointer を渡せる
のに渡さず、`receiveHead` の pull にしています —— control flow が source に見えた
ままになる形だからです(`docs/principles.md` §2)。

**Kizu にも関数 pointer はあります**(`fn(i64) -> i64`、`SPEC.md` §5)。pull なのは
書けないからではなく、Zig と同じ判断をしたからです(ADR-0136)。closure は無いので、
handler が状態を捕捉することはどのみちできません。

もう 1 つ: **`accept` は 1 接続ずつです。** この tutorial の逐次 loop では、2 人目の
caller は 1 人目が答え終わるまで listen backlog で待ちます。多数を 1 thread で
捌くには、server が state を持つ `first` / `next` か、接続を worker へ渡す
`accept_connection` + `TaskSet` を使います(`docs/std/http.md`)。

複数 OS thread の parallelism はまだありません(`SPEC.md` §15)。TaskSet は I/O の
待ちで worker を切り替える 1 thread の concurrency で、同時に 2 本の CPU code を
走らせるものではありません。

---

## 1. listen する

```kizu
import std::http;
import std::io;
import std::mem;

pub fn main() -> http::Failure!void {
    let handle = io::blocking();
    let allocator = mem::page_allocator();
    var server = try http::listen(handle, allocator, "127.0.0.1:8080");
    defer server.deinit(allocator);
    return;
}
```

`Io` は外の世界に触る権限で、`Allocator` は確保の出どころです。どちらも
引数です —— Kizu に hidden global runtime はありません(`SPEC.md` §15.1)。

`http::listen` が返す `Server` は owner です。`deinit` が `self` を**値で**
取るので、close の後に server は残りません: close 後の使用は runtime の報告
ではなく**型 error** です。

address は `host:port` です。IPv6 の host は bracket で囲みます
(`[::1]:8080`)。**port 0** は「空いている port をくれ」で、
`server.local_port()` がどれになったかを答えます —— test が他のプロセスと
衝突しないのはこれによります。サンプルが port 0 を使っているのはこのためです。

---

## 2. 1 接続に答える

```kizu
fn serve_one(handle: Io, allocator: Allocator, server: &var http::Server)
    -> http::Failure!void
{
    var exchange = try server.accept(handle, allocator, 1048576);
    defer exchange.deinit(allocator);
    try exchange.respond_text(handle, allocator, 200, "text/plain", "hello");
    return;
}
```

`accept` は 1 接続を受け、request を 1 つ読み切って `Exchange` を返します。
`Exchange` は届いた request と、それに対して返す response の 2 つです。

**`serve_one` を独立した関数にしているのには理由があります。** response は
`Connection: close` を送るので、client にとって body の終わりは **close**
です。`exchange` の frame が終わって初めて `deinit` が socket を閉じるので、
loop の 1 周を関数にしておくと閉じる位置が明確になります。

request を読めなかったときは、`accept` が**その場で status を返してから**
失敗を返します。client は status を受け取る権利があり、caller は parse すら
していない request に対して status を送れないからです。

| 失敗 | status |
| --- | --- |
| `MalformedRequest` / `Incomplete` / `InvalidHeader` | 400 |
| `HeadTooLarge` | 431 |
| `BodyTooLarge` | 413 |
| `UnsupportedVersion` | 505 |
| `UnsupportedEncoding` | 501 |

---

## 3. request を読む

```kizu
let method = exchange.request.method.as_bytes();
let target = exchange.request.target.as_bytes();
let path = http::path_of(target);      // "/users/alice"
let query = http::query_of(target);    // "page=2"
```

method・target・header は **所有した copy** です。読み取り buffer は次の
message のために再利用されるので、そこを borrow していたら次の read で
dangling view になります。

path と query が field ではないのは、どちらも target の中の run だからです。
`String` への view はその field の borrow(ADR-0111)なので、`let` で束縛して
から使います。

`path_of` は decode しません。path segment の中の `%2F` は区切りではなく、
それを区切りに畳むのが path traversal の通り道だからです。

header は名前を大文字小文字を無視して引きます。

```kizu
if exchange.request.headers.find("content-type") |field| {
    let value = field.value.as_bytes();
    ...
}
```

---

## 4. routing

routing は **登録する表ではなく、聞く質問**です。

```kizu
if try http::route(allocator, "POST /users/{name}/visits", method, path, &var params) {
    if params.find("name") |entry| {
        let name = entry.value.as_bytes();
        ...
    }
}
```

pattern の綴りは Go 1.22 の `ServeMux` です。`{name}` は空でない 1 segment、
`{rest...}` は残り全部(その中の `/` ごと)で、最後にしか置けません。
捕捉した値は segment に分けた**後**に percent 復号します —— 1 つの segment の
中の `%2F` はその text の中の `/` であって、新しい segment ではありません。

`params` が捕捉を持つのは答えが true のときだけです。呼ぶ前に clear するので、
1 つの `Params` が `if` の連鎖全体を通して使えます。

サンプルの `route_request` はこの `if` の連鎖そのものです。handler の登録簿は
同じ質問を lookup の中で答えるだけで、選ばれた handler は呼び出し元から
見えません。

---

## 5. state を持つ

```kizu
pub struct Visits {
    counts: map::Map<[]u8, i64>,
}
```

**この tutorial は 1 接続ずつなので、同期は要りません。** TaskSet server に変える
なら、決定が要るのはまさにこの state です。worker 間で暗黙に共有せず、誰が所有し
どこへ結果を返すかを決めます —— 指させる場所に置いておくのは、そのためでも
あります。

state は `main` が持ち、loop の各周に `&var` で渡します。global はありません。

```kizu
var visits = service::visits_new(allocator);
defer visits.deinit(allocator);
```

`Map<[]u8, i64>` は key を所有します。`deinit` が値と key をまとめて解放し、
`defer` がその呼び出しを source に見せます。

---

## 6. 答えを組み立てる

`respond_text` は 1 呼び出しの答えです。細かく作るなら:

```kizu
try exchange.response.set_status(201);
try exchange.response.header(allocator, "Content-Type", "application/json");
try exchange.response.write(allocator, body);
try exchange.respond(handle, allocator);
```

response は**全部組み立ててから送ります**。socket に何も届かないのは `encode`
が走るまでで、それによって status line が body の実際の長さを持つ
Content-Length を運べます。代わりに chunked transfer encoding が要りません ——
それは今の実装が話せない framing です。

`Content-Length` / `Transfer-Encoding` / `Connection` は caller が set しても
落とします。message の実体から書くもので、矛盾する 2 つを並べて送るわけには
いきません。1xx / 204 / 304 は body を持たないので Content-Length も付きません。

`respond` の後は 2 度目が `Error::ResponseFinished` です。1 つの接続に
2 つ目の message は流れません。

### 読む借用と書く借用を分ける

サンプルの `answer` が 2 つの呼び出しに分かれているのは、この 1 点のためです。

```kizu
let status = try route_request(allocator, visits, &exchange.request, &var body);
let text = body.as_bytes();
try exchange.respond_text(handle, allocator, status, "text/plain", text);
```

routing は `exchange.request` を読み、答えるのは `exchange` を可変で取ります。
2 つの呼び出しに分けると、読みの borrow は 1 つ目の呼び出しで終わります。
1 つの関数に書くと borrow checker が拒否します —— そしてそれは正しい拒否です。

---

## 7. 上限を決める

```kizu
var limits = http::default_limits();
limits.max_head_bytes = 4096;
limits.max_headers = 32;
var server = try http::listen_with(handle, allocator, "127.0.0.1:8080", limits);
```

上限は **caller のもの**です。proxy の後ろの service と公開 internet の
service が同じ上限を欲しがるとは限らず、名指せない上限は誰にも上げられません。

既定は request head 8 KiB、header 64 個です。

**body の上限はここにありません。** `accept` の引数です。

```kizu
var exchange = try server.accept(handle, allocator, 65536);
```

body の大きさは endpoint が決めるものだからです —— upload と form が同じ数を
共有する理由がありません。Go も `MaxHeaderBytes` は server に置き、body の上限は
`MaxBytesReader` として handler に渡します。

上限は head **そのもの**に対して測ります —— 1 回の read がたまたま全部運んで
きても、上限を超えた head は超えた head です。

---

## 8. 時間の上限を決める

byte の上限だけでは、公開して置ける service になりません。

```console
$ printf 'GET / HTTP/1.1\r\n' | nc 127.0.0.1 8080 &
$ curl -m 5 http://127.0.0.1:8080/
$ echo $?
124
```

接続して黙るだけです。トラフィックも要らず、脆弱性も要りません。この tutorial の
逐次 loop では、その 1 本が返らない限り全員が待ちます。TaskSet server でも、期限が
無ければその接続は worker と state を永久に握ります。

`Limits` は時間も持ちます。

```kizu
var limits = http::default_limits();
limits.read_head_millis = 5000;    // head を読み切るまで
limits.read_body_millis = 15000;   // body を読み切るまで
limits.write_millis = 15000;       // 答えを書き切るまで
```

既定は 10s / 30s / 30s です。3 つに分かれているのは、**それぞれが自分の始まりで
自分の期限を作る**からです —— body が head の余りしか貰えないのはおかしいし、
書き始めの期限は `accept` ではなく `respond` から測るべきです。

期限が切れた request には `408 Request Timeout` が返り、server は次の accept へ
戻ります。

### deadline は budget ではない

ここが読み飛ばせないところです。この期限は**その phase 全体**を覆います。
1 回の read ごとに配り直されるものではありません。

4 秒ごとに 1 byte 送ってくる相手を考えてください。「read あたり 5 秒」の
timeout には永久に引っかかりません。read はいつも 4 秒で返るからです。
head 全体に 5 秒、なら 5 秒で終わります。この違いのために `std::net` は
deadline を **時点**として持っています(→ [ADR-0137](../adr/0137-a-deadline-is-a-moment-the-socket-owner-holds.md))。

0 は「期限を置かない」です。既に bound している proxy の後ろなら妥当な答えです
が、忘れた結果として 0 になることは無いようにしてあります。

---

## 9. 本物の loop

サンプルは決まった数の request を回して終わりますが、本物はこうです。

```kizu
pub fn main() -> Failure!void {
    let handle = io::blocking();
    let allocator = mem::page_allocator();
    var server = try http::listen(handle, allocator, "127.0.0.1:8080");
    defer server.deinit(allocator);
    var visits = service::visits_new(allocator);
    defer visits.deinit(allocator);
    while true {
        // 1 接続の失敗で service を落とさない。accept は既に client へ
        // status を返している。
        serve_one(handle, allocator, &var server, &var visits) catch continue;
    }
    return;
}
```

サンプルが client を同じプロセスに持つのは、外部 process に依存しない決定的な
example にするためです。逐次 loop で `http::get` を呼ぶと read が同じ execution
path の accept を待つので、tutorial は `wire` module で「接続する → 書く → server
に 1 回答えさせる → 読む」を順に回しています。

---

## 10. この tutorial では話さないこと

| 採らないもの | 何が起きるか |
| --- | --- |
| `accept_connection` + `TaskSet` | この tutorial は 1 接続ずつ。production shape は `examples/http_tasks.kizu` |
| 接続を跨ぐ idle 期限 | 1 接続 1 request なので、押し直す相手がまだいない |
| `connect` の期限 | `tcp_connect` は黒穴宛てに host の既定(~75s)まで固まる |
| keep-alive の既定 | 既定は最大 100 request / idle 5 秒。この逐次 tutorial は `exchange.next` を呼ばず、1 request で接続を返す |
| trailer を読むこと | terminator の後ろの trailer は消費して捨てる |
| compression | `Content-Encoding` は素通しで decode しない |
| TLS / HTTPS | `std::http::get` は `https` を `Error::UnsupportedScheme` で拒否する。暗号化を頼まれたものを平文で送らない |
| HTTP/2 / HTTP/3 | 無い |
| header folding | RFC 9110 が protocol から外したもので、繋ぐのではなく拒否する |

---

## 読む順

1. [`web-server/src/service/service.kizu`](web-server/src/service/service.kizu)
   —— service そのもの。state、router、handler
2. [`web-server/src/main.kizu`](web-server/src/main.kizu) —— loop と配線
3. [`web-server/src/wire/wire.kizu`](web-server/src/wire/wire.kizu)
   —— tutorial を 1 つのプログラムにするためだけの client

関連する例:

- [`examples/http_server.kizu`](../../examples/http_server.kizu) —— 最小の server
- [`examples/http_router.kizu`](../../examples/http_router.kizu) —— routing
- [`examples/http_static_file.kizu`](../../examples/http_static_file.kizu)
  —— `{rest...}` と `std::fs`
- [`examples/http_client.kizu`](../../examples/http_client.kizu) —— client の 2 つの半分
- [`examples/http_response.kizu`](../../examples/http_response.kizu) —— response の状態
- [`examples/http_timeout.kizu`](../../examples/http_timeout.kizu) —— 期限切れの head と 408
- [`examples/http_stream.kizu`](../../examples/http_stream.kizu) —— body を自分で書く
- [`examples/http_upload.kizu`](../../examples/http_upload.kizu) —— body を自分で読む
- [`examples/http_keep_alive.kizu`](../../examples/http_keep_alive.kizu) —— 1 接続で複数 request
- [`examples/net_deadline.kizu`](../../examples/net_deadline.kizu) —— deadline が budget ではないこと

API 一覧は [`docs/std/http.md`](../std/http.md) と
[`docs/std/net.md`](../std/net.md) です。
