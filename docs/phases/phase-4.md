# Phase 4: Move checker

状態: 未着手

## 目的

Kizu の ownership と move semantics の基本を検査する。

## TODO

- [ ] Copy 型を定義する
- [ ] non-copy 型を定義する
- [ ] assignment による move を検査する
- [ ] function argument への move を検査する
- [ ] use after move を検出する
- [ ] double move を検出する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] `examples/move_error.kizu` が期待どおり compile error になる
- [ ] Copy 型は move 後も使える

## 範囲外

- borrow checker
- arena / handle
- unsafe
