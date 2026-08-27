# std::net

`std::net` は TCP だけを持ちます。listener は接続を受け、stream は byte を運び、
どちらも owner なので、その descriptor は値が持つ `deinit` が解放します。
protocol の framing はこの上の層(`std::http`)が Kizu source で書きます。

safe Kizu から descriptor は見えません。`TcpListener` と `TcpStream` は private
field に持つので、socket に届く唯一の道はこの module が返した値です。

```kizu
pub fn tcp_listen(io: Io, address: []u8) -> std::net::Error!std::net::TcpListener
pub fn tcp_connect(io: Io, address: []u8) -> std::net::Error!std::net::TcpStream
pub fn parse_address(address: []u8) -> std::net::Error!std::net::Address

fn (self: &var TcpListener) accept(io: Io) -> std::net::Error!std::net::TcpStream
fn (self: &TcpListener) local_port() -> std::net::Error!i64
fn (self: TcpListener) deinit() -> void

fn (self: &var TcpStream) read_into(
    io: Io,
    allocator: Allocator,
    out: &var std::string::String,
    max: i64,
) -> std::net::Error!i64
fn (self: &var TcpStream) write_all(io: Io, bytes: []u8) -> std::net::Error!void
fn (self: TcpStream) deinit() -> void
```

## address

address は `host:port` です。IPv6 の host は bracket で囲みます(`[::1]:8080`)——
中の colon が区切りに読まれないためです。host が空なら `tcp_listen` は全 interface、
`tcp_connect` は loopback を指します。host の解決は `tcp_listen` / `tcp_connect` が
行い、`parse_address` は text を分けるだけです。

`Address` は `host: []u8` と `port: i64` を持ちます。`host` は引数の中を指す borrow
なので、address の text より長く持てません。

port 0 は「空いている port をくれ」という意味で、`local_port` がどれになったかを
答えます。test が他のプロセスと衝突しないのはこれによります。

## deinit は消費する

`TcpListener.deinit` と `TcpStream.deinit` は `self` を値で取ります。close の後に
stream が残らないので、**close 後の使用は型 error** です —— runtime が
「その descriptor は閉じている」と言うのではなく、書いた場所で拒否されます。
これは他の std container の `deinit` と同じ規則です。

## read_into

`read_into` は 1 回の read が返した byte を caller の `String` に追記し、その数を
返します。**0 は相手が閉じたこと**を意味します。stream には長さが無いので、
「message が完結したか」を判断する loop は protocol を知っている側が持ちます。

`max <= 0` は `Error::InvalidLength` です。max は 1 回の read の上限であり、
buffer 全体の上限ではありません —— 全体の上限は呼ぶ側が積算します
(`std::http` の request header 上限がその例)。

`write_all` は全 byte を書くか失敗を返すかのどちらかです。部分書き込みは
caller が扱う結果ではありません。

## 並行性

現在の Kizu に並行 API はありません(SPEC §15)。したがって
`listener.accept` は 1 接続ずつしか返せず、その接続を処理し終えるまで次の
caller は待ちます。listen backlog(128)がその待ち行列です。

これは Zig の `std.http.Server` が今日出荷している形と同じで、並行性は呼ぶ側が
持ち込みます。Kizu の呼ぶ側はまだ何も持ち込めないので、これは milestone であって
production server ではありません。

## エラー

```text
InvalidAddress      address の綴りが読めない、port が範囲外
AddressNotFound     host 名を解決できなかった
AddressInUse        その address は既に使われている
ConnectionRefused   相手が listen していない
ConnectionReset     接続が切れた
PermissionDenied    その port / address を開く権限が無い
TooManyOpenFiles    descriptor が尽きた
TimedOut            接続が時間内に確立しなかった
InvalidLength       read の上限が 0 以下
ReadFailed          read が失敗した
WriteFailed         write が失敗した
OutOfMemory         buffer を伸ばせなかった
LimitExceeded       上限を超えた
IoFailing           `std::testing::failing_io()` が渡された
OperationFailed     上のどれにも当たらない host の失敗
Closed              解放済みの descriptor が使われた
```

`std::testing::failing_io()` を渡すと、host に触れずにすべての呼び出しが
`Error::IoFailing` を返します。I/O の失敗経路を test から通せるのはこれによります。
