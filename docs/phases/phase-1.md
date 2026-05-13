# Phase 1: Lexer / Parser / AST / CLI skeleton

状態: 完了

## 目的

Kizu の見た目を固定するために、`.kizu` ファイルを読み、tokenize / parse して、読みやすい AST 表現を出せるようにする。

## TODO

- [x] Go module を作成する
- [x] `cmd/kizu/main.go` を作成する
- [x] `kizu parse <file>` を追加する
- [x] `kizu run <file>` を stub として追加する
- [x] `kizu check <file>` を stub として追加する
- [x] token 定義を追加する
- [x] lexer を追加する
- [x] AST の最小構造を追加する
- [x] parser の最小実装を追加する
- [x] `examples/hello.kizu` を追加する
- [x] lexer/parser の基本テストを追加する
- [x] Nix flake を追加する
- [x] direnv を有効化する
- [x] pre-commit / lint / test の品質ゲートを追加する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `go run ./cmd/kizu parse examples/hello.kizu` が読みやすい AST 表現を出す
- [x] `go run ./cmd/kizu run examples/hello.kizu` がクラッシュしない
- [x] `go run ./cmd/kizu check examples/hello.kizu` がクラッシュしない

## 範囲外

- interpreter
- type checker
- move checker
- borrow checker
- struct / enum
- arena / handle
