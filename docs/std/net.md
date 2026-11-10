# std::net

`std::net` は `Poller` が待てる descriptor を持ちます —— TCP と、子プロセスの
pipe です。listener は接続を受け、stream は byte を運び、child は書いたものを
渡し、どれも owner なので、その descriptor は値が持つ `deinit` が解放します。
protocol の framing はこの上の層(`std::http`)が Kizu source で書きます。

子プロセスが `std::process` でなくここにあるのは、poller がその pipe に届く
必要があり、private field は自分の module からしか読めないためです。
`std::process` はこのプロセス自身について答え、接続と並べて出力を待つ child は
この module のものです。

`wasm32-browser` は socket capability を提供しません。`std::net` の socket operation と
`std::http` の network 経路に到達する program は build 時に target 非対応として
拒否します。address、URL、HTTP message の pure な parse / encode は portable です。
WebSocket や Fetch を TCP に見せる暗黙 adapter はありません。

`wasm32-wasi` の WASI Preview1 boundary にも、この API が要求する listen / connect /
poller capability はありません。socket descriptor を暗黙に渡したことにはせず、
これらの network operation に到達する program を同じく build 時に拒否します。

safe Kizu から descriptor は見えません。`TcpListener` / `TcpStream` / `Child` は
private field に持つので、socket や pipe に届く唯一の道はこの module が返した値です。

```kizu
pub fn tcp_listen(io: Io, address: []u8) -> std::net::Error!std::net::TcpListener
pub fn tcp_connect(io: Io, address: []u8) -> std::net::Error!std::net::TcpStream
pub fn tcp_connect_before(io: Io, address: []u8, at: i64)
    -> std::net::Error!std::net::TcpStream
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
fn (self: &var TcpStream) write_some(io: Io, bytes: []u8) -> std::net::Error!i64
fn (self: &var TcpStream) set_read_deadline(at: i64) -> void
fn (self: &var TcpStream) set_write_deadline(at: i64) -> void
fn (self: &var TcpStream) clear_read_deadline() -> void
fn (self: &var TcpStream) clear_write_deadline() -> void
fn (self: &var TcpStream) close_write() -> void
fn (self: TcpStream) deinit() -> void

pub fn poller_new(io: Io, capacity: i64) -> std::net::Error!std::net::Poller
fn (self: &var Poller) watch_stream(
    io: Io, stream: &TcpStream, token: i64, interest: std::net::Interest,
) -> std::net::Error!void
fn (self: &var Poller) watch_listener(
    io: Io, listener: &TcpListener, token: i64,
) -> std::net::Error!void
fn (self: &var Poller) forget(io: Io, stream: &TcpStream) -> std::net::Error!void
fn (self: &var Poller) forget_listener(io: Io, listener: &TcpListener) -> std::net::Error!void
fn (self: &var Poller) wait(io: Io, at: i64) -> std::net::Error!i64
fn (self: &Poller) ready(index: i64) -> ?std::net::Ready
fn (self: Poller) deinit() -> void

pub enum Stdio { Inherit, Null, Pipe }
pub fn command(allocator: Allocator, program: []u8) -> std::net::Error!std::net::Command
fn (self: &var Command) arg(allocator: Allocator, value: []u8) -> std::net::Error!void
fn (self: &var Command) set_stdin(stdio: std::net::Stdio) -> void
fn (self: &var Command) set_stdout(stdio: std::net::Stdio) -> void
fn (self: &var Command) set_stderr(stdio: std::net::Stdio) -> void
fn (self: &Command) spawn(io: Io) -> std::net::Error!std::net::Child
fn (self: Command) deinit(allocator: Allocator) -> void

fn (self: &var Child) read_stdout_ready(
    io: Io, allocator: Allocator, out: &var std::string::String, max: i64,
) -> std::net::Error!?i64
fn (self: &var Child) read_stderr_ready(
    io: Io, allocator: Allocator, out: &var std::string::String, max: i64,
) -> std::net::Error!?i64
fn (self: &var Child) read_stdout_into(
    io: Io, allocator: Allocator, out: &var std::string::String, max: i64,
) -> std::net::Error!i64
fn (self: &var Child) read_stderr_into(
    io: Io, allocator: Allocator, out: &var std::string::String, max: i64,
) -> std::net::Error!i64
fn (self: &var Child) write_stdin_all(io: Io, bytes: []u8) -> std::net::Error!void
fn (self: &var Child) write_stdin_some(io: Io, bytes: []u8) -> std::net::Error!i64
fn (self: &var Child) close_stdin() -> void
fn (self: &var Child) close_stdout() -> void
fn (self: &var Child) close_stderr() -> void
fn (self: &var Child) try_wait(io: Io) -> std::net::Error!?i64
fn (self: &var Child) wait(io: Io) -> std::net::Error!i64
fn (self: &var Child) kill(io: Io) -> std::net::Error!void
fn (self: Child) deinit() -> void

fn (self: &var Poller) watch_child_stdout(io: Io, child: &Child, token: i64) -> std::net::Error!void
fn (self: &var Poller) watch_child_stderr(io: Io, child: &Child, token: i64) -> std::net::Error!void
fn (self: &var Poller) watch_child_stdin(io: Io, child: &Child, token: i64) -> std::net::Error!void
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

`write_all` は全 byte を書くか失敗を返すかのどちらかです。部分書き込みを
caller が扱いたいときは [`write_some`](#write_all-と-write_some) です。

## deadline

deadline は **時点** です。duration ではありません。

```kizu
stream.set_read_deadline(net::deadline_in_millis(5000));
```

`deadline_in_millis(5000)` が「今から 5000ms 後の時点」を作り、setter がその時点を
受け取ります。Go の `conn.SetReadDeadline(time.Now().Add(d))` と同じ形です。

deadline は **待つ場所**まで決めます。descriptor は内部で non-blocking にしてあり、
待つのは syscall の中ではなく `poll` です。blocking な `send` は部分書き込みで
返らず、渡された分が全部入るまで kernel に留まるので、その中にいる間は deadline を
誰も読めません —— 読まない peer への大きな write が期限を無視するのはそれが理由
でした。`MSG_DONTWAIT` は Darwin の stream socket では無視されるので、答えは
`O_NONBLOCK` です。

**呼ぶ側からは何も変わりません。** deadline が無ければ `poll` が無期限に待ち、
`WouldBlock` は Kizu に出ません。`write_all` は今も「全部書くか失敗するか」です。

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

### connect の deadline

`tcp_connect` は host が待つだけ待ちます —— 何も答えない address に対して 1 分
前後です。`tcp_connect_before(io, address, at)` がそれを縛ります。

```kizu
// 203.0.113.1 は TEST-NET-3。どこにも routing されない
var stream = try net::tcp_connect_before(
    handle, "203.0.113.1:80", net::deadline_in_millis(300));
```

実測で 303 ms でした(縛らないと host 既定まで待ちます)。

deadline を stream に設定できないのはこの 1 箇所だけです —— まだ stream が
無いので、期限は connect の引数として渡すしかありません。

### listener の deadline

`set_accept_deadline` は `accept` に期限を与えます。定期的な仕事を挟みたい loop は
期限を設け、`TimedOut` をその合図として受け取り、次の期限を設けます。

## write_all と write_some

```kizu
fn (self: &var TcpStream) write_all(io, bytes) -> Error!void   // 全部書くか失敗
fn (self: &var TcpStream) write_some(io, bytes) -> Error!i64   // 今入る分だけ
```

`write_all` は**終わるまで返りません**。送らなければならない message には正しい
契約ですが、evented な loop では間違いです —— 読むのをやめた peer が、deadline
まで呼び出し元の thread を握ります。実測で 1 本の write が loop を 1 秒(deadline
いっぱい)止めました。

`write_some` は**申し出ます**。今入る分を送り、その数を返します。残りは caller が
持ったまま wait に戻ります。

**0 は error でも終端でもありません。**「今は書けない」で、いつ再試行するかは
poller が言います。同じ測定が 1000 ms → **0 ms** になりました。

`write_some` は待たないので、write deadline は掛かりません。待つのは poller の
仕事で、それを縛るのは `Poller.wait` の deadline です。

## Poller —— 多数を同時に待つ

blocking な read は 1 つの descriptor を待ちます。だから
`examples/http_server.kizu` の server は 1 接続ずつしか捌けません —— 1 本を
待っている間、他の声が聞こえないからです。

`Poller` は多数を待ち、**どれが喋ったか**を答えます。

```kizu
var poller = try net::poller_new(handle, 64);
defer poller.deinit();
try poller.watch_stream(handle, &conn, 7, net::Interest::Read);

let count = try poller.wait(handle, net::deadline_in_millis(1000));
var index = 0;
while index < count {
    if poller.ready(index) |event| {
        // event.token は登録時に渡した値。std::net は中身を見ません
    }
    index = index + 1;
}
```

`token` は caller のものです。この module は一度も読みません —— 「どの接続か」を
知っているのは caller だけなので、それを表す値を預かって返すだけです。

`wait` の `at` は **時点**で、[deadline](#deadline) と同じ種類の値です。

### Poller と `io::evented()` の違い

Poller は readiness と token を caller に返し、caller が接続表と state machine を
持ちます。`io::evented()` は普通の `read_into` の途中で worker を park し、TaskSet
が worker state を持ちます。どちらも 1 thread の多重化で、前者は state machine、
後者は接続ごとの直線 code を選ぶ API です。待ちと worker の生成はどちらも source
に見えます(原理 2)。

### 1 つの descriptor が 2 回来ることがあります

kqueue は filter ごとに 1 event を返し、epoll は 1 event に両方の bit を立てます。
read と write の両方が立った descriptor は、host によって 1 回とも 2 回とも来ます。
**どちらも間違いではない**ので、この module は host が言った通りを渡します。
渡された flag に従って動く caller はどちらでも正しく動きます。

`capacity` は 1 回の `wait` が報告できる上限です。溢れた分は失われません ——
まだ ready なので次の `wait` が即座に返します。

### 接続は collection に持てます

```kizu
var served = array::new<net::TcpStream>(allocator);
defer served.deinit(allocator);
// token は index。event から state を引くのは lookup ではなく添字
try poller.watch_stream(handle, stream, index, net::Interest::Read);
```

collection の element cleanup は「解放が allocator を名指すか」を comptime で
聞いてから呼びます(ADR-0142)。socket は descriptor を解放するので名指しません。

## 子プロセス —— 走っている間に出力を読む

`std::process::spawn_output8` は子が終わってから全部を返します。長い build の
出力を出た順に browser へ流すような program は、届いた分ずつ、しかも接続と
同じ `wait` で知る必要があります。`Child` はそのための型です。

```kizu
var command = try net::command(allocator, "make");
defer command.deinit(allocator);
try command.arg(allocator, "test");
command.set_stdout(net::Stdio::Pipe);
var child = try command.spawn(handle);
defer child.deinit();
try poller.watch_child_stdout(handle, &child, child_token);
// ready.token == child_token のとき
if try child.read_stdout_ready(handle, allocator, &var out, 4096) |got| {
    if got == 0 {
        child.close_stdout();
    }
}
```

- 3 つの stream はどれも `Inherit` で始まり、`Null` は捨て、`Pipe` はこの
  プロセスがもう一端を持ちます。pipe でない stream を読むと `Closed` です。
- `read_stdout_ready` / `read_stderr_ready` は `TcpStream.read_ready` と同じ
  契約です。null は「まだ無い」、0 は「子が閉じた」。
- **0 を読んだら閉じます。** 終端に達した pipe は readable のままなので、
  watch したままだと毎回の `wait` が即座に返ります。`close_*` はこのプロセスの
  端を閉じ、poller からも外れます。`close_stdin` は子に入力の終わりを見せます。
- `write_stdin_some` は `write_some` と同じく、0 が「今は書けない」です。
- 待つ形は `read_stdout_into` / `read_stderr_into` / `write_stdin_all` です。
  `TcpStream.read_into` / `write_all` と同じ契約で、evented な `Io` では呼んだ
  worker だけが park します。接続ごとの worker が子の出力を中継する server は
  この形で書きます。deadline はありません —— 止めるのは `kill` です。
- 起動できない program は error ではなく、子が 127 で終わります
  (`spawn_wait8` と同じ)。exec が成功したかは子の中でしか分からないためです。
- `try_wait` は待たずに終了 status か null を、`wait` は終わるまで待って
  status を返します。status は終了 code、signal で終わった場合は 128 + signal です。
  読まれない pipe に書いている子は先にそこで止まるので、終端まで読んでから
  `wait` します。`wait` は waitpid なので、evented な `Io` でも thread ごと
  待ちます。出力を終端まで読んだ後なら、待つのは直後の終了だけです。
- `kill` は SIGKILL を送ります。回収は caller の `wait` です。
- `deinit` は pipe を閉じ、まだ回収していない子は kill して回収します。
  owner を落としても、走り続ける子も zombie も残りません。
- Windows では `spawn` が `OperationFailed` を返します。`WSAPoll` は socket しか
  待てず、pipe を接続と同じ `wait` で待つというこの型の目的が果たせないためです。

## 並行性

現在の Kizu に thread はありません(SPEC §15)。Poller と evented worker は
**1 thread の多重化**です —— 多数の接続を扱えますが、2 つのことを同時には
実行しません。`std::http` は server-owned な `first` / `next` と worker-owned な
`accept_connection` + TaskSet の両方を持ちます。

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
InvalidLength       read の上限が 0 以下、command の program が空、引数に NUL がある
ReadFailed          read が失敗した
WriteFailed         write が失敗した
OutOfMemory         buffer を伸ばせなかった
LimitExceeded       上限を超えた
IoFailing           `std::testing::failing_io()` が渡された
OperationFailed     上のどれにも当たらない host の失敗
Closed              解放済みの descriptor、または pipe でない child の stream が使われた
```

`std::testing::failing_io()` を渡すと、host に触れずにすべての呼び出しが
`Error::IoFailing` を返します。I/O の失敗経路を test から通せるのはこれによります。
