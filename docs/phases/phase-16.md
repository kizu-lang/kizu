# Phase 16: low-level type system hardening

状態: 完了

## 目的

v0 checker の基本型検査から一段進めて、システムプログラミング言語として必要な型境界を明確にする。

## 方針

- 暗黙の numeric promotion は採用しない
- numeric conversion は `cast<T>(value)` で明示する
- raw pointer cast は `unsafe` 内に限定する
- `void` は戻り値なし型として継続し、`Unit` alias は導入しない
- type alias は v0.1 では導入しない

## TODO

- [x] numeric cast / conversion の構文とルールを決める
- [x] 暗黙変換しない範囲を再確認する
- [x] `void` / `unit` の表記方針を整理する
- [x] type alias を採用するか判断する
- [x] pointer cast を unsafe 境界でどう扱うか決める
- [x] nullable pointer と non-null pointer の診断を強化する
- [x] struct field type checking の仕様と実装状態を文書に反映する
- [x] 型エラーのメッセージを改善する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] cast / conversion policy が `SPEC.md` または ADR にある
- [x] 型 checker の test が低レベル型の境界を網羅する
- [x] unsafe が必要な型操作と safe な型操作が区別されている

## 構文

```kizu
let x = cast<i32>(1)
```

unsafe pointer cast:

```kizu
unsafe {
    let q = cast<ptr<u8>>(p)
}
```

## 範囲外

- full generics
- trait / contract system
- type-level comptime
- C struct layout の完全実装
