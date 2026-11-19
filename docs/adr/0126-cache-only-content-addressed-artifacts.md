# ADR-0126: build cache の key は、仕事を決めるものをすべて含む

Status: 採用

Issue: #1675

## 背景

build cache には 2 種類の entry があった。

1. **artifact**: native runtime object と実行ファイル。key はその artifact の
   入力そのもの(runtime C source の hash、LLVM IR text の hash と toolchain)
   から導かれる。
2. **text**: `build --emit-llvm` / `--target wasm32-wasi` が印字する lowering
   結果。key は **source file の hash** から導かれ、lowering は compiler が行う。

text entry の key は compiler 自身を含まない。compiler を変えて同じ source を
lower すると、cache は**古い compiler の出力を正とした text** を返す。これは
「cache は済んだ仕事の記録」に反する: 仕事の同一性には、それを行った道具が
含まれる。

一方、artifact だけにすると、何も変えていない `kizu build` も front end と backend を
全部回す。compiler 自身の build で 3.1 秒かかり、合成した 16 万行の package では Go の no-op build の約 24 倍だった。

## 決定

cache の entry は 3 種類とし、どれも仕事を決めるものをすべて key に持つ。

- **artifact**: 生成物の中身から key を作る。compiler が変われば IR が変わるので、
  compiler 自身は key に要らない。
- **build plan**(`native-plan`): `build --target native` の実行ファイル。key は
  compiler の実行ファイルの SHA-256、target、lib dir、出力先、native option、
  toolchain(`clang --version` の 1 行目、host、`SDKROOT`)から作る。entry は前回の
  build が読んだ入力の一覧と実行ファイルを持つ。入力は読んだ file すべて(source、std、
  manifest、`@embed_file`)の中身の hash と、走査した directory の `.kizu` file と
  sub directory の名前の hash で、全部一致したときだけ実行ファイルを返す。

- **関数の機械語**(`native-fn`): direct backend が出力した関数ごとの機械語。entry の
  key は compiler の実行ファイルの SHA-256 と target で、中の関数ごとの key は inline を
  終えた MIR と symbol 名の SHA-256。

library や framework を link する build は plan を残さない。link する file の中身を
追跡していないため。`build --emit-llvm` と `--target wasm32-wasi` は毎回 lower して
印字する。text の生成は toolchain を伴わず安価で、cache の利得が無い。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| source の hash だけを key にする | compiler を変えたときに古い出力を返す |
| `Version` 定数を挙動変更のたびに手で bump する | 人の記憶に依存して漂流する。bump を忘れた 1 回が stale な出力を配る |
| version command の文字列を key に足す | 開発 build は `devel` で全部同じ key になり、いちばん compiler が変わる状況で効かない |
| build 時に compiler と std の source tree hash を binary に埋め込む | 毎回の hash(12 MB で約 33 ms)は省けるが、埋め込みの更新を忘れた build が stale な出力を返す |
| file の時刻と大きさで変更を判定する | 速いが、時刻を保ったまま中身が変わる操作で stale な出力を返す |
