# ADR-0016: 明示 lifetime annotation は採用しない

Status: 置換

Superseded by: [ADR-0059: explicit lifetime syntax for borrowed views](0059-explicit-lifetime-syntax.md)

## 背景

Kizu は Rust の安全性の考え方を参考にするが、Rust のような lifetime programming は目指さない。
ユーザーに lifetime parameter を書かせると、言語の学習コストと型システムの複雑さが大きくなる。

一方、Kizu はシステムプログラミング言語を目指すため、長寿命の関係や C ABI との接続も必要になる。

## 決定

Kizu は明示 lifetime annotation を採用しない。

方針:

- borrow はローカル限定にする
- borrow を struct field に保存できない
- borrow を関数から返せない
- borrow を lexical block の外へ escape できない
- 長寿命の関係は `arena<T>` と `handle<T>` で表す
- raw pointer は `unsafe` 境界で扱う

## arena / handle

`handle<T>` は参照ではなく opaque ID とする。
`arena<T>.get(handle<T>)` はローカル borrow を返す。

handle は borrow より長生きしてよいが、対応する arena より長生きしてはいけない。

この制約は lifetime annotation ではなく、arena ownership と handle provenance の検査で扱う。

## unsafe / raw pointer

raw pointer は lifetime 管理の代替ではない。

`unsafe` 内でも次は禁止する。

- moved value の再利用
- borrow escape
- safe borrow の lifetime extension

raw pointer の dereference は unsafe 操作とし、safe borrow とは別の検査境界で扱う。

## 影響

- Phase 5 は local borrow checker に限定する
- Phase 6 は arena / handle の provenance 検査を扱う
- Phase 12 は raw pointer と unsafe 境界を扱う
- Rust 風の lifetime parameter は syntax として追加しない
