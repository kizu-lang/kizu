# AGENTS.md

Kizu はメモリ安全な systems programming language です。

## 最優先

基本の実行経路は `kizu run examples/hello.kizu`、必須 CLI は `run` / `parse` / `check` です。
リポジトリ全体の構造とデータフローは `docs/architecture.md` を先に読んでください。

## 実装ルール

- 賢いコードより単純なコードを優先する。
- 大きな依存を追加しない。
- `SPEC.md` と矛盾する構文や機能を勝手に追加しない。
- parser / AST / checker / backend は読みやすく保つ。
- ファイルが 1000 行を超える場合、分割を検討し、関心が分離できていない可能性を疑う。
- ユーザー判断で仕様判断を変える場合だけ `SPEC.md` または `docs/adr/` を更新する。

## 禁止事項

- テストを pass させるだけの場当たり的変更やハードコードを入れない。
- selfhost 実装で 静的コード生成に分岐する実装を増やさない。
- `backend.kizu` に静的 LLVM 文字列を積み増すだけの変更をしない。
- hidden fallback、Go fallback、削除条件のない互換分岐を入れない。
- `main` へ直接 commit / push しない。

## テストと性能

テスト実行時間は 120s 以内に収めることを目標にしてください。
遅くなったら profile、重複削除、アルゴリズム改善、不要な gate 分離で改善します。
並列化でごまかす改善は NG です。
commit 前は原則 `pre-commit run --all-files` を通してください。
`go test ./...` は pre-push hook にあり、commit 時ではなく push 時に走ります。

selfhost 作業では、毎回 full bootstrap しないで検証段階を分けます。

## PR Workflow

作業は topic branch / Pull Request ベースで進めます。
PR には目的、主要変更、検証結果、対応 Issue を短く書いてください。
