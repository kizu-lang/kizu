# ADR-0017: safe Kizu のメモリ安全性を保証する

Status: 採用

## 背景

Kizu はシステムプログラミング言語を目指す。
そのため raw pointer、C ABI、allocator primitive、unchecked operation などの低レベル機能が必要になる。

一方で、Kizu の最重要方針は safe code のメモリ安全性を守ることである。

## 決定

Kizu のメモリ安全性保証は safe Kizu に対して行う。

safe Kizu では次を保証する。

- use-after-move を許さない
- dangling safe borrow を許さない
- borrow escape を許さない
- mutable aliasing を許さない
- arena より長生きする handle 使用を許さない
- 別 arena の handle 使用を許さない
- 初期化前の通常変数使用を許さない

`@unsafe` Kizu では、memory safety obligation はプログラマが負う。

`@unsafe` capability が必要なもの:

- raw pointer dereference
- pointer cast
- C ABI call
- `@requires_unsafe() fn` call
- volatile primitive

unchecked indexing、allocator primitive、atomic primitive は safe Kizu の保証外に
置くが、v0.1 の初期 capability set には含めない。採用する場合は個別の
capability と安全規則を先に設計する。

ただし、`@unsafe` は compiler check を全面的に無効化するものではない。
`unsafe fn` は採用しない。呼び出し側に memory safety obligation を要求する
関数は `@requires_unsafe() fn` で宣言し、呼び出しは `@unsafe(unsafe_call)`
内でのみ許可する。

`@unsafe` 内でも次は引き続き error にする。

- type mismatch
- syntax error
- moved value の safe use
- safe borrow の escape
- safe borrow の lifetime extension

## 影響

- Phase 4/5/6 は safe Kizu のメモリ安全性を支える
- Phase 12 は `@unsafe` の境界と責務を明示する
- `@unsafe` を含む API は safe wrapper を提供できるが、安全性の根拠を明確にする必要がある
- Kizu のドキュメントでは「memory safe」は safe Kizu の保証として説明する
