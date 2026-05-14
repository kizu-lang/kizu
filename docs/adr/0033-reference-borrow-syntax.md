# ADR-0033: borrow syntax は &T / &mut T にする

Status: 採用

## 背景

Kizu v0.1 では当初 `borrow T` を local borrow parameter として使っていた。
ただし、`borrow` は関数 signature で長く、Rust 由来の ownership model と対応しづらい。

Kizu は Rust clone ではないが、shared borrow と mutable borrow の表記は Rust に寄せる方が、
systems programming language として読みやすく、既存の開発者にも理解しやすい。

## 決定

Kizu v0.1 の borrow syntax は次にする。

```kizu
fn show(value: &T) -> void
fn update(value: &mut T) -> void
```

意味:

- `T` は owned value として扱い、non-copy 型は move される
- `&T` は shared local borrow として扱い、値を move しない
- `&mut T` は mutable local borrow として扱い、値を move しない
- `&mut` argument は mutable local binding に限定する
- `&T` と `&mut T` は同時に overlap できない
- `&mut T` 同士も同じ値に対して overlap できない

`borrow T` keyword syntax は v0.1 から採用しない。

## 制約

Kizu は引き続き明示 lifetime annotation を採用しない。

`&T` / `&mut T` は local borrow に限定する。
borrow を struct field に保存したり、関数から返したり、task / comptime / unsafe 境界で
lifetime extension させたりすることはできない。

## 影響

- parser / formatter / examples / conformance manifest は `&T` / `&mut T` を使う
- local borrow checker は shared / mutable borrow conflict を検査する
- `Dyn<Contract>` の dynamic dispatch は `&Dyn<Contract>` に限定する
- raw pointer は引き続き `ptr<T>` / `ptr<const T>` で扱い、`&T` とは別物にする
