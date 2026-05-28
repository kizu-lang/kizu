# ADR-0038: v0.1 から if expression を削除する

## Status

Superseded by ADR-0074.

ADR-0074 restores expression-position `if` with branch expressions that do not
use `;`, while keeping the enclosing simple statement semicolon required.

## Context

ADR-0037 では `if` expression を採用したが、Kizu は explicit `return`、
statement semicolon 必須、Rust 風 tail expression return 不採用を基本方針にしている。

`if` expression だけが branch 末尾の expression statement を値として扱うと、
statement と expression の境界が曖昧になり、parser、type checker、ownership checker、
interpreter、IR、self-host compiler の説明量が増える。

## Decision

Kizu v0.1 では `if` を statement 専用に戻す。

値を条件で選びたい場合は、関数から明示的に `return` するか、`var` binding に明示的に
代入する。

```kizu
fn level(ok: bool) -> i64 {
    if ok {
        return 1;
    }

    return 0;
}
```

```kizu
var level = 0;
if ok {
    level = 1;
}
```

## Consequences

- expression 位置の `if` は parse error になる。
- `IfExpr` AST node と、関連する type / ownership / runtime / IR 経路を削除する。
- `match` は引き続き statement として扱い、match arm の `return` は関数 return として扱う。
- 三項演算子は v0.1 では導入しない。
