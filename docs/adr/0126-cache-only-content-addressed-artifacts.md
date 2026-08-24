# ADR-0126: build cache は content-addressed な artifact だけを保持する

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
含まれる。selfhost 移植で Go CLI と selfhost が cache を共有するようになり、
この取り違えは 2 つの compiler 間でも起きるようになった。

## 決定

text entry を廃止し、cache は content-addressed な artifact だけを持つ。
`build --emit-llvm` と `--target wasm32-wasi` は毎回 lower して印字する。
`GetOrBuild` / `newFileInput` / `writeEntry` / `Result` は削除する。

artifact entry は影響を受けない。runtime object の key はその C source の
bytes を、実行ファイルの key は LLVM IR text と runtime object 名を含むので、
compiler が変われば IR が変わり、key も変わる。stale hit は構造的に起きない。

text の生成(parse → check → lower → emit)は toolchain を伴わず安価で、
cache の利得は計測可能なほど無い。高価なのは clang の compile / link だけで、
それは artifact 側が持ち続ける。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| compiler binary の hash を key に足す | selfhost は自分の実行ファイルの path を知る手段が無く(std::process は argv[0] を出さない)、Go CLI とだけ key が付く非対称な cache になる |
| `Version` 定数を挙動変更のたびに手で bump する | 人の記憶に依存して漂流する。bump を忘れた 1 回が stale text を配る |
| version command の文字列を key に足す | 開発 build は `devel` で全部同じ key になり、いちばん compiler が変わる状況で効かない |
