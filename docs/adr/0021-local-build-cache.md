# ADR-0021: ローカルビルドキャッシュは上限付きにする

Status: 採用

## 背景

Kizu は CI とローカル開発のビルドを軽く保ちたい。
Rust のように build cache が数十 GB まで肥大化する状態は避ける。

一方で、no-op rebuild や LLVM IR generation はキャッシュできる。
キャッシュする場合でも、保存場所、key、上限、削除方法、再ビルド理由を明示する必要がある。

## 決定

Kizu のローカルビルドキャッシュは上限付きにする。

初期方針:

- cache directory は `KIZU_CACHE_DIR` で上書きできる
- 未指定時は OS の user cache directory 配下の `kizu` を使う
- cache key は compiler cache version、target、source path、source content hash から作る
- cache entry は JSON metadata と artifact file に分ける
- デフォルト上限は 256 MiB
- 上限超過時は古い entry から削除する
- `kizu cache status` で状態を表示する
- `kizu cache prune` で明示的に削除できる
- `kizu why-rebuild <file>` で cache hit / miss と理由を表示する

## v0 の対象

Phase 10 では `kizu build --emit-llvm <file>` の LLVM IR text を cache 対象にする。

native object、link result、package dependency graph、remote cache は扱わない。

## 影響

- キャッシュは無制限に増えない
- no-op rebuild は cache hit にできる
- single-file edit rebuild は source content hash の変化として説明できる
- cache key に source path を含めるため、同じ内容でも別 file は別 entry になる
- remote cache や分散ビルドは将来 phase で別途判断する
