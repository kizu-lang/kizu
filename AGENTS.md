# AGENTS.md

このリポジトリでは、Kizu というメモリ安全なシステムプログラミング言語を実装します。

## プロジェクト目標

Kizu v0.1 は Go 製の interpreter-first language core です。
現在は v0.2 stdlib prototype を基準に、言語仕様と Go 実装の品質を固めます。

Kizu は Rust clone ではありません。

Rust 風の安全性のうち、次を限定して採用します。

- ownership
- move semantics
- local borrowing
- explicit lifetime annotation なし
- macro なし
- proc macro なし
- build script なし

## 最優先方針

作りすぎないこと。

基本の実行経路は、次が動く CLI です。

```sh
kizu run examples/hello.kizu
```

v0.1 の中心は以下です。

1. lexer
2. parser
3. AST
4. interpreter
5. type checker
6. move checker
7. borrow checker
8. Arena / Handle

v0.2 の中心は、最小 stdlib と tooling です。

- `std::mem`
- `std::array::Array<T>`
- `std::string::String`
- `std::map::Map<K, V>`
- `std::testing`
- explicit-Io `std::fs` / `std::path` / `std::io` / `std::process`
- `kizu test <file>`

active work は GitHub Issues を正として管理します。
Markdown の phase TODO 文書は使いません。

Kizu 固有の Codex skill は `.codex/skills/kizu-language-design/` を正として管理します。
言語設計、stdlib、memory-safety の判断では、この skill の方針を参照してください。

stdlib API の現状整理と Kizu 製 std への移行方針は `docs/stdlib.md` を参照してください。
新しい `std::...` builtin を追加する場合は、同じ変更で registry、examples、conformance を更新してください。

開発は branch / Pull Request ベースで進めます。
`main` への直接 commit / push は行わないでください。

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
internal/stdprim
internal/native
std
examples
tests
```

## 実装ルール

* 賢いコードより、単純なコードを優先する
* 大きな依存を追加しない
* parser と AST は読みやすく保つ
* 各 milestone にテストを追加する
* v0 では generics を本格実装しない
* `async fn` / `await` syntax は実装しない
* native executable generation は限定 subset の明示 build target として扱う
* macro は実装しない
* package manager はまだ実装しない
* `SPEC.md` と矛盾する構文や機能を勝手に追加しない
* 設計判断を変更する場合は `docs/adr/` に ADR を追加または更新する

## 品質ゲート

日常コマンドは `justfile` にまとめています。利用可能な recipe は `just --list` で確認してください。
特に build/cache 性能確認は `just perf`、`just perf-cache`、`just cache-smoke` を使ってください。

Kizu は target / build cache が無制限に肥大化する設計を避けます。compiler、backend、
stdlib、test、CI に関わる変更では、build time、cache size、no-op rebuild、CI 実行時間への
影響を常に確認し、悪化があり得る場合は `docs/perf.md` または対象 Issue の受け入れ条件に
測定方法を明記してください。

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

Go code comments は英語で書きます。
package / command comment と、すべての function / method comment は必須です。
コメントは処理の逐語説明ではなく、責務、前提、境界条件、失敗条件を説明してください。

## Git ワークフロー

`main` は常に merge 済みの安定状態として扱います。
実装、仕様変更、ドキュメント更新は必ず topic branch で行い、Pull Request で review / merge します。

基本手順:

```sh
git switch main
git pull --ff-only
git switch -c <type>/<short-name>
```

branch 名は次を基本にしてください。

```text
feat/<short-name>
fix/<short-name>
docs/<short-name>
refactor/<short-name>
test/<short-name>
```

作業後は次を実行します。

```sh
pre-commit run --all-files
git status --short
git commit -m "<message>"
git push -u origin <branch>
```

Pull Request には、目的、主要変更、検証結果、対応 Issue を短く書いてください。
PR が merge されるまで `main` へ直接 push してはいけません。

repository 側でも GitHub branch protection を有効にし、少なくとも `main` への direct push を禁止してください。

## CLI

必須コマンド:

```sh
kizu run <file>
kizu parse <file>
kizu check <file>
```

将来追加してよいコマンド:

```sh
kizu lint
```

## 完了条件

タスクは次を満たしたら完了です。

* code が build できる
* 関連テストが通る
* examples が壊れていない
* エラーが読める
* 挙動が `SPEC.md` に合っている

## Goal ワークフロー

Goal は GitHub Issue を正として進めてください。
Markdown の phase TODO 文書は削除済みであり、active TODO tracker ではありません。

* 対象 Issue の本文、受け入れ条件、コメントを実装前に確認する
* 実装は対象 Issue の範囲に絞る
* 関連テストと examples を追加または更新する
* 新しい TODO は Markdown ではなく GitHub Issue として作る
* 仕様や設計判断が変わる場合だけ `SPEC.md` または `docs/adr/` を更新する
* 明示要件と成果物を突き合わせて完了監査する
* `pre-commit run --all-files` を通す
* topic branch に変更を commit する
* branch を push して Pull Request を作る
* PR が merge されたら対応する Issue に結果をコメントし、完了したら close する

commit message は、Issue 完了なら次の形を基本にします。

```text
Complete #<issue-number> <short name>
```
