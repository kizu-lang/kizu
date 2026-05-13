# Phase 14: C header import

状態: 未着手

## 目的

C header から Kizu の extern 宣言を生成できるようにする。

## TODO

- [ ] header import の範囲を決める
- [ ] clang など外部 tool への依存方針を決める
- [ ] import した宣言の cache key を決める
- [ ] `kizu import-c-header <file>` のような CLI を検討する
- [ ] unsupported C feature の error 方針を決める

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] 小さな C header から extern fn を生成できる
- [ ] unsupported syntax が読める error になる

## 範囲外

- C preprocessor 完全互換
- C++ header import
- macro import
