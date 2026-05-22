# ADR-0023: low-level type conversion は明示 cast に限定する

Status: 採用

## 背景

Kizu は低レベルに寄せたシステムプログラミング言語を目指す。
そのため、明示幅整数、raw pointer、C ABI といった境界を扱う必要がある。

一方で、暗黙の integer promotion や pointer conversion を許すと、Kizu の安全性とレビューしやすさが落ちる。

## 決定

Kizu v0.1 は暗黙変換を追加しない。

異なる numeric type の変換は `cast<T>(value)` で明示する。

```kizu
let x = cast<i32>(1);
```

safe code で許可する cast:

- numeric type から numeric type

unsafe が必要な cast:

- `ptr<T>` / `ptr<const T>` / `?ptr<T>` / `?ptr<const T>` の raw pointer 間 cast

禁止する cast:

- `[]u8` から numeric
- `bool` から numeric
- numeric から raw pointer
- raw pointer から numeric

`void` は Kizu v0.1 の戻り値なし型として使う。
`Unit` alias は導入しない。

type alias は v0.1 では導入しない。

## 影響

- 型変換はコード上で見える
- C ABI 境界で必要な危険な pointer cast は `unsafe` に閉じ込められる
- 暗黙変換によるビルド差分や診断の複雑化を避ける
- alias / typedef 的な機能は後続 phase で別途判断する
