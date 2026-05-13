# Kizu 実装フェーズ

このファイルは、Kizu の実装状態を追跡するための入口です。

詳細な TODO と受け入れ条件は `docs/phases/` 配下でフェーズごとに管理します。

## フェーズ一覧

| Phase | 状態 | 内容 | 詳細 |
| --- | --- | --- | --- |
| Phase 1 | 完了 | Lexer / Parser / AST / CLI skeleton | [phase-1.md](docs/phases/phase-1.md) |
| Phase 2 | 完了 | Interpreter | [phase-2.md](docs/phases/phase-2.md) |
| Phase 3 | 完了 | Type checker | [phase-3.md](docs/phases/phase-3.md) |
| Phase 4 | 完了 | Move checker | [phase-4.md](docs/phases/phase-4.md) |
| Phase 5 | 完了 | Local borrow checker | [phase-5.md](docs/phases/phase-5.md) |
| Phase 6 | 完了 | arena / handle | [phase-6.md](docs/phases/phase-6.md) |
| Phase 7 | 未着手 | 予約 | [phase-7.md](docs/phases/phase-7.md) |
| Phase 8 | 完了 | typed SSA IR | [phase-8.md](docs/phases/phase-8.md) |
| Phase 9 | 完了 | LLVM IR backend | [phase-9.md](docs/phases/phase-9.md) |
| Phase 10 | 次に着手 | build cache / why-rebuild | [phase-10.md](docs/phases/phase-10.md) |
| Phase 11 | 未着手 | WASM / WASI backend | [phase-11.md](docs/phases/phase-11.md) |
| Phase 12 | 未着手 | unsafe / C ABI | [phase-12.md](docs/phases/phase-12.md) |
| Phase 13 | 未着手 | comptime | [phase-13.md](docs/phases/phase-13.md) |
| Phase 14 | 未着手 | C header import | [phase-14.md](docs/phases/phase-14.md) |

## 横断フェーズ

| 状態 | 内容 | 詳細 |
| --- | --- | --- |
| 未着手 | ビルド性能とキャッシュ評価 | [build-performance.md](docs/phases/build-performance.md) |

## 状態の意味

- `未着手`: まだ実装しない
- `次に着手`: 現在の goal 候補
- `進行中`: 実装中
- `完了`: 受け入れ条件を満たした
- `保留`: 仕様または実装方針の再検討が必要

## 更新ルール

- TODO は完了したら `[x]` にする
- 受け入れ条件がすべて満たされたら、その Phase を `完了` にする
- 範囲外の機能を途中で追加しない
- 仕様変更が必要な場合は、先に `SPEC.md` を更新する
- commit 前に `pre-commit run --all-files` を通す
