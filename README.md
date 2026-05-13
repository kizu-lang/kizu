# Kizu

Kizu は、小さく、シンプルで、メモリ安全なプログラミング言語のプロトタイプです。

現時点では Go 製の初期実装で、Phase 1 として lexer、parser、AST、CLI を実装しています。

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
go run ./cmd/kizu run examples/hello.kizu
```

または、shell に入らずに直接実行できます。

```sh
nix develop -c go test ./...
nix develop -c golangci-lint run
nix develop -c pre-commit run --all-files
nix develop -c go run ./cmd/kizu parse examples/hello.kizu
```

`run` はまだ実行器を持たない stub です。入力ファイルを parse できることだけ確認します。

## 方針

詳細な言語仕様は [SPEC.md](SPEC.md) を、Codex 向けの実装方針は [AGENTS.md](AGENTS.md) を参照してください。
実装フェーズと TODO は [PHASES.md](PHASES.md) から追跡します。
設計判断の履歴は [docs/adr](docs/adr) で管理します。
性能評価の方針は [docs/perf.md](docs/perf.md) で管理します。
