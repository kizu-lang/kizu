# Phase 5: Local borrow checker

状態: 未着手

## 目的

明示 lifetime なしで、ローカル限定 borrow の基本ルールを検査する。

## TODO

- [ ] `borrow T` parameter を検査する
- [ ] borrow が値を move しないことを検査する
- [ ] borrow 中の値を move できないことを検査する
- [ ] borrow を関数から返せないことを検査する
- [ ] borrow を struct field に保存できないことを検査する
- [ ] mutable borrow conflict を検査する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] `examples/borrow.kizu` が通る
- [ ] borrow escape の例が期待どおり error になる

## 範囲外

- lifetime annotation
- non-local borrow
- unsafe pointer proof
