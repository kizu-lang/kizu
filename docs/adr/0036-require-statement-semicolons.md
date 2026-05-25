# ADR-0036: statement semicolon を必須にする

## Status

Accepted.

## Context

Kizu v0.1 は explicit return を採用し、Rust の tail expression return は採用しない。
一方で、これまで statement の終端 `;` は実質 optional だった。

「あってもなくてもよい」構文は、parser、formatter、conformance corpus、将来の
self-host compiler の互換性を弱くする。Kizu は Zig 寄りの明示的なシステム
プログラミング言語を目指すため、statement の終端も明示する。

## Decision

simple statement の終端には `;` を必須にする。

対象:

```text
let / var declaration
assignment
return
break / continue
expression statement
match arm body の simple statement
```

block statement、`if`、`while`、`for`、`match`、`@unsafe`、`comptime if` 自体には
終端 `;` を付けない。

struct field、enum tag、union variant、match arm は list separator として `,` を使う。
simple statement と list separator の役割を混ぜない。

## Consequences

- formatter output は simple statement に `;` を含める
- examples と conformance corpus は semicolon 必須 syntax を正とする
- semicolon missing は parse error として扱う
- semicolon の有無で戻り値や expression value は変わらない
- self-host compiler は同じ semicolon rule を実装する必要がある
