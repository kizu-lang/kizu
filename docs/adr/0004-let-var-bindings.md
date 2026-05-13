# ADR-0004: ローカル束縛は let / var を使う

Status: 採用

## 背景

Zig は `const` / `var` を使う。
一方、Kizu では ownership と move semantics を持つため、ローカル値の束縛としては `let` / `var` の方が意味が明確である。

`const` はコンパイル時定数や module-level constant と誤解されやすい。

## 決定

ローカル束縛は次にする。

```text
let: 再代入できない束縛
var: 再代入できる束縛
```

基本方針は `let` default、必要なときだけ `var`。

## 影響

- `let` / `var` の両方に move rule を適用する
- move 済み `let` は再代入できないので使用不能のまま
- move 済み `var` は新しい値を再代入すれば再び使用可能
- 将来 `var` が一度も再代入されない場合は lint または check で警告してよい
- `const` は将来の comptime constant 用に予約する余地を残す
