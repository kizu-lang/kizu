# ADR-0025: async は Io / TaskGroup で明示する

Status: 採用

## 背景

Kizu は memory-safe、simple、reviewable な言語を目指す。

Rust 風の `async fn` / `await` を最初から導入すると、関数に async/sync の色が付き、
borrow、cancellation、resource cleanup、compiler lowering が重くなる。

## 決定

Kizu v0.1 では `async fn` / `await` syntax は実装しない。

ただし、I/O と並行処理の境界は v0.1 から実装する。
最初の design では `async fn` を中心にしない。
I/O は `Io` capability として明示し、並行処理は `Task` / `TaskGroup` で明示する。
`Io` / `Task` / `TaskGroup` は v0.1 では interpreter builtin から始めるが、
v0.1 のうちに `std::io` と `std::task` の API 境界へ寄せる。

マルチスレッドと async は Kizu の重要な言語特性として扱う。
v0.1 では低レベル thread API を直接中心にせず、safe structured concurrency を先に固める。

## Io capability

I/O する関数は `Io` を受け取る。

```kizu
fn read_config(io: Io, path: []const u8) -> ![]const u8 {
    return std::fs::read_to_string(io, path);
}
```

`Io` を受け取る関数は外部世界に触る可能性がある。
`Io` を受け取らない関数は、基本的に local / pure な処理として読める。

## Task / TaskGroup

並行処理は `TaskGroup` を通して明示する。

```kizu
let group = std::task::Group();
let task = group.spawn(io, read_config, "config.toml");
let text = task.await();
```

spawn された task は await または cancel されなければならない。
TaskGroup を抜ける前に、全 task は完了または cancel される。
task は `TaskGroup` の structured scope を越えて escape できない。

v0.1 で目指す標準 API 境界:

```text
std::task::Group          structured task scope
std::task::Queue          deterministic deferred task queue
std::task::parallel_for   safe data parallelism
std::task::parallel_map   disjoint partition output
std::channel::Channel<T>  owned message passing
```

`std::thread::scoped`、`std::atomic::Atomic`、`std::sync::Mutex` は必要だが、
v0.1 では safe structured API の
実装基盤または実験的 API として扱う。ユーザーに最初から raw thread sharing を中心に書かせない。

`std::channel::Channel<T>` は owned message passing とする。`send(value)` は non-copy
value を move し、`recv()` は owned value を返す。borrow と raw pointer は safe Kizu
では channel boundary を越えられない。

`std::task::parallel_for` は data-parallel API とする。disjoint output は
`std::task::partition_mut(init: i64, count: i64)` と
`std::task::parallel_map(io, partition, start, end, worker)` に閉じ込める。
worker-local scratch は `std::task::LocalBuffer` のような trusted std API に閉じ込める。
v0.1 interpreter は逐次実行でもよいが、API と checker rule は実並行 runtime でも
維持できる形にする。

`std::atomic::Atomic` は v0.1 では seq_cst-only とする。memory order を細かく選ぶ API は、
safe structured API が固まった後に追加する。

## Borrow boundary

spawn された task は local borrow を捕まえられない。

task へ渡す値は owned value または copy value でなければならない。
non-copy value を task に渡す場合、その値は move される。
safe Kizu では task 間で mutable state を暗黙共有できない。

```kizu
let name = "alice";
let task = group.spawn(io, print_name, name);
print(name); // error: moved into task
```

## 影響

- 隠れた async work を作らない
- borrow checker と task lifetime の複雑さを抑える
- cancellation と cleanup の境界を TaskGroup に寄せる
- v0.1 は interpreter 上の structured task model を実装対象にする
- v0.1 interpreter の `spawn` は同期評価であり、OS thread や event loop を作らない
- v0.1 の目標に `std::task`、`std::channel`、safe data parallelism の最小 API を含める
- 実並行 runtime を導入する場合も、owned/copy value だけを task 境界に渡す方針を維持する
- 標準ライブラリ化するときは `std::io` と `std::task` の API に分ける
- OS thread、event loop、networking runtime、atomic ordering の詳細 API は safe structured API の後に扱う
