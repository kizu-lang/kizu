# Phase 3: Type checker

状態: 完了

## 目的

Kizu の基本型と関数呼び出しを静的に検査する。

## TODO

- [x] 型環境を設計する
- [x] `int` / `bool` / `string` / `void` を検査する
- [x] `int` は v0 の暫定整数型として扱う
- [x] `i8` / `i16` / `i32` / `i64` を検査する
- [x] `u8` / `u16` / `u32` / `u64` を検査する
- [x] `usize` / `isize` を検査する
- [x] `f32` / `f64` を検査する
- [x] 暗黙の integer promotion をしない
- [x] 未定義変数を検出する
- [x] 引数の数の不一致を検出する
- [x] 引数の型の不一致を検出する
- [x] 戻り値の型の不一致を検出する
- [x] non-void 関数に explicit return path があることを検査する
- [x] tail expression return を error にする
- [x] 不正な二項演算を検出する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `kizu check` が基本的な型エラーを検出できる
- [x] 正しい Phase 2 examples が `kizu check` を通る
- [x] non-void 関数の return 漏れが error になる

## 範囲外

- move checker
- borrow checker
- generics
- struct field type checking
