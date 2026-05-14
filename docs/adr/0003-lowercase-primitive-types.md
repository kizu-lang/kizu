# ADR-0003: 基本型は Zig 寄りの小文字表記にする

Status: 採用

## 背景

初期仕様では `Int` / `Bool` / `String` / `Unit` を使っていた。
しかし Kizu は低レベル寄りのシステムプログラミング言語を目指すため、Zig に近い primitive 表記の方が自然である。

## 決定

v0 の基本型は次にする。

```text
i64
bool
void
```

`i64` は整数 literal のデフォルト型とする。
`int` は幅が曖昧なため採用しない。

```text
i8 i16 i32 i64
u8 u16 u32 u64
usize isize
f32 f64
```

## 影響

- examples と parser tests は小文字型を使う
- `void` は戻り値省略時の型として扱う
- string literal は `[]const u8` として扱い、`string` primitive は採用しない
- `Int` / `String` / `Unit` は採用しない
