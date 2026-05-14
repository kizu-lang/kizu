# ADR-0027: v0.1 は interpreter-first language core とする

Status: 採用

## 背景

Kizu の長期目標には、低レベルシステムプログラミング、native compile、
小さい build cache、短い build time、Rust と比較できる性能が含まれる。

一方で、これらをすべて最初の v0.1 完了条件に含めると、仕様固定、checker、
runtime、backend、stdlib、benchmark、self-hosting が同時に必要になり、release が終わらない。

## 決定

Kizu v0.1 は Go 製 interpreter による language core release とする。

SPEC に見えている言語表面は、実装量が現実的で安全性の設計を壊さない限り、
なるべく v0.1 に持ち込む。
そのため、Zig/C-style tag enum、tagged union、simple match は v0.1 の実装対象に含める。

v0.1 の主な目的は次の通り。

- 言語仕様を実装済み範囲へ絞る
- interpreter で仕様どおりに動かす
- type / move / borrow / arena / error union / comptime の安全性を検査する
- Zig/C-style tag enum / tagged union / match を interpreter で実行できるようにする
- `Io` capability / `TaskGroup` による structured task model を interpreter で扱う
- `contract` / `satisfy` / `&Dyn<Contract>` による明示抽象化を扱う
- examples と negative tests で仕様を固定する
- 将来の compiler / performance work の測定基盤を残す

## v0.1 に含めないもの

次は v0.1 の完了条件に含めない。

- self-hosting compiler
- Rust 同等以上の runtime performance guarantee
- native executable generation
- full LLVM backend
- full WASM backend
- full stdlib
- package manager
- `async fn` / `await` syntax
- OS thread / event loop / networking runtime
- Rust-style trait system

## backend の扱い

LLVM IR backend、WASM / WASI backend、C header import、build cache は experimental として残す。

これらは v0.1 の正ではない。
v0.1 の正は interpreter と static checks である。

backend が対応できない機能は、silent fallback ではなく明示 error または experimental limitation として扱う。

## 影響

- v0.1 の完了条件が現実的になる
- SPEC と実装の差分を小さくできる
- TODO なしの対象を v0.1 scope に限定できる
- native compiler と performance work を後続 milestone に分離できる
- 長期目標を捨てずに、まず言語の形を固定できる
