# Phase 2: Interpreter

状態: 次に着手

## 目的

Go 製 interpreter を実装し、基本的な Kizu プログラムを `kizu run` で実行できるようにする。

この Phase は TDD で進める。

## TODO

### Examples

- [ ] `examples/functions.kizu` を追加する
- [ ] `examples/variables.kizu` を追加する
- [ ] `examples/arithmetic.kizu` を追加する
- [ ] `examples/if.kizu` を追加する
- [ ] `examples/while.kizu` を追加する

### Parser

- [ ] `if / else` を parse する
- [ ] `while` を parse する
- [ ] block statement を interpreter で扱いやすい形に整える
- [ ] return statement を function call から扱えるようにする

### Interpreter

- [ ] `print` builtin を実装する
- [ ] `int` を実行できる
- [ ] `bool` を実行できる
- [ ] `string` を実行できる
- [ ] `void` を扱える
- [ ] `let` を実行できる
- [ ] `var` を実行できる
- [ ] assignment を実行できる
- [ ] integer arithmetic を実行できる
- [ ] boolean comparison を実行できる
- [ ] function call を実行できる
- [ ] return を実行できる
- [ ] `if / else` を実行できる
- [ ] `while` を実行できる

### CLI

- [ ] `kizu run <file>` が interpreter を呼ぶ
- [ ] runtime error を短く読める形で表示する

### Tests

- [ ] interpreter の unit test を追加する
- [ ] example の期待出力テストを追加する
- [ ] CLI の薄い smoke test を追加する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] `go run ./cmd/kizu run examples/hello.kizu` が `hello, kizu` を出す
- [ ] `go run ./cmd/kizu run examples/functions.kizu` が `3` を出す
- [ ] `go run ./cmd/kizu run examples/variables.kizu` が `alice` と `31` を出す
- [ ] `go run ./cmd/kizu run examples/arithmetic.kizu` が `7` を出す
- [ ] `go run ./cmd/kizu run examples/if.kizu` が `adult` を出す
- [ ] `go run ./cmd/kizu run examples/while.kizu` が `0`, `1`, `2` を出す

## 範囲外

- struct
- enum
- type checker
- move checker
- borrow checker
- arena / handle
- generics
- match
- `kizu build`
- `kizu fmt`
- package manager
