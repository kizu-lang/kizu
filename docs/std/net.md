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
pub fn deadline_in_millis(millis: i64) -> i64

fn (self: &var TcpListener) accept(io: Io) -> std::net::Error!std::net::TcpStream
fn (self: &TcpListener) local_port() -> std::net::Error!i64
fn (self: &var TcpListener) set_accept_deadline(at: i64) -> void
fn (self: &var TcpListener) clear_accept_deadline() -> void
fn (self: TcpListener) deinit() -> void

fn (self: &var TcpStream) read_into(
    io: Io,
    allocator: Allocator,
    out: &var std::string::String,
    max: i64,
) -> std::net::Error!i64
fn (self: &var TcpStream) write_all(io: Io, bytes: []u8) -> std::net::Error!void
fn (self: &var TcpStream) set_read_deadline(at: i64) -> void
fn (self: &var TcpStream) set_write_deadline(at: i64) -> void
fn (self: &var TcpStream) clear_read_deadline() -> void
fn (self: &var TcpStream) clear_write_deadline() -> void
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

## deadline

deadline は **時点** です。duration ではありません。

```kizu
stream.set_read_deadline(net::deadline_in_millis(5000));
```

`deadline_in_millis(5000)` が「今から 5000ms 後の時点」を作り、setter がその時点を
受け取ります。Go の `conn.SetReadDeadline(time.Now().Add(d))` と同じ形です。

**この 1 つの時点が、以後の read 全体を覆います。** 1 回の read ごとに配り直される
budget ではありません。この差が全部です —— 4 秒ごとに 1 byte 送る相手は
「read あたり 5 秒」の timeout に永久に引っかかりませんが、deadline は使い切ります。
`SO_RCVTIMEO` を使わず field に持っているのはこのためで、Rust std / Python / Java の
`set_read_timeout` / `settimeout` / `setSoTimeout` は前者、Kizu と Go は後者です。

覆うのが全体である以上、**1 本の接続で複数の message を扱うなら、間で押し直します**。
deadline は自分で更新しません。

`clear_read_deadline` で外すと、deadline が無い状態(初期値)に戻り、read は待ち
続けます。deadline を過ぎた後の呼び出しは待たずに `Error::TimedOut` を返します。

### duration を渡してしまったら

`set_read_deadline(5000)` は時点として読まれます。monotonic clock の 5000ms は
この host が起動して 5 秒後で、とうに過ぎているので、**最初の read が
`TimedOut` で落ちます**。静かに無期限になるより気付ける方向に倒してあります。

### listener の deadline

`set_accept_deadline` は `accept` に期限を与えます。定期的な仕事を挟みたい loop は
期限を設け、`TimedOut` をその合図として受け取り、次の期限を設けます。

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
TimedOut            deadline を過ぎた、または接続が時間内に確立しなかった
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
