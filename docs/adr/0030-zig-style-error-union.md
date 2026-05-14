# ADR-0030: エラー処理は Zig 風の !T に寄せる

Status: 採用

## 背景

Kizu は低レベル寄りのシステムプログラミング言語を目指す。
Rust 風の `Result<T, E>` は表現力が高い一方、v0.1 の段階では full generics と
標準ライブラリ設計に強く依存する。

## 決定

Kizu v0.1 では `result<T>` / `ok(value)` を採用しない。
エラー処理は Zig に近い error union 構文として `!T` を使う。

```kizu
fn parse() -> !i64 {
    return 1
}

fn fail() -> !i64 {
    return error("bad")
}

fn main() -> !i64 {
    let value = try parse()
    return value + 1
}
```

`!T` 関数で `T` を返した場合は成功値として扱う。
失敗値は `error(message)` で明示的に作る。

## 影響

- Kizu の表層構文から `Result` / `result<T>` / `ok(value)` を外す
- `try` は `!T` のみを unwrap / propagate する
- error payload は v0.1 では `string` message に固定する
- full generics なしでエラー処理を実装できる
- 将来、error set や stdlib error 型を追加する余地は残す
