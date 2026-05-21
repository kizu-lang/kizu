# AGENTS.md

Kizu はメモリ安全な systems programming language です。
Rust clone ではなく、ownership / move semantics / local borrowing だけを限定して採用します。
explicit lifetime annotation、macro、proc macro、build script は v0 では扱いません。

## 最優先

Go compiler は薄く保ち、常に Kizu self-host compiler へ移せる実装を選んでください。
基本の実行経路は `kizu run examples/hello.kizu`、必須 CLI は `run` / `parse` / `check` です。
active work は GitHub Issues を正とし、Markdown の phase TODO は使いません。

## 実装ルール

- 賢いコードより単純なコードを優先する。
- 大きな依存を追加しない。
- `SPEC.md` と矛盾する構文や機能を勝手に追加しない。
- parser / AST / checker / backend は読みやすく保つ。
- ファイルが 1000 行を超える場合、分割を検討し、関心が分離できていない可能性を疑う。
- 新しい TODO は Markdown ではなく GitHub Issue として作る。
- 仕様判断を変える場合だけ `SPEC.md` または `docs/adr/` を更新する。

## 禁止事項

- テストを増やすだけの Issue は作らない。
- テストを pass させるだけの場当たり的変更やハードコードを入れない。
- selfhost 実装で source literal / fixture path / 静的コード生成に分岐する実装を増やさない。
- `backend.kizu` に静的 LLVM 文字列を積み増すだけの変更をしない。
- hidden fallback、Go fallback、削除条件のない互換分岐を入れない。
- `main` へ直接 commit / push しない。

## Selfhost Progress

selfhost の前進とは、次のいずれかです。

- CLI を実際の selfhost compiler component に通す。
- hardcoded dispatch / fallback / static artifact branch を削除する。
- real path に必要な stdlib / runtime / backend capability を実装する。

parity case 追加だけでは前進と見なしません。

## テストと性能

テスト実行時間は 120s 以内に収めることを目標にしてください。
遅くなったら profile、重複削除、アルゴリズム改善、不要な gate 分離で改善します。
並列化でごまかす改善は NG です。
commit 前は原則 `pre-commit run --all-files` を通してください。

## PR Workflow

作業は topic branch / Pull Request ベースで進めます。
PR には目的、主要変更、検証結果、対応 Issue を短く書いてください。
PR 作成後、subagent に「無駄な後方互換分岐が残っていないか、Issue を解決する本質的な実装か、より単純にできるか」を review させ、PR にコメントさせて対応してください。
