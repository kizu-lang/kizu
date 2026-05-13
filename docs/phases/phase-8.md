# Phase 8: typed SSA IR

状態: 完了

## 目的

checked AST から typed SSA IR に lowering する。

Phase 8 は、まず IR の形と dump を固定する。
最適化 pass は IR が安定してから後半で追加する。

## Phase 8A: IR 導入

- [x] IR の package 構成を決める
- [x] IR の型表現を定義する
- [x] value / instruction / block / function を定義する
- [x] 基本的な terminator を定義する
- [x] checked AST から IR へ lower する entrypoint を作る
- [x] `kizu ir <file>` の CLI を追加する
- [x] IR dump を読みやすくする
- [x] IR dump の snapshot test を追加する
- [x] `examples/hello.kizu` を IR に lower する
- [x] `examples/functions.kizu` を IR に lower する
- [x] `examples/variables.kizu` を IR に lower する
- [x] baseline 測定に `kizu ir examples/hello.kizu` を追加する

## Phase 8B: control flow lowering

- [x] basic block を複数持てるようにする
- [x] branch terminator を定義する
- [x] `if` を basic block に lower する
- [x] `while` を basic block に lower する
- [x] phi node を定義する
- [x] phi node が必要な代入を扱う
- [x] `examples/if.kizu` を IR に lower する
- [x] `examples/while.kizu` を IR に lower する

## Phase 8C: struct / arena lowering

- [x] struct literal の IR 表現を定義する
- [x] field access の IR 表現を定義する
- [x] `arena<T>()` の IR 表現を定義する
- [x] `arena.add(value)` の IR 表現を定義する
- [x] `arena.get(handle)` の IR 表現を定義する
- [x] `examples/arena.kizu` を IR に lower する

## Phase 8D: small optimization passes

- [x] optimization pass の package 構成を決める
- [x] constant folding を追加する
- [x] dead code elimination を追加する
- [x] simple copy propagation を追加する
- [x] 最適化前後の IR dump を snapshot test で検証する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `kizu ir examples/hello.kizu` が typed SSA IR を出す
- [x] Phase 2 examples を IR に lower できる
- [x] IR dump が snapshot test で検証される
- [x] baseline 測定に `kizu ir` を追加できる

## Phase 8A の完了条件

- [x] `pre-commit run --all-files` が通る
- [x] `go run ./cmd/kizu ir examples/hello.kizu` が IR dump を出す
- [x] `go run ./cmd/kizu ir examples/functions.kizu` が IR dump を出す
- [x] `go run ./cmd/kizu ir examples/variables.kizu` が IR dump を出す
- [x] IR dump の snapshot test がある
- [x] `scripts/measure-baseline.sh` が `kizu ir examples/hello.kizu` を測定する

## 範囲外

- LLVM IR 生成
- WASM / WASI 生成
- unsafe / C ABI
- comptime
