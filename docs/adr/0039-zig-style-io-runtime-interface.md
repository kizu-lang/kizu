# ADR-0039: Io runtime は Zig 0.16 寄りの選択式 interface にする

Status: 採用（`Io` interface 方針のみ。`TaskGroup` を前提にした節は ADR-0025 で撤回）

## 背景

Kizu は async / multi-threading を重要な言語特性として扱う。
ただし、Go のような hidden global runtime や、Rust のような `async fn` 中心の
function coloring は v0.1 の設計目標と合わない。

Zig 0.16 は I/O を `std.Io` interface として扱い、application 側が `Io`
instance を渡す。これにより同じ library code を threaded / evented / platform
backend などに接続できる。

Kizu もこの方向に寄せる。ただし Kizu は safe code の memory safety を最優先するため、
runtime を差し替えても ownership / borrow / task boundary の制約は弱めない。

## 決定

Kizu は hidden global async runtime を持たない。

I/O と async execution の入口は `Io` interface として明示する。
I/O を行う関数は `Io` を受け取り、task / future / group は `Io` に紐づく。

将来の標準 API では、`Io` implementation を明示的に選べるようにする。

```kizu
let io = std::io::blocking();
let io = std::io::evented();
```

失敗する implementation は test 用の道具なので `std::testing` に置く。Zig が
`std.testing.FailingAllocator` を、Go が `testing/iotest` を production module の
外に置いているのと同じ理由で、production の I/O 実装の一覧には入れない。

```kizu
let io = std::testing::failing_io();
```

platform-specific backend は後続で追加する。

```kizu
let io = std::io::uring();  // Linux
let io = std::io::kqueue(); // BSD / macOS
```

現時点で実装しているのは `std::io::blocking()` と
`std::testing::failing_io()` である。`std::io::threaded()` は ADR-0025 で撤回した。
thread 実行系が動いてから、意味のある実装と一緒に戻す。
`evented` / platform backend は後続で扱う。

## Safe Boundary

runtime implementation を切り替えても、safe Kizu の境界ルールは弱めない。

具体的な境界ルールは ADR-0025 で撤回した。並行 API を持たない今、境界を越える
値そのものが存在しないため、規則だけを先に固定し直すことはしない。thread 実行系が
動いた時点で、その上で書く。

固定して残すのは方針だけである。

- hidden global runtime を持たない
- `Io` は明示的に渡す。function coloring を作らない
- safe Kizu で data race を書ける API は採用しない

3 番目が Zig との差である。Zig は data race を型で防がない。Kizu は防ぐ。

## Runtime Implementation Candidates

`std::io` は v0.1 で次の implementation を持つ。

```text
std::io::blocking()  simple blocking I/O
std::fs::read_file   explicit-Io file read returning ![]u8
std::fs::write_file  explicit-Io file write returning !void
```

evented / platform backend は optional とする。

```text
std::io::evented()   event-loop or stackful coroutine backed I/O
std::io::uring()     Linux io_uring backend
std::io::kqueue()    kqueue backend
std::io::dispatch()  platform dispatch backend
```

v0.1 では `evented` / platform backend は実装しない。
v0.1 で固定するのは API shape と memory-safety contract である。

`std::fs` は hidden global runtime を持たない。`read_file` / `write_file` は必ず
`Io` capability を第1引数に取り、I/O failure は `!T` error として返す。
blocking I/O と task-based I/O の違いを、同じ API のどちらの呼び方で表すかは、
thread 実行系と一緒に決める（ADR-0025）。

## Task API Direction

当初この節は `std::task::Group(io)` と `group.spawn(...)` を v0.1 の実装対象として
定めた。ADR-0025 でこの API を撤回したので、具体的な形はここでは決めない。

残すのは接続方針だけである。task runtime implementation は `Io` が表し、
`io.async(...)` のように `Io` から detached-looking task を直接作る API は採らない。

## Cancellation

cancellation の具体的な意味論（`await` / `cancel` の順序規則、cleanup 境界との
関係、cancellation error の型）は ADR-0025 で撤回した。実 runtime と一緒に決める。

方針として残すのは「hidden background work を残さない」ことだけである。

## 非目標

v0.1 では次を実装しない。

- `async fn` syntax
- language-level `await` keyword
- hidden global event loop
- detached task
- real OS-thread scheduler
- evented runtime
- io_uring / kqueue backend
- blocking semantics for channel receive
- `select`

## 影響

- Kizu は Zig 0.16 に近い明示的な `Io` interface model を採用できる
- library code は `Io` を受け取るだけで runtime implementation を選べる
- safe Kizu の memory-safety rule は runtime implementation に依存しない
- 並行 API の形は thread 実行系が動いてから決める（ADR-0025）
