# ADR-0028: enum は Zig/C 寄りの tag enum にする

Status: 採用

## 背景

Rust の `enum` は payload を持てるため、tagged union / algebraic data type に近い。
Kizu は Zig 寄りの低レベル言語を目指すため、tag だけの enum と payload を持つ union を分ける。

## 決定

Kizu の `enum` は Zig/C 寄りの tag enum とする。

v0.1 の `enum` は payload を持たない。
値は enum 型に属する named tag であり、`Color::Red` のように参照する。

```kizu
enum Color {
    Red
    Green
    Blue
}

fn main() {
    let color = Color::Red;
}
```

payload を持つ sum type は `enum` では扱わない。
v0.1 では `union` として別機能で実装する。

```kizu
union Shape {
    Point;
    Circle(i64);
    Label([]const u8);
}
```

## match

v0.1 の `match` は simple enum value と tagged union value の分岐に限定する。
Rust-style の広い pattern matching ではなく、Zig `switch` 寄りの tag dispatch として扱う。

```kizu
match color {
    Red => print("red");
    Green => print("green");
    Blue => print("blue");
}
```

tagged union の payload binding は扱う。
duplicate arm、unknown tag、non-exhaustive match は compile error とする。

guard と多段 destructuring は v0.1 では扱わない。

v0.2 では issue #534 により wildcard pattern `_` を match fallback arm として
採用する。`_` arm は最後に 1 つだけ書ける。payload binding はできない。
`_` arm は明示されていない残りの tag を束ねるため exhaustive とみなすが、
明示 arm の duplicate / unknown tag 検査は維持する。

## 影響

- `enum` の概念を絞れる
- C ABI や Zig-style enum へ寄せやすい
- Rust-style payload enum と混同しにくい
- payload を持つ sum type は `union` として明示的に扱える
