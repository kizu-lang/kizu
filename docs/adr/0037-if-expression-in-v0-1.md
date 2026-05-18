# ADR-0037: v0.1 で if expression を採用する

## Status

Superseded by ADR-0038.

## Context

Zig の `if` は statement としても expression としても使える。
Kizu は explicit return と statement semicolon 必須を採用しているため、
Rust 風の tail expression return は採用しない。

一方で、条件に応じて値を選ぶ構文は systems code でも頻出する。
`if` expression を v0.1 で採用する場合、branch value の規則を明示しないと
semicolon の有無や block の最後の式が曖昧になる。

## Decision

Kizu v0.1 は `if` expression を採用する。

```kizu
let size = if debug {
    1;
} else {
    2;
};
```

規則:

- expression 位置の `if` は `else` を必須にする
- 各 branch block は最後に expression statement を持つ
- branch value は最後の expression statement の値
- 両 branch の value type は一致しなければならない
- branch 内の move は外側へ merge される
- function の戻り値は引き続き explicit `return` のみ
- function body の Rust 風 tail expression return は採用しない

## Consequences

- `if` statement と `if` expression は同じ keyword を使う
- parser は statement 位置では `IfStmt`、expression 位置では `IfExpr` を作る
- type checker は branch value type の一致を検査する
- ownership checker は branch 内の possible move を外側へ反映する
- interpreter と IR は branch value を phi-like value として扱う
- self-host compiler は v0.1 conformance として同じ規則を実装する
