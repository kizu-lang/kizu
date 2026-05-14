# ADR-0024: C ABI layout と native linking は明示指定に限定する

Status: 採用

## 背景

Kizu は C ABI と接続する必要がある。
Phase 12-14 で `extern "c" fn`、raw pointer、C header import は扱えるようになった。

ただし、C struct layout、link name、library linking、native runtime を曖昧に扱うと、
安全性とビルドの再現性が崩れる。

## 決定

C ABI layout と linking はすべて明示する。

Phase 17 では actual native linker 実装は行わない。
先に、native 実行へ進むための境界を固定する。

## C function linking

`extern "c" fn` は C ABI call boundary を表す。
将来 link name や library が必要になった場合は、宣言に attribute を付ける。

検討する構文:

```kizu
@link_name("puts")
@link_lib("c")
extern "c" fn c_puts(s: ptr<const u8>) -> i32
```

v0.1 では attribute parser は実装しない。

## C layout

Kizu の通常 `struct` は C layout を約束しない。

C ABI と共有する layout には、将来 `extern struct` または `repr(c)` 相当を導入する。

検討する構文:

```kizu
extern struct Point {
    x: i32
    y: i32
}
```

または:

```kizu
@repr("c")
struct Point {
    x: i32
    y: i32
}
```

どちらを採用するかは、実装 phase で決める。
ただし、通常 struct を暗黙に C layout として扱うことは禁止する。

## Runtime symbols

compiler runtime が必要とする symbol は `kizu_` prefix を持つ。

例:

```text
kizu_print_string
kizu_print_int
kizu_print_bool
```

runtime symbol は user symbol と衝突しないように予約する。

## LLVM lowering

`extern "c" fn` call は LLVM IR では `declare` と `call` に lower する方針にする。

例:

```llvm
declare i32 @puts(ptr)
%1 = call i32 @puts(ptr %s)
```

現状の LLVM backend は `extern "c" fn` の native link 実行を完成させない。
native object / executable 生成は後続 phase で扱う。

## Smoke test 方針

actual native linking を実装する phase では、最小 smoke test として次を置く。

```text
Kizu source -> LLVM IR -> object -> native executable -> run
```

対象は限定された C function call 1つにする。

## 影響

- C ABI 境界がコード上で見える
- 通常 struct と C layout struct を混同しない
- native linking を入れる前に cache key と runtime symbol の境界を決められる
- C++ ABI、package manager、cross compilation 完全対応は別 phase に分離する
