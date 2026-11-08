# build / cache 性能の測り方

ビルド時間とキャッシュサイズを継続的に測り、悪化に気づけるようにするための文書です。
絶対性能の数値保証はまだ置きません。同じ対象を同じ方法で測り続けることを優先します。

守るもの:

- ビルド時間を短く保つ(`docs/principles.md` 原理 11: 速さは機能)
- 生成物ディレクトリとローカル build cache が、気づかないうちに数十 GB へ
  膨らむ設計を避ける
- 再ビルド理由を説明できる
- CI の必須 path を軽く保ち、重い測定は明示 command として分離する

## 測定カテゴリ

| カテゴリ | 何を見るか |
| --- | --- |
| cold run | cache が無い状態の実行時間 |
| warm run | cache がある状態の実行時間 |
| no-op rebuild | 入力を変えずに再実行したときの速さ |
| single-file edit rebuild | 1 file だけ変えたときの再処理範囲 |
| cache size | cache 総量と項目数 |

`just perf`(`scripts/measure-baseline.sh`)が広めの baseline を、
`just perf-cache`(`scripts/measure-cache.sh`)が cold / warm / no-op /
single-file edit を通しで測ります。`just perf-cache-isolated` は隔離した
一時 cache directory で同じことをします。

baseline の対象は `parse` / `check` / `run` / `ir` / `build --emit-llvm` /
`build --target wasm32-wasi` / `cache status` と、`go test ./...`、
`pre-commit run --all-files` です。入力は `examples/hello.kizu` と、complex app
baseline として `examples/user_registry.kizu` を使います。

記録する値: command、elapsed time、入力 file 数と byte 数、cache directory の
サイズと項目数、cache hit / miss、Kizu の commit、host の OS / architecture。

## build cache に入るもの

| artifact | cache key | 保存条件 |
| --- | --- | --- |
| runtime object | runtime C source + toolchain + `clang --version` の 1 行目 | native link のたび(`build --target native` も含む。初回だけ compile) |
| run/test 実行ファイル | LLVM IR + runtime C source の hash + toolchain | `run` / `test` の link |

toolchain は driver・target・flag・SDK です。`build --emit-llvm` の LLVM IR と
`build --target wasm32-wasi` の出力は cache せず、毎回 lower します(ADR-0126)。
Go seed と Kizu 製 compiler は同じ directory と同じ key を共有します。

`cache prune` が全消し、既定の上限(256 MiB)を超えた分は古い順に落ちます。

CI は `KIZU_CACHE_DIR` を runner の一時 directory に固定し、commit ごとの key で
content-addressed artifact を復元・保存します。新しい commit は直近 cache を prefix
match で復元しますが、全 test は毎回実行し、key が合わない artifact だけを
再 build します。shipping compiler が build する selfhost の第 1 stage も、全入力 file の
hash が一致するときだけ復元します。`TestSelfhostBootstrap` の第 2 stage は毎回
build するため、fixed point の検査は cache hit で省略しません。

## cache 設計の制約

- 既定の上限を持ち、`cache status` で見え、`cache prune` で消せる
- cache key は artifact が何でできているか(LLVM IR・runtime source・toolchain)
  だけから作り、source path や時刻を入れない。compiler が変われば IR が変わるので
  key も変わる(ADR-0126)
- debug artifact は明示 opt-in
- build script と proc macro による隠れた依存を作らない
- CI の必須 path に巨大 artifact 生成や無制限 cache population を入れない
- cache / artifact の種類を足すときは、cache key・保存条件・prune 条件・
  status 表示・測定 command を同じ変更で定義する
- public interface が変わらない編集では、依存 module の再処理範囲が説明できること
