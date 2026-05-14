# ADR-0005: 実装フェーズは Markdown の TODO と受け入れ条件で管理する

Status: 置換

置換先: [ADR-0029: active work は GitHub Issues で管理する](0029-issue-based-work-tracking.md)

## 背景

Kizu の実装範囲は広い。
仕様書駆動ツールを早期に導入すると、ツール運用が実装より重くなる可能性がある。

## 決定

実装フェーズは Markdown で管理する。

- `PHASES.md`: フェーズ一覧
- `docs/phases/phase-N.md`: 各 Phase の TODO、受け入れ条件、範囲外

## 影響

- Codex は Phase ごとの TODO を `[ ]` / `[x]` で更新できる
- goal は Phase の受け入れ条件を基準に作る
- `SPEC.md` は長期仕様、Phase 文書は実装計画として分ける

## 置換理由

v0.1 の作業範囲が広くなり、実装 TODO、受け入れ条件、議論、完了状態を Markdown だけで管理すると、
実際の開発タスクとの対応が追いにくくなった。

以後の active work は GitHub Issues を正とし、Markdown は仕様、ADR、履歴、設計メモに限定する。
