# ADR-0008: C 親和性は ABI / FFI / layout / pointer で確保する

Status: 採用

## 背景

Kizu をシステムプログラミング言語にするなら、C と呼び合えることは重要である。
ただし C 互換構文や C preprocessor を持ち込むと、Kizu の単純さと安全性が損なわれる。

## 決定

C 親和性は次で確保する。

- `extern "c" fn`
- C ABI 指定
- raw pointer 型
- nullable pointer 型
- 明示幅整数

Phase 12 ではまず `extern "c" fn` と raw pointer 型を扱う。
Phase 14 では、限定された C function prototype から `extern "c" fn` を生成する。
struct layout、alignment、link name は後続 phase で扱う。

検討する構文:

```kizu
extern "c" fn puts(s: ptr<const u8>) -> i32
```

```kizu
extern struct Point {
    x: i32
    y: i32
}
```

採用しない方針:

- C preprocessor 互換
- C header の完全自動解釈
- 暗黙の integer promotion
- `void*` の暗黙変換
- 配列と pointer の暗黙変換
- null の暗黙許容

Phase 14 の header import は、clang / libclang に依存しない限定 parser とする。
対応するのは function prototype、C ABI primitive、単純な pointer だけである。
unsupported syntax は importer が読める error として返す。

## 影響

- C ABI call は `@unsafe(extern_call)` 境界に寄せる
- safe wrapper を書けるようにする
- `ptr<T>` は non-null、`?ptr<T>` は nullable pointer として検討する
