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

## Io capability

I/O する関数は `Io` を受け取る。

```kizu
fn read_config(io: Io, path: string) -> !string {
    return fs.read_to_string(io, path)
}
```

`Io` を受け取る関数は外部世界に触る可能性がある。
`Io` を受け取らない関数は、基本的に local / pure な処理として読める。

## Task / TaskGroup

並行処理は `TaskGroup` を通して明示する。

```kizu
let group = TaskGroup()
let task = group.spawn(io, read_config, "config.toml")
let text = task.await()
```

spawn された task は await または cancel されなければならない。
TaskGroup を抜ける前に、全 task は完了または cancel される。

## Borrow boundary

spawn された task は local borrow を捕まえられない。

task へ渡す値は owned value または copy value でなければならない。
non-copy value を task に渡す場合、その値は move される。

```kizu
let name = "alice"
let task = group.spawn(io, print_name, name)
print(name) // error: moved into task
```

## 影響

- 隠れた async work を作らない
- borrow checker と task lifetime の複雑さを抑える
- cancellation と cleanup の境界を TaskGroup に寄せる
- v0.1 は interpreter 上の structured task model を実装対象にする
- OS thread、event loop、networking stdlib は別 phase に分離する
