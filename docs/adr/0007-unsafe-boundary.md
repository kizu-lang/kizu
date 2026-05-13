# ADR-0007: unsafe は低レベル操作の明示境界にする

Status: 提案

## 背景

Kizu はシステムプログラミング言語として、raw pointer、C ABI、unchecked operation などを扱える必要がある。
ただし unsafe が ownership / move / borrow の根本ルールを無効化すると、Kizu の安全性が壊れる。

## 決定

`unsafe` はコンパイラが証明しない低レベル操作を、人間が明示して使う境界にする。

unsafe code の memory safety obligation はプログラマが負う。
ただし、unsafe は compiler check を全面的に無効化するものではない。

unsafe は safe Kizu の所有権モデルを無効化しない。
unsafe が許すのは、コンパイラが証明しない低レベル操作の実行であり、
safe borrow / move / type の基本検査は継続する。

検討する構文:

```kizu
unsafe {
    ptr_write(p, 20)
}
```

```kizu
unsafe fn raw_write(p: ptr<u8>, len: usize) -> void {
    ...
}
```

unsafe で許す候補:

- raw pointer dereference
- pointer cast
- unchecked indexing
- C ABI call
- allocator primitive
- volatile / atomic primitive

unsafe でも許さないもの:

- moved value の再利用
- borrow escape
- safe borrow の lifetime extension
- 型不一致
- 初期化前の通常変数使用
- arbitrary AST rewrite

## 影響

- unsafe はレビュー可能な境界として書ける
- safe wrapper で unsafe を局所化できる
- Phase としては IR または backend 設計後に実装するのが自然
