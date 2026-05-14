# ADR-0028: enum は Zig/C 寄りの tag enum にする

Status: 採用

## 背景

Rust の `enum` は payload を持てるため、tagged union / algebraic data type に近い。
Kizu は Zig 寄りの低レベル言語を目指すため、tag だけの enum と payload を持つ union を分けたい。

## 決定

Kizu の `enum` は Zig/C 寄りの tag enum とする。

v0.1 の `enum` は payload を持たない。
値は enum 型に属する named tag であり、`Color.Red` のように参照する。

```kizu
enum Color {
    Red
    Green
    Blue
}

fn main() {
    let color = Color.Red
}
```

payload を持つ sum type は `enum` では扱わない。
将来必要になった場合は、`union` または `tagged union` として別機能で設計する。

## match

v0.1 の `match` は simple enum value の分岐に限定する。

```kizu
match color {
    Red => print("red")
    Green => print("green")
    Blue => print("blue")
}
```

payload pattern、guard、destructuring は v0.1 では扱わない。

## 影響

- `enum` の概念が小さく保たれる
- C ABI や Zig-style enum へ寄せやすい
- Rust-style payload enum と混同しにくい
- payload を持つ sum type は後続 phase で明示的に設計できる
