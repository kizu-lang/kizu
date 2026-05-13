# Kizu

Kizu は、小さく、シンプルで、メモリ安全なプログラミング言語のプロトタイプです。

現時点では Go 製の初期実装で、Phase 3 として lexer、parser、AST、CLI、interpreter、type checker を実装しています。

## 実行方法

Nix flake で Go を含む開発環境に入れます。

```sh
nix develop
pre-commit install
```

開発環境内で以下を実行します。

```sh
go test ./...
golangci-lint run
pre-commit run --all-files
go run ./cmd/kizu parse examples/hello.kizu
go run ./cmd/kizu check examples/hello.kizu
go run ./cmd/kizu run examples/hello.kizu
```

または、shell に入らずに直接実行できます。

```sh
nix develop -c go test ./...
nix develop -c golangci-lint run
nix develop -c pre-commit run --all-files
nix develop -c go run ./cmd/kizu parse examples/hello.kizu
nix develop -c go run ./cmd/kizu check examples/hello.kizu
nix develop -c go run ./cmd/kizu run examples/functions.kizu
```

`run` は Phase 2 の interpreter を呼び出し、`print`、整数演算、変数、関数、`if`、`while` を実行できます。
`check` は Phase 3 の type checker を呼び出し、基本型、関数呼び出し、return、二項演算を静的に検査できます。

## 方針

詳細な言語仕様は [SPEC.md](SPEC.md) を、Codex 向けの実装方針は [AGENTS.md](AGENTS.md) を参照してください。
実装フェーズと TODO は [PHASES.md](PHASES.md) から追跡します。
設計判断の履歴は [docs/adr](docs/adr) で管理します。
性能評価の方針は [docs/perf.md](docs/perf.md) で管理します。
