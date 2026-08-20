# ADR-0033: borrow syntax は &T / &var T にする

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
fn update(value: &var T) -> void
fn view(value: &T) -> &T borrows value
```

意味:

- `T` は owned value として扱い、non-copy 型は move される
- `&T` は shared local borrow として扱い、値を move しない
- `&var T` は mutable local borrow として扱い、値を move しない
- `&var` argument は mutable local binding に限定する
- `&T` と `&var T` は同時に overlap できない
- `&var T` 同士も同じ値に対して overlap できない

`borrow T` keyword syntax は v0.1 から採用しない。

## 制約

ADR-0060 により、関数境界を越える borrowed return では `borrows <source>` を採用する。

`&T` / `&var T` は local borrow として扱う。borrow を struct field に保存する
モデルは v0.2 では採用しない。関数から返す場合は `-> &T borrows value` のように
戻り値の source を明示する。task / comptime / `@unsafe` 境界で safe borrow を延命
させることはできない。

## 影響

- parser / formatter / examples / conformance manifest は `&T` / `&var T` を使う
- local borrow checker は shared / mutable borrow conflict を検査する
- raw pointer は引き続き `ptr<T>` / `ptr<const T>` で扱い、`&T` とは別物にする
