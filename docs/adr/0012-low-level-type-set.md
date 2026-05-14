# ADR-0012: 低レベル型セットは Zig 寄りに広めに持つ

Status: 採用

## 背景

Kizu はシステムプログラミング言語を目指す。
Phase 3 の type checker では、v0 の簡易型だけでなく、将来の LLVM / C ABI / WASM を見据えた低レベル型セットが必要になる。

## 決定

低レベル型セットは Zig 寄りに広めに持つ。

整数型:

```text
i8
i16
i32
i64
u8
u16
u32
u64
usize
isize
```

浮動小数点型:

```text
f32
f64
```

その他:

```text
bool
void
```

`int` は残さない。
整数 literal は `i64` として扱い、幅を変える場合は `cast<T>(value)` で明示する。

## 影響

- Phase 3 の type checker は明示幅整数を扱える設計にする
- C ABI と LLVM IR の型対応を表現しやすくなる
- integer promotion は暗黙に行わない
- 型変換は明示的に扱う
