# ADR-0006: comptime は採用候補とし、macro は採用しない

Status: 採用

## 背景

Kizu は Zig に近い低レベル指向を目指す。
そのため `comptime` は有力な機能である。

一方、macro / proc macro / AST rewrite は仕様とビルドを複雑にし、Kizu の小ささと明快さを損なう可能性が高い。

## 決定

`comptime` は Phase 13 で限定的に採用する。
ただし macro は採用しない。

方針:

- `comptime` は型検査済みの Kizu コードとして扱う
- ownership / borrow check の対象にする
- AST や token stream を直接書き換える API は提供しない
- runtime borrow が comptime 境界を越えて escape することは禁止する
- 任意の filesystem access や build script 的副作用は許さない

v0.1 で採用するもの:

- `comptime <expr>`
- `fn f(comptime n: i64)`
- `comptime if <bool expr> { ... } else { ... }`

v0.1 の `comptime` expression は、リテラル、単項演算、二項演算に限定する。
runtime local value は `comptime` expression から参照できない。

## 影響

- `comptime` ありでも borrow checker は技術的に可能
- comptime の結果は型付きの値として扱う
- branch / call path selection は `comptime if` で表す
- macro-heavy な表現力は目指さない
- type-level comptime と top-level declaration generation は今後の phase で別途判断する
