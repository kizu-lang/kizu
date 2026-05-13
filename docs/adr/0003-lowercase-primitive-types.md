# ADR-0003: 基本型は Zig 寄りの小文字表記にする

Status: 採用

## 背景

初期仕様では `Int` / `Bool` / `String` / `Unit` を使っていた。
しかし Kizu は低レベル寄りのシステムプログラミング言語を目指すため、Zig に近い primitive 表記の方が自然である。

## 決定

v0 の基本型は次にする。

```text
int
bool
string
void
```

`int` は v0 の簡易整数型とする。
将来の native backend では明示幅整数を追加する。

```text
i8 i16 i32 i64
u8 u16 u32 u64
usize isize
f32 f64
```

## 影響

- examples と parser tests は小文字型を使う
- `void` は戻り値省略時の型として扱う
- `string` は v0 の組み込み文字列型として扱う
- `String` / `Unit` は採用しない
