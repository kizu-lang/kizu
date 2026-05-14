# ADR-0029: active work は GitHub Issues で管理する

Status: 採用

## 背景

Kizu v0.1 では、言語仕様、interpreter、checker、examples、diagnostics、performance baseline など、
複数の作業を並行して扱う必要がある。

Markdown の TODO は履歴として読みやすいが、担当、議論、進捗、完了状態、関連 commit の追跡には弱い。

## 決定

active work は GitHub Issues で管理する。

Markdown は次の用途に限定する。

- `SPEC.md`: 言語仕様
- `docs/adr/`: 採用した設計判断
- `PHASES.md`: 過去フェーズの索引
- `docs/phases/`: 過去フェーズの履歴と設計メモ

新しい TODO、受け入れ条件、v0.1 完了作業は GitHub Issue として作る。

## Issue の書き方

Issue body は次を持つ。

```text
目的
範囲
受け入れ条件
範囲外
検証
```

v0.1 関連 issue には `v0.1` label を付ける。
設計判断が変わる場合だけ `SPEC.md` または ADR を更新する。

## 影響

- Codex goal は GitHub Issue を作業単位にする
- Markdown の checklist 更新は行わない
- 完了状態は Issue close で管理する
- 仕様と実装タスクの境界が分かりやすくなる
