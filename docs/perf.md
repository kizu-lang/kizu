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
`build --target wasm32-wasi` / `cache status` と、`scripts/bootstrap.sh`、
`pre-commit run --all-files` です。入力は `examples/hello.kizu` と、complex app
baseline として `examples/user_registry.kizu` を使います。

記録する値: command、elapsed time、入力 file 数と byte 数、cache directory の
サイズと項目数、cache hit / miss、Kizu の commit、host の OS / architecture。

## build cache に入るもの

| artifact | cache key | 保存条件 |
| --- | --- | --- |
| runtime object | runtime C source + toolchain + `clang --version` の 1 行目 | native link のたび(`build --target native` も含む。初回だけ compile) |
| unit object(`native-obj`) | unit の LLVM IR + toolchain + `clang --version` の 1 行目 | debug の native link で miss した unit(`build --target native` も含む) |
| unit bitcode(`native-bc`) | 同上(toolchain に `-flto=thin` と link 側の codegen flag が入る) | `build --target native --opt --thinlto` で miss した unit |
| run/test 実行ファイル | 全 unit の LLVM IR の hash を unit 順に並べたもの + runtime C source の hash + toolchain | `run` / `test` の link |

debug build は LLVM IR を module ごとの unit と program unit(package の `main`、test
runner、error 名の表)に割り、unit ごとに object を作って link します。cache に無い
unit は processor の数だけ並行に compile し、link は完了順でなく unit 順に並べます。1 module の
編集で clang が compile し直すのはその unit だけで、署名や型を変えたときはそれを
参照する unit も変わります。`--opt` は 1 unit のまま clang に -O3 で丸ごと渡します。
`--opt --thinlto` は debug と同じく module ごとに割り、unit ごとに `-O3 -flto=thin` の
bitcode を作って cache し、link で ThinLTO が unit を跨いで最適化します。linker が
ThinLTO で作った code は cache directory の下の `thinlto/` に linker 自身が置き、
prune も linker の既定に任せます(空の cache で build すればここも空から始まる)。
`--opt` の IR inliner は分割の前にかかるので、小さい関数の編集は呼ぶ側の unit も変えます。
実行ファイルの鍵に clang の版は入らないので、cache にある実行ファイルを走らせる
`run` は toolchain に触れません。

toolchain は driver・target・flag・SDK です。`build --emit-llvm` の LLVM IR と
`build --target wasm32-wasi` の出力は cache せず、毎回 lower します(ADR-0126)。

`cache prune` が全消し、既定の上限(256 MiB)を超えた分は最後に使われたのが古い順
(LRU)に落ちます。hit のたびに entry の `used_at` を更新します。保存のたびに見るのは
artifact の file サイズの合計だけで、entry を全部読んで並べるのは上限を超えたときだけです。
`cache status` は artifact の種別(key の先頭: `native-exe` など)ごとの件数とサイズも出します。

CI は `KIZU_CACHE_DIR` を runner の一時 directory に固定し、commit ごとの key で
content-addressed artifact を復元・保存します。新しい commit は直近 cache を prefix
match で復元しますが、全 test は毎回実行し、key が合わない artifact だけを
再 build します。種が build する stage1 もこの cache を通ります。bootstrap の検査
(`compiler/tests/harness` の `bootstrap`)は stage2 に空の cache で自分自身を build し直させる
ため、fixed point の検査は cache hit で省略しません。

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
