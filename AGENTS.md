# AGENTS.md

このリポジトリでは、Kizu という小さなメモリ安全言語を実装します。

## プロジェクト目標

Kizu v0.1 を Go 製の小さな interpreter として実装します。

Kizu は Rust clone ではありません。

Rust 風の安全性のうち、次だけを小さく採用します。

- ownership
- move semantics
- local borrowing
- explicit lifetime annotation なし
- macro なし
- proc macro なし
- build script なし

## 最優先方針

作りすぎないこと。

最初の目標は、次が動く CLI です。

```sh
kizu run examples/hello.kizu
```

その後、段階的に以下を実装します。

1. lexer
2. parser
3. AST
4. interpreter
5. type checker
6. move checker
7. borrow checker
8. Arena / Handle

## リポジトリ構成

次の構成を使ってください。

```text
cmd/kizu/main.go
internal/token
internal/lexer
internal/ast
internal/parser
internal/interp
internal/types
internal/ownership
examples
tests
```

## 実装ルール

* 賢いコードより、単純なコードを優先する
* 大きな依存を追加しない
* parser と AST は読みやすく保つ
* 各 milestone にテストを追加する
* v0 では generics を本格実装しない
* async は実装しない
* native code generation は実装しない
* macro は実装しない
* package manager はまだ実装しない
* `SPEC.md` と矛盾する構文や機能を勝手に追加しない
* 設計判断を変更する場合は `docs/adr/` に ADR を追加または更新する

## 品質ゲート

commit 前に次を通してください。

```sh
pre-commit run --all-files
```

pre-commit では次を実行します。

* `gofmt`
* `go test ./...`
* `golangci-lint run`

lint は、機械的な整形だけでなく、未使用コード、静的解析、複雑度、基本的な可読性ルールを検査します。
ただし、過度に主観的なスタイルルールは避け、保守性に直接効くものを優先します。

Go の compiler 実装では、次を守ります。

* 1 関数 70 行以内
* 1 関数 45 statement 以内
* 1 行 100 文字以内

## CLI

必須コマンド:

```sh
kizu run <file>
kizu parse <file>
kizu check <file>
```

将来追加してよいコマンド:

```sh
kizu fmt <file>
kizu test
```

## 完了条件

タスクは次を満たしたら完了です。

* code が build できる
* 関連テストが通る
* examples が壊れていない
* エラーが読める
* 挙動が `SPEC.md` に合っている
