# Phase 10: build cache / why-rebuild

状態: 未着手

## 目的

Kizu のビルドキャッシュを上限付きで管理し、再ビルド理由を説明できるようにする。

## TODO

- [ ] cache directory の場所を決める
- [ ] cache key の構成を決める
- [ ] cache entry format を決める
- [ ] cache size 上限を決める
- [ ] `kizu cache status` を実装する
- [ ] `kizu cache prune` を実装する
- [ ] `kizu why-rebuild <file>` を実装する
- [ ] cold / warm / no-op rebuild の測定を追加する
- [ ] small edit rebuild の測定を追加する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] cache size を表示できる
- [ ] cache を prune できる
- [ ] no-op rebuild が cache hit になる
- [ ] `why-rebuild` が再ビルド理由を表示する
- [ ] cache がデフォルト上限を持つ

## 範囲外

- 分散ビルド
- remote cache
- package manager
