# Phase 10: build cache / why-rebuild

状態: 完了

## 目的

Kizu のビルドキャッシュを上限付きで管理し、再ビルド理由を説明できるようにする。

## TODO

- [x] cache directory の場所を決める
- [x] cache key の構成を決める
- [x] cache entry format を決める
- [x] cache size 上限を決める
- [x] `kizu cache status` を実装する
- [x] `kizu cache prune` を実装する
- [x] `kizu why-rebuild <file>` を実装する
- [x] cold / warm / no-op rebuild の測定を追加する
- [x] small edit rebuild の測定を追加する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] cache size を表示できる
- [x] cache を prune できる
- [x] no-op rebuild が cache hit になる
- [x] `why-rebuild` が再ビルド理由を表示する
- [x] cache がデフォルト上限を持つ

## 実装メモ

cache directory:

```text
KIZU_CACHE_DIR があればその値
未指定なら OS の user cache directory 配下の kizu
```

cache key:

```text
compiler cache version
target
absolute source path
source content hash
```

entry format:

```text
<key>.json  metadata
<key>.out   artifact
```

Phase 10 では `kizu build --emit-llvm <file>` の LLVM IR text を cache 対象にする。

デフォルト上限は 256 MiB。
上限超過時は古い entry から削除する。

測定:

- `scripts/measure-baseline.sh` に no-op LLVM build、`why-rebuild`、`cache status` を追加
- `scripts/measure-cache.sh` に cold / warm / no-op / small edit 測定を追加

## 範囲外

- 分散ビルド
- remote cache
- package manager
