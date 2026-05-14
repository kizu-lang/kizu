# ADR-0031: 幅が曖昧な int を廃止する

Status: 採用

## 背景

Kizu は Zig 寄りの低レベルシステムプログラミング言語を目指す。
そのため、整数型の bit 幅が曖昧な `int` を残すと、ABI、overflow、IR lowering、
最適化、性能評価の前提が不明確になる。

## 決定

Kizu の source-level type として `int` は採用しない。
整数 literal のデフォルト型は `i64` とする。

幅を変えたい場合は、明示的に `i32` / `u32` などの型を使い、
literal から幅付き型へ渡すときは `cast<T>(value)` を書く。

```kizu
fn add(a: i64, b: i64) -> i64 {
    return a + b;
}

fn take_i32(x: i32) -> i32 {
    return x;
}

fn main() {
    print(take_i32(cast<i32>(1)));
}
```

## 影響

- `int` は type checker の既知型から削除する
- integer literal、comptime integer、IR const は `i64` になる
- C の `int` は C header import 上の入力としてのみ扱い、Kizu では `i32` に変換する
- 暗黙の integer promotion は引き続き行わない
- examples / tests / SPEC は `i64` を使う
