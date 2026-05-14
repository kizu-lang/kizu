# Phase XX: async policy

状態: 完了

## 目的

Kizu v0 では async を実装しない。

将来の非同期 I/O は `async fn` 中心ではなく、`Io` capability と `TaskGroup` で明示的に扱う方針にする。

## 方針

- v0 では async を実装しない
- 最初の async design では `async fn` / `await` を中心にしない
- I/O する関数は `Io` capability を受け取る
- 並行処理は `Task` / `TaskGroup` で明示する
- spawn された task は await または cancel される必要がある
- task は local borrow を捕まえられない
- task へ渡す non-copy value は move される
- 野良 task は許可しない

## TODO

- [x] `docs/phases/phase-xx-async.md` を整理する
- [x] `SPEC.md` に async policy を追加するか決める
- [x] `Io` capability の役割を定義する
- [x] `Task` / `TaskGroup` の所有権ルールを定義する
- [x] task が local borrow を捕まえられないルールを明文化する
- [x] v0 では実装しないことを明記する

## 受け入れ条件

- [x] async 方針が `SPEC.md` または ADR にある
- [x] `async fn` を最初の async design に入れない理由が明確
- [x] borrow checker と task の境界が説明されている

## 例

```kizu
fn read_config(io: Io, path: string) -> result<string> {
    return fs.read_to_string(io, path)
}
```

```kizu
fn main(io: Io) -> result<void> {
    let group = TaskGroup()
    let task = group.spawn(io, read_config, "config.toml")
    let text = task.await()
    print(text)
    return ok(void)
}
```

## 範囲外

- async runtime 実装
- event loop 実装
- networking stdlib
- compiler lowering
