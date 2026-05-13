# Phase 4: Move checker

状態: 完了

## 目的

Kizu の ownership と move semantics の基本を検査する。

## TODO

- [x] Copy 型を定義する
- [x] non-copy 型を定義する
- [x] assignment による move を検査する
- [x] function argument への move を検査する
- [x] use after move を検出する
- [x] double move を検出する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `examples/move_error.kizu` が期待どおり compile error になる
- [x] Copy 型は move 後も使える

## 範囲外

- borrow checker
- arena / handle
- unsafe
