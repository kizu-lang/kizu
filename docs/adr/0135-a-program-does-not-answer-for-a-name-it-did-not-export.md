# ADR-0135: プログラムは自分が export していない名前に答えない

## Status

Accepted.

## Context

Kizu の関数は LLVM の symbol にそのままの名前で降りていました。`fn send(...)` は
`@send`、`fn read(...)` は `@read` です。default の linkage は external なので、
その symbol は **linker が誰にでも差し出す答え**になります。

同じ実行ファイルには C runtime(`internal/native/runtime/runtime.c`)が並んで
link されていて、runtime は libc を名前で呼びます —— `send`、`recv`、`close`、
`rename`、`exit`。片方が定義し、片方が要求する名前が一致すれば、linker は
プログラムの定義を選びます。

つまり `std::net` を使うプログラムが `fn send(...)` を書くと、runtime の
socket write がその関数を呼びます。実際にそうなりました。

```text
* thread #1, stop reason = EXC_BAD_ACCESS (address=0x16f603ff0)
  frame #0: kizu_std_builtin_net_write_all_result + 12
->  0x10000b294 <+12>: str    x2, [sp, #0x20]
```

stack guard page です。`send()` が Kizu の `@send` を呼び、それが
`write_all` を呼び、それが `send()` を呼ぶ —— 診断のない無限再帰です。

これは新しい傷ではなく、`std::net` が**見えるようにした**傷です。`send` /
`recv` / `accept` / `listen` / `connect` / `close` は network を書く
プログラムが選ぶ名前そのもので、衝突は例外ではなく既定になります。

## Decision

**native では `main` 以外のすべての関数定義を internal linkage で出す。**

```llvm
define internal i64 @app__math__answer() { ... }
define i32 @main(i32 %kizu.argc, ptr %kizu.argv) { ... }
```

native Kizu program は `main` 以外を export しません。`extern "c" fn` は C の symbol を
**import** する綴りで、native の逆向きはありません(SPEC §12)。だから native で外の
世界が必要とする名前は `main` 1 つだけです。

`wasm32-browser` は source に `export "browser" fn` と書いた関数だけ、同名の stable ABI
wrapper を export します。通常の関数や `pub fn` を暗黙に出さず、実装関数そのものを
host ABI にしません。これは program が自分で名乗った名前にだけ答える同じ規則です。

internal linkage の symbol は link に参加しません。runtime.o の `send` は
libc の `send` に解決され、プログラムが自分のために選んだ名前は自分のもので
あり続けます。

`std::internal::builtin::*` primitive と `kizu_*` runtime entry point は
declaration であって定義ではないので、この規則の対象外です。両者が名前で
一致していること自体が contract です(`llvmFunctionName`)。

## 却下

| 案 | 却下理由 |
| --- | --- |
| 何もしない | 診断の無い無限再帰が残る。しかも衝突する名前は network を書けば普通に選ぶ名前で、「気をつける」で閉じられない |
| runtime.c 側で libc の別名(`__send` など)を使う | platform ごとに違う内部名に依存する。libc の呼び出し全部(`fopen`、`rename`、`exit`…)が対象で、増えるたびに漏れる |
| 利用者の関数名に prefix を付けて mangle する(`kizu_send`) | 生成 IR が読めなくなり、`--emit-llvm` と backtrace の名前が source と一致しなくなる。衝突は linkage の問題であって綴りの問題ではない |
| 衝突しうる名前を予約語にして拒否する | 予約表が platform の libc に依存する。利用者に「この名前は使えない」と課すのは原理 6 に反するし、表は必ず古くなる |
| runtime を static library にして symbol を隠す | link 順の問題を link 順で解こうとするだけで、プログラムの定義が優先される事実は変わらない |

## Consequences

- `fn send` / `fn read` / `fn close` を持つプログラムが正しく動く。
  `examples/net_shadowed_libc_name.kizu` がその 4 つを同時に宣言して回す
- module 配下の関数は元から名前空間を持つ(`app__math__answer`)ので、
  衝突していたのは package 根の関数だけだった。internal linkage は
  どちらにも同じ規則を与える
- 最適化ありの build では、呼ばれない internal 関数が落ちる。
  binary が小さくなる副次効果がある
- `--emit-llvm` の出力に `internal` が付く。LLVM corpus は再生成した
- WASI backend は `_start` だけを export する
- browser backend は `memory` / `kizu_start` と source に明記した
  `export "browser" fn` wrapper だけを export する
