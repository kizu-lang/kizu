# ADR-0005: 実装フェーズは Markdown の TODO と受け入れ条件で管理する

Status: 採用

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
