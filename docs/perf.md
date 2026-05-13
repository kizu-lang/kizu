# Kizu 性能評価方針

この文書は、Kizu のビルド時間、コンパイル時間、キャッシュサイズを継続的に評価するための入口です。

## 目的

- ビルド時間を短く保つ
- キャッシュが無制限に肥大化しないようにする
- 性能改善をインクリメンタルに評価できるようにする
- 再ビルド理由を説明できるようにする

## 測定カテゴリ

### cold run

キャッシュがない状態の実行時間を測る。

### warm run

キャッシュがある状態の実行時間を測る。

### no-op rebuild

入力を変更していない状態で、再実行がどれだけ速いかを測る。

### small edit rebuild

1 ファイルだけ変更したときに、再処理範囲が小さく保たれるかを測る。

### cache size

キャッシュ総量と項目数を測る。

### why-rebuild

なぜ再ビルドされたかを説明できるかを測る。

## 初期ベースライン

Phase 2 までは Go 実装と interpreter が中心なので、まず次を測定対象にする。

```sh
go test ./...
go run ./cmd/kizu parse examples/hello.kizu
go run ./cmd/kizu run examples/hello.kizu
pre-commit run --all-files
```

## 将来の測定対象

```sh
kizu parse <file>
kizu check <file>
kizu run <file>
kizu ir <file>
kizu build <file>
kizu cache status
kizu why-rebuild <file>
```

## 記録する値

- command
- elapsed time
- input files
- input bytes
- cache directory size
- cache entry count
- cache hit / miss
- rebuild reason
- Kizu commit
- host OS / architecture

## キャッシュ設計の制約

- デフォルト上限を持つ
- `cache status` で状態を見られる
- `cache prune` で削除できる
- `why-rebuild` で再ビルド理由を見られる
- debug artifact は明示 opt-in にする
- build script と proc macro による隠れた依存は作らない
