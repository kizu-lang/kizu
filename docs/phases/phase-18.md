# Phase 18: !T / try error handling

状態: 完了

## 目的

Kizu のエラー処理を exception ではなく値として扱う。

## 方針

- v0.1 では `!T` を実装する
- `Result<T, E>` は採用せず、Zig に近い `!T` を使う
- error payload は標準の `[]const u8` message とする
- `option<T>` は型名として予約し、runtime helper は後続 phase に回す
- `try` は `!T` を返す関数内でのみ使える

## TODO

- [x] `option<T>` と `!T` の v0 表現を決める
- [x] generics 本格実装なしで扱う範囲を決める
- [x] `try` の構文と return propagation ルールを決める
- [x] error value の標準型を決める
- [x] type checker の診断を追加する
- [x] interpreter で最小実行できるようにする
- [x] IR lowering 方針を決める
- [x] examples を追加する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `!T` を返す関数を `try` で伝播できる
- [x] 戻り値が `!T` でない場所の `try` が error になる
- [x] error message が読める

## 構文

```kizu
fn parse() -> !i64 {
    return 1
}

fn main() -> !i64 {
    let value = try parse()
    return value + 1
}
```

## 範囲外

- exception
- stack unwinding
- full generics
- async error propagation
- `option<T>` runtime helpers
