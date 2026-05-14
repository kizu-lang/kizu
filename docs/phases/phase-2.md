# Phase 2: Interpreter

状態: 完了

## 目的

Go 製 interpreter を実装し、基本的な Kizu プログラムを `kizu run` で実行できるようにする。

この Phase は TDD で進める。

## TODO

### Examples

- [x] `examples/functions.kizu` を追加する
- [x] `examples/variables.kizu` を追加する
- [x] `examples/arithmetic.kizu` を追加する
- [x] `examples/if.kizu` を追加する
- [x] `examples/while.kizu` を追加する

### Parser

- [x] `if / else` を parse する
- [x] `while` を parse する
- [x] block statement を interpreter で扱いやすい形に整える
- [x] return statement を function call から扱えるようにする
- [x] Rust 風の tail expression return を実装しない

### Interpreter

- [x] `print` builtin を実装する
- [x] `i64` を実行できる
- [x] `bool` を実行できる
- [x] `[]const u8` を実行できる
- [x] `void` を扱える
- [x] `let` を実行できる
- [x] `var` を実行できる
- [x] assignment を実行できる
- [x] integer arithmetic を実行できる
- [x] boolean comparison を実行できる
- [x] function call を実行できる
- [x] return を実行できる
- [x] `void` 関数は return なしで終了できる
- [x] `if / else` を実行できる
- [x] `while` を実行できる

### CLI

- [x] `kizu run <file>` が interpreter を呼ぶ
- [x] runtime error を短く読める形で表示する

### Tests

- [x] interpreter の unit test を追加する
- [x] example の期待出力テストを追加する
- [x] CLI の薄い smoke test を追加する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `go run ./cmd/kizu run examples/hello.kizu` が `hello, kizu` を出す
- [x] `go run ./cmd/kizu run examples/functions.kizu` が `3` を出す
- [x] `go run ./cmd/kizu run examples/variables.kizu` が `alice` と `31` を出す
- [x] `go run ./cmd/kizu run examples/arithmetic.kizu` が `7` を出す
- [x] `go run ./cmd/kizu run examples/if.kizu` が `adult` を出す
- [x] `go run ./cmd/kizu run examples/while.kizu` が `0`, `1`, `2` を出す

## 範囲外

- struct
- enum
- union
- type checker
- move checker
- borrow checker
- arena / handle
- generics
- match
- `kizu build`
- `kizu fmt`
- package manager
