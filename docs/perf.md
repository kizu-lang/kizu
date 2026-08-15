# Kizu 性能評価方針

この文書は、Kizu のビルド時間、コンパイル時間、キャッシュサイズを継続的に評価するための入口です。

## 目的

- ビルド時間を短く保つ
- キャッシュが無制限に肥大化しないようにする
- `target` 相当の生成物ディレクトリやローカル build cache が、気づかないうちに
  数十 GB へ膨らむ設計を避ける
- 性能改善をインクリメンタルに評価できるようにする
- 再ビルド理由を説明できるようにする
- CI で毎回走る path を軽く保ち、重い測定は明示 command として分離する

## 測定カテゴリ

### cold run

キャッシュがない状態の実行時間を測る。

### warm run

キャッシュがある状態の実行時間を測る。

### no-op rebuild

入力を変更していない状態で、再実行がどれだけ速いかを測る。

### single-file edit rebuild

1 ファイルだけ変更したときに、再処理範囲が抑えられるかを測る。

### cache size

キャッシュ総量と項目数を測る。

## 初期ベースライン

Go 実装と interpreter を中心に、まず次を測定対象にする。

```sh
go test ./...
go run ./cmd/kizu parse examples/hello.kizu
go run ./cmd/kizu check examples/hello.kizu
go run ./cmd/kizu run examples/hello.kizu
go run ./cmd/kizu parse examples/user_registry.kizu
go run ./cmd/kizu check examples/user_registry.kizu
go run ./cmd/kizu run examples/user_registry.kizu
go run ./cmd/kizu ir examples/hello.kizu
go run ./cmd/kizu build --emit-llvm examples/hello.kizu
go run ./cmd/kizu build --target native examples/hello.kizu
go run ./cmd/kizu cache status
pre-commit run --all-files
```

`examples/user_registry.kizu` は complex app baseline として扱う。

絶対性能の数値保証はまだ置かない。
この段階では、継続的に同じ対象を測り、悪化を見つけられることを優先する。

## 将来の測定対象

```sh
kizu parse <file>
kizu check <file>
kizu run <file>
kizu ir <file>
kizu build <file>
kizu cache status
kizu cache prune
```

## 記録する値

- command
- elapsed time
- input files
- input bytes
- cache directory size
- cache entry count
- cache hit / miss
- Kizu commit
- host OS / architecture

## build cache に入るもの

| artifact | cache key | 保存条件 |
| --- | --- | --- |
| LLVM IR text | source path + source hash + optimization mode | `build --emit-llvm` |
| wasm text | source path + source hash + optimization mode | `build --target wasm32-wasi` |
| runtime object | runtime C source + toolchain | native link のたび(初回だけ compile) |
| run/test 実行ファイル | LLVM IR + runtime object + toolchain | `run` / `test` の link |

prune 条件と status 表示はどれも共通で、`cache prune` が全消し、上限超過分は
古い順に落ちます。測定は `scripts/measure-cache.sh` が cold/warm と single-file
edit を通しで測ります。

## キャッシュ設計の制約

- デフォルト上限を持つ
- `cache status` で状態を見られる
- `cache prune` で削除できる
- module-aware cache key は、compiler version、manifest hash、module graph
  hash、source hash、public interface hash、target、backend、optimization
  mode、stdlib hash を含む
- debug artifact は明示 opt-in にする
- build script と proc macro による隠れた依存は作らない
- CI の必須 path に巨大 artifact 生成や無制限 cache population を入れない
- cache / artifact の新規種類を追加する場合は、cache key、保存条件、prune 条件、
  status 表示、測定 command を同じ変更で定義する
- no-op rebuild と single-file edit rebuild が不必要に重くならないことを確認する

## module graph 測定

module/import 実装後は、少なくとも `tests/fixtures/modules/basic` を使って次を測る。

- manifest と source が変わらない no-op check
- private 実装だけを変えた single-file edit check
- public interface を変えた single-file edit check
- import graph を変えた manifest/module edit check

public interface が変わらない編集では、依存 module の再処理範囲が説明可能でなければならない。
