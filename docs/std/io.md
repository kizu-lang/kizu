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
`LimitExceeded`、`StackProtectionFailed` を持ちます。

`wasm32-browser` では `blocking()` と stdout / stderr write を JavaScript host callback が
提供します。callback が拒否すれば `WriteFailed` です。stdin と evented `Io` は host が
提供しないため build 時に target 非対応として拒否します。byte ownership と adapter は
[`docs/wasm-browser.md`](../wasm-browser.md) にあります。

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
) -> std::io::Error!std::io::Future

fn (self: &var std::io::Future) await() -> void
fn (self: &var std::io::Future) cancel() -> void
fn (self: &std::io::Future) finished() -> bool
fn (self: std::io::Future) deinit(allocator: Allocator) -> void
```

`entry` は top-level function です。closure が無いので capture するものは無く、
`Io` と `Allocator` は引数で渡ります。作業対象は caller が **貸した** 1 つの値で、
worker はそれに書き、caller は Future を解放した後にそれを読みます。

`async` は**並行性の約束ではありません**。`blocking()` の `async` は worker を
その場で走らせ、終わった Future を返します。走らせるものが他に無い実装にとって
それが正直な答えで、差が出るのは待つときだけです。

evented worker の stack は 256 KiB で、`async` が allocator から一度だけ確保します。
最終的な frame size は backend が決めるため、caller はその実装依存の数字を渡しません。
stack は worker の実行中には伸びません。

確保する 1 block は usable stack に alignment と guard 用の 2 page を足した
大きさです。usable stack の直下を読み書き不可にし、native Kizu 関数の
stack probe で大きな frame もその guard を飛び越さないようにします。guard を
設定できなければ `async` / `spawn` は `StackProtectionFailed` を返し、
実行中に overflow すれば cleanup は走らず process が停止します。

`await` は loop を回して worker が終わるまで待ちます。同じ loop に居る他の
worker も進みます —— 1 回の turn は準備できたものを全て起こすので、待ちが
自分以外のためにも働きます。

`Future` は owner なので落とせません。解放は `deinit(allocator)` を通り、`deinit` は
cancel してから stack を同じ allocator へ返します。`cancel` は worker に旗を立て、
park している待ちを `std::net::Error::Canceled` で起こします。worker は自分が書いた `catch` /
`defer` を通って戻るので、握っていたものは落ちません。旗を無視して待ち直す
worker は cancel できません —— context を見ない goroutine と同じです。

worker は `void` を返します。その下に `try` する caller は居ないので、報告は
貸された状態に書きます。

`Future` は貸された状態を排他的に借り、保持する Io と allocator には shared に
tie されます。どれかを先に解放することも、Future をその frame の外へ出すことも
できません。`examples/negative/io_future_escape.kizu`、
`io_future_state_released.kizu`、`io_future_loop_released.kizu`、
`io_future_names_another_allocator.kizu` がその境界です。

## 所有する worker と TaskSet

結果を caller が読み戻さない worker は、状態を借りる必要がありません。`TaskSet`
は N 個の worker と、各 worker へ move した状態を所有します。

```kizu
pub struct TaskSet
pub fn task_set_new(io: Io, allocator: Allocator)
    -> std::io::Error!std::io::TaskSet

pub fn spawn<A>(
    tasks: &var std::io::TaskSet,
    entry: fn(Io, Allocator, A) -> void,
    state: A,
) -> std::io::Error!void

fn (self: std::io::TaskSet) deinit(allocator: Allocator) -> void
```

`task_set_new` に渡した Io と allocator が全 worker に渡ります。TaskSet は両方を
保持するため、event loop や fixed-buffer allocator より長生きできません。
allocator は set、worker state、stack の確保に使われ、`deinit` でも同じものを
名指します。

`spawn` は `state` を worker へ move します。`A` は struct で、view(`[]u8` / borrow)
や別の `Io` / `Allocator` を含められません。worker は値渡しされた owner を通常の
Kizu code と同じように全経路で cleanup します。spawn 自体が失敗した場合は、move
される前の state を wrapper が cleanup します。

完了した worker の state と stack は TaskSet が回収します。`deinit` は残っている
worker を cancel し、各 worker が自分の `defer` を通って終わってから set を解放
します。個別の結果を読み戻すときは `Future`、接続のように渡したら終わりの仕事は
`TaskSet` を使います。どちらも thread ではなく、evented Io の待ちの間に 1 thread
上で交互に進みます。

実例は `examples/io_evented.kizu` と `examples/http_tasks.kizu` にあります。
