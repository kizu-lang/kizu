# std::io

`std::io` は標準入出力を明示的な `Io` capability 経由で扱います。書き込みと
読み込みは失敗し得るため、すべて error union を返します。

```kizu
pub fn blocking() -> Io

pub fn write_stdout(io: Io, bytes: &[]u8) -> std::io::Error!void
pub fn write_stdout_line(io: Io, bytes: &[]u8) -> std::io::Error!void
pub fn write_stderr(io: Io, bytes: &[]u8) -> std::io::Error!void
pub fn write_stderr_line(io: Io, bytes: &[]u8) -> std::io::Error!void

pub fn read_stdin(
    io: Io,
    allocator: Allocator,
    limit: std::mem::Limit,
) -> std::io::Error!std::string::String

pub fn read_stdin_into(
    io: Io,
    allocator: Allocator,
    out: &var std::string::String,
) -> std::io::Error!void
```

`write_stdout` と `write_stderr` は bytes をそのまま書きます。`*_line` は bytes の
後に改行を追加し、途中の書き込みを含む失敗をそのまま返します。formatting や
暗黙の allocation は行いません。

`read_stdin` は EOF まで読んだ bytes を caller-owned `String` として返します。
allocator と上限は caller が明示します。`read_stdin_into` は既存の `String` に
追記し、独自の storage を持ちません。

`std::io::Error` は `WriteFailed`、`ReadFailed`、`OutOfMemory`、`IoFailing`、
`LimitExceeded` を持ちます。

## evented な Io

`blocking()` は「戻ってこないこと」で待ちます。1 本の read が終わるまで、その
thread は他の何もできません。`evented` な `Io` は **thread を返して** 待ちます
——呼び出しはその場で止まり、descriptor が来たときに loop がそこへ戻ります
(ADR-0146)。

```kizu
pub struct Loop
pub fn loop_new() -> std::io::Error!std::io::Loop
pub fn evented(state: &var std::io::Loop) -> Io
fn (self: std::io::Loop) deinit() -> void
```

`Loop` は caller が持ちます。`evented` が返す `Io` はその `Loop` に **tie** され、
`mem::fixed_buffer` の allocator と同じ規則で frame を出られません —— capability は
何かに届く許可なので、届く先より長生きできません。

```kizu
var loop = try io::loop_new();
defer loop.deinit();
let handle = io::evented(&var loop);
```

この `handle` を渡された `std::net` / `std::http` の code は何も変わりません。
待ち方が違うだけで、`read_into` は同じ `read_into` です。

## async と Future

```kizu
pub struct Future
pub fn async<A>(
    io: Io,
    allocator: Allocator,
    entry: fn(Io, Allocator, &var A) -> void,
    state: &var A,
    stack_bytes: i64,
) -> std::io::Error!std::io::Future

fn (self: &var std::io::Future) await() -> void
fn (self: &var std::io::Future) cancel() -> void
fn (self: &std::io::Future) finished() -> bool
fn (self: std::io::Future) deinit() -> void
```

`entry` は top-level function です。closure が無いので capture するものは無く、
`Io` と `Allocator` は引数で渡ります。作業対象は caller が **貸した** 1 つの値で、
worker はそれに書き、caller は Future を解放した後にそれを読みます。

`async` は**並行性の約束ではありません**。`blocking()` の `async` は worker を
その場で走らせ、終わった Future を返します。走らせるものが他に無い実装にとって
それが正直な答えで、差が出るのは待つときだけです。

`await` は loop を回して worker が終わるまで待ちます。同じ loop に居る他の
worker も進みます —— 1 回の turn は準備できたものを全て起こすので、待ちが
自分以外のためにも働きます。

`Future` は owner なので落とせません。解放は `deinit` を通り、`deinit` は
cancel してから閉じます。`cancel` は worker に旗を立て、park している待ちを
`std::net::Error::Canceled` で起こします。worker は自分が書いた `catch` /
`defer` を通って戻るので、握っていたものは落ちません。旗を無視して待ち直す
worker は cancel できません —— context を見ない goroutine と同じです。

worker は `void` を返します。その下に `try` する caller は居ないので、報告は
貸された状態に書きます。

`Future` は貸された状態に tie されます。貸した値を先に解放することも、Future を
その frame の外へ出すこともできません —— `examples/negative/io_future_escape.kizu`
と `examples/negative/io_future_state_released.kizu` がその 2 つです。

実例は `examples/io_evented.kizu` にあります。
