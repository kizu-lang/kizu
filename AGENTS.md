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

selfhost 作業では、毎回 full bootstrap しないで検証段階を分けます。

- 既存 `target/selfhost/stage2/selfhost` がある場合、日常ループはまず `just selfhost-fast-gate` を使う。
- selfhost source を実装した checkpoint で `just selfhost-production-from-scratch` を通す。
- `just selfhost-oracle` は Go/Kizu oracle evidence が必要な PR や frontend parity 確認で明示的に使う。
- oracle の時間予算そのものを検証する場合だけ `just selfhost-oracle-budget` を使う。
- IR / backend / runtime / CLI contract の direct heavyweight gate は focused debugging 用で、通常ループに入れない。
- `KIZU_RUN_SELFHOST_RUN_TAPE=1` や `KIZU_RUN_SELFHOST_RUN_RENDER=1` の run tape/render gate は
  Go interpreter 上で selfhost 内部関数を実行する heavyweight debug path です。通常検証や待機ループに入れず、
  必要な blocker pin のために明示実行する場合だけ raw command で使ってください。
- interpreted selfhost gate を実行する場合は、`tail -4` などで出力を隠さず、ログファイルと時間予算を明示してください。
  完了待ち shell を複数積まないでください。
- heavyweight gate や oracle が遅い場合でも、hidden fallback や Go fallback で短縮しない。

## PR Workflow

作業は topic branch / Pull Request ベースで進めます。
PR には目的、主要変更、検証結果、対応 Issue を短く書いてください。
PR 作成後、subagent に「無駄な後方互換分岐が残っていないか、Issue を解決する本質的な実装か、より単純にできるか」を review させ、PR にコメントさせて対応してください。
