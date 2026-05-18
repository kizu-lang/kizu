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
v0.1 の API は `std::io` と `std::task` の境界に置く。
裸の `Io()` / `TaskGroup()` constructor は採用しない。
`Io` runtime の選択式 interface については ADR-0039 に従う。

マルチスレッドと async は Kizu の重要な言語特性として扱う。
v0.1 では低レベル thread API を直接中心にせず、safe structured concurrency を先に固める。
ここで固めるのは標準ライブラリ API の形状と safe Kizu の checker rule であり、
実 OS thread、event loop、networking runtime は後続で扱う。

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
let io = std::io::blocking();
let group = std::task::Group(io);
let task = group.spawn(read_config, "config.toml");
let text = task.await();
```

spawn された task は await または cancel されなければならない。
TaskGroup を抜ける前に、全 task は完了または cancel される。
task は `TaskGroup` の structured scope を越えて escape できない。
`await` は task body の error を伝播する。
`cancel` は v0.1 では task の完了を待ち、結果または error を破棄する。

v0.1 で目指す標準 API 境界:

```text
std::task::Group          structured task scope
Task<T>                   awaited or canceled task result
std::task::Queue          deterministic deferred task queue
std::task::parallel_for   safe data parallelism
std::task::parallel_map   disjoint partition output
std::channel::Channel<T>  owned message passing
std::thread::scoped       scoped thread boundary
std::sync::Mutex<T>       explicit shared mutable state wrapper
std::atomic::Atomic<T>   seq_cst-only atomic primitive
Io                        explicit I/O capability
std::fs::read_file        explicit-Io file read returning ![]const u8
std::fs::write_file       explicit-Io file write returning !void
```

`std::thread::scoped`、`std::atomic::Atomic<T>`、`std::sync::Mutex<T>` は必要だが、
v0.1 では safe structured API の境界を固定するための API として扱う。
ユーザーに raw thread sharing を中心に書かせない。

`std::channel::Channel<T>` は owned message passing とする。`send(value)` は non-copy
value を move し、`recv()` は owned value を返す。borrow と raw pointer は safe Kizu
では channel boundary を越えられない。
v0.1 では empty receive は runtime error とし、blocking semantics と `select` は採用しない。

`std::fs` は hidden global runtime を持たない。I/O API は必ず `Io` capability を
受け取り、失敗は `!T` error として返す。v0.1 の最小 API は
`std::fs::read_file(io, path)` と `std::fs::write_file(io, path, bytes)` だけにする。
blocking / threaded の違いは呼び出し側が選んだ `Io` と `TaskGroup` で表す。

`std::task::parallel_for` は data-parallel API とする。disjoint output は
`std::task::partition_mut(init: i64, count: i64)` と
`std::task::parallel_map(io, partition: &mut Partition, start, end, worker)` に閉じ込める。
worker-local scratch は `std::task::LocalBuffer` のような trusted std API に閉じ込める。
collection / mutable slice への直接接続は v0.1 では行わず、ADR-0040 に従って
`std::mem` と `std::array::Array<T>` の仕様後に設計する。
v0.1 interpreter は逐次実行でもよいが、API と checker rule は実並行 runtime でも
維持できる形にする。

`std::sync::Mutex<T>` は explicit shared mutable state wrapper とする。
v0.1 では copy value だけを受け取り、API 形状を固定する。
guard / lock mutation semantics と non-copy payload は後続で固める。
raw pointer は Mutex に入れられない。

`std::atomic::Atomic<T>` は v0.1 では seq_cst-only とする。
対応する `T` は `bool` と `i64` に限定する。
memory order を細かく選ぶ API は、safe structured API が固まった後に追加する。

Rust の `Send` trait は採用しない。
v0.1 では boundary を越えられる型を checker rule として明示する。
copy primitive、enum、safe field だけを持つ owned struct / union は boundary を越えられる。
`Atomic<T>` は v0.1 atomic 対応型なら boundary を越えられる。
`Channel<T>` は `T` が boundary-safe な場合だけ boundary を越えられる。
local borrow、mutable borrow、raw pointer は safe Kizu では boundary を越えられない。
raw pointer を field / payload に含む struct / union も boundary を越えられない。
`arena<T>` / `handle<T>` / `Dyn<Contract>` / `Mutex<T>` / `Task<T>` は
v0.1 では boundary を越えられない。
arena / handle の thread-safe sharing は v0.1 では扱わない。

## Borrow boundary

spawn された task は local borrow を捕まえられない。

task へ渡す値は owned value または copy value でなければならない。
non-copy value を task に渡す場合、その値は move される。
safe Kizu では task 間で mutable state を暗黙共有できない。

```kizu
let name = "alice";
let task = group.spawn(print_name, name);
print(name); // error: moved into task
```

## 影響

- 隠れた async work を作らない
- borrow checker と task lifetime の複雑さを抑える
- cancellation と cleanup の境界を TaskGroup に寄せる
- v0.1 は interpreter 上の structured task model を実装対象にする
- v0.1 の `blocking` / `failing` spawn は同期評価であり、event loop を作らない
- v0.1 の `threaded` spawn は goroutine で実行し、await / cancel が完了を待つ
- v0.1 の目標に `std::task`、`std::channel`、`std::thread`、`std::sync`、
  `std::atomic`、safe data parallelism の API 形状と安全契約を含める
- 実並行 runtime を導入する場合も、owned/copy value だけを task 境界に渡す方針を維持する
- `std::io` と `std::task` の API 境界を v0.1 から使う
- hidden global runtime は持たず、`Io` implementation を明示的に渡す
- OS thread、event loop、networking runtime、atomic ordering の詳細 API は safe structured API の後に扱う
