# ADR-0024: C ABI layout と native linking は明示指定に限定する

Status: 採用

## 背景

Kizu は C ABI と接続する必要がある。
Phase 12-14 で `extern "c" fn`、raw pointer、C header import は扱えるようになった。

ただし、C struct layout、link name、library linking、native runtime を曖昧に扱うと、
安全性とビルドの再現性が崩れる。

## 決定

C ABI layout と linking はすべて明示する。

最初の native linker 実装は、LLVM IR backend が扱える限定 subset だけを対象にする。
`kizu_print_*` runtime symbol は compiler が明示的に link する小さな shim で提供する。

## C function linking

`extern "c" fn` は C ABI call boundary を表す。symbol がどの library のものかは
宣言の上の `@link_lib("x")` / `@link_framework("X")` が名指し、linker が
それをどこで探すかは manifest の `[native]` が持つ(SPEC §3、§12.2)。

分けた理由: 名前は宣言を読む人が知りたい情報で、在処は build する machine の
事情だからだ。Rust は前者を `#[link(name)]` で source に、後者を build.rs の
`cargo:rustc-link-search` で持つ。Kizu は build script を持たないので、後者は
宣言的な manifest の表に置く。

却下した案:

| 案 | 却下理由 |
|---|---|
| 名前も manifest に書く(`libraries = ["m"]`) | どの extern がどの library に依存するかが source から消え、使わない宣言の library まで link する |
| 探索 directory も attribute に書く | source が machine の path を持ち、package を別の machine で build できない |
| header を読んで library を推測する | 依存が暗黙になる。ADR の主旨に反する |
| `@link_name("puts")` で symbol 名を付け替える | 必要になった例が無い。宣言名 = symbol で足りている |
| pkg-config 連携 | 外部 tool への依存。必要になったら `[native]` に足す |

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
kizu_main_error_message
```

runtime symbol は user symbol と衝突しないように予約する。

## LLVM lowering

`extern "c" fn` call は LLVM IR では `declare` と `call` に lower する方針にする。

例:

```llvm
declare i32 @puts(ptr)
%1 = call i32 @puts(ptr %s)
```

現状の LLVM backend は `kizu build --target native` で native executable を生成できる。
ただし、対象は LLVM lowering 済み subset と `kizu_print_*` runtime shim に限定する。
`extern "c" fn` の library selection、C struct layout、cross compilation 完全対応は後続で扱う。

## Smoke test 方針

native linking の最小 smoke test として次を置く。

```text
Kizu source -> LLVM IR -> object -> native executable -> run
```

対象は限定された C function call 1つにする。

## 影響

- C ABI 境界がコード上で見える
- 通常 struct と C layout struct を混同しない
- native linking を入れる前に cache key と runtime symbol の境界を決められる
- C++ ABI、package manager、cross compilation 完全対応は別 phase に分離する
