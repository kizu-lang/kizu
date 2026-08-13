# AGENTS.md

Kizu はメモリ安全な systems programming language です。

## 最優先

基本の実行経路は `kizu run examples/hello.kizu`、必須 CLI は `run` / `parse` / `check` です。
言語の正しさは `examples/` と `cmd/kizu/conformance_test.go` が持ちます。
リポジトリ全体の構造とデータフローは `docs/architecture.md` を先に読んでください。

## 実装ルール

- 賢いコードより単純なコードを優先する。
- 大きな依存を追加しない。
- `SPEC.md` と矛盾する構文や機能を勝手に追加しない。
- parser / AST / checker / backend は読みやすく保つ。
- ファイルが 1000 行を超える場合、分割を検討し、関心が分離できていない可能性を疑う。
- ユーザー判断で仕様判断を変える場合だけ `SPEC.md` または `docs/adr/` を更新する。
- 実装は Go 一本(ADR-0082)。selfhost は削除済みで、言語が固まるまで作り直さない。

## 禁止事項

- テストを pass させるだけの場当たり的変更やハードコードを入れない。
- LLVM を文字列リテラルで書き下ろさない(ADR-0073)。ソースの形ごとの payload 型・
  関数名分岐・形状 template を作らない(ADR-0081 が示した失敗)。
- 第二実装を作らない。言語機能は Go 実装 1 つで完成させる(ADR-0082)。
- hidden fallback、Go fallback、削除条件のない互換分岐を入れない。
- 関数の内部形状や生成テキスト断片を grep で固定する**構造 pin を新規に追加しない**
  (ADR-0080)。検証は probe 差分・parity manifest・実行 golden で行う。
- `main` へ直接 commit / push しない。red な gate を含む変更を merge しない。
  WIP checkpoint は branch に置く。

## テストと性能

テスト実行時間は 120s 以内に収めることを目標にしてください。
遅くなったら profile、重複削除、アルゴリズム改善、不要な gate 分離で改善します。
並列化でごまかす改善は NG です。
commit 前は原則 `pre-commit run --all-files` を通してください。
`go test ./...` は pre-push hook にあり、commit 時ではなく push 時に走ります。


## PR Workflow

作業は topic branch / Pull Request ベースで進めます。
**commit の前に必ず停止し、ユーザーのコードチェックを受けてください。**
自動での commit / push / merge は、ユーザーがその変更のチェックを終えてから行います。
PR には目的、主要変更、検証結果、対応 Issue を短く書いてください。
