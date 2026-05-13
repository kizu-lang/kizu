# Phase 8: typed SSA IR

状態: 未着手

## 目的

checked AST から typed SSA IR に lowering する。

## TODO

- [ ] IR の package 構成を決める
- [ ] IR の型表現を定義する
- [ ] value / instruction / block / function を定義する
- [ ] phi node を定義する
- [ ] `kizu ir <file>` の CLI を追加する
- [ ] Phase 2 examples を IR に lower する
- [ ] IR dump を読みやすくする
- [ ] constant folding を追加する
- [ ] dead code elimination を追加する
- [ ] simple copy propagation を追加する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] `kizu ir examples/hello.kizu` が typed SSA IR を出す
- [ ] Phase 2 examples を IR に lower できる
- [ ] IR dump が snapshot test で検証される
- [ ] baseline 測定に `kizu ir` を追加できる

## 範囲外

- LLVM IR 生成
- WASM / WASI 生成
- unsafe / C ABI
- comptime
