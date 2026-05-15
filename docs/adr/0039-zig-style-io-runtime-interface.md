# ADR-0039: Io runtime は Zig 0.16 寄りの選択式 interface にする

Status: 採用

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
let io = std::io::threaded();
let io = std::io::evented();
let io = std::io::failing();
```

platform-specific backend は後続で追加する。

```kizu
let io = std::io::uring();  // Linux
let io = std::io::kqueue(); // BSD / macOS
```

v0.1 interpreter は `std::io::blocking()`、`std::io::threaded()`、
`std::io::failing()` を実装する。
ただし `evented` / platform backend は後続で扱う。

## Safe Boundary

runtime implementation を切り替えても、safe Kizu の境界ルールは同じにする。

- detached task は許可しない
- task は `TaskGroup` の structured scope を越えて escape できない
- task は await または cancel されなければならない
- task / future / thread / channel boundary に local borrow を渡せない
- task / future / thread / channel boundary に mutable borrow を渡せない
- task / future / thread / channel boundary に raw pointer を渡せない
- non-copy value を boundary に渡す場合は move する
- shared mutable state は `std::sync` / `std::atomic` の明示型だけで扱う

これは Zig より制約が強い。
Kizu は低レベル制御を残しつつ、safe code の memory safety を優先する。

## Runtime Implementation Candidates

`std::io` は v0.1 で次の implementation を持つ。

```text
std::io::blocking()  simple blocking I/O
std::io::threaded()  thread-backed I/O and task execution
std::io::failing()   test implementation that supports no external I/O
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

## Task API Direction

Kizu の `TaskGroup` は structured concurrency の所有者である。

将来的には `Io` が task runtime implementation を表し、`TaskGroup` が structured
scope を所有する形に寄せる。

```kizu
let io = std::io::threaded();
let group = std::task::Group(io);
let task = group.spawn(load, "config.toml");
```

`io.async(...)` のように `Io` から detached-looking task を直接作る API は採用しない。
task creation は `TaskGroup` を通す。

v0.1 はこの API を実装する。
旧 `Io()` / `std::task::Group()` / `group.spawn(io, ...)` 形式は採用しない。

## Cancellation

task cancellation は cleanup 境界と一体で扱う。

- `cancel` は task resource を解放する操作でもある
- `await` と `cancel` は structured scope 内で完結する
- cancellation request が発生した場合の error は値として伝播する
- hidden background work は残さない

v0.1 では cancellation は task を完了扱いにする prototype でよい。
実 runtime 導入時に cancellation 用の typed error を標準化する。

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
- `Io` / `TaskGroup` / `Task<T>` は stdlib API として発展させる
- v0.1 interpreter は同期実行のまま、将来 runtime の仕様負債を増やさずに済む
