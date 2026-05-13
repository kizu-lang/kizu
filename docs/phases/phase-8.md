# Phase 8: typed SSA IR

状態: 次に着手

## 目的

checked AST から typed SSA IR に lowering する。

Phase 8 は、まず IR の形と dump を固定する。
最適化 pass は IR が安定してから後半で追加する。

## Phase 8A: IR 導入

- [ ] IR の package 構成を決める
- [ ] IR の型表現を定義する
- [ ] value / instruction / block / function を定義する
- [ ] 基本的な terminator を定義する
- [ ] checked AST から IR へ lower する entrypoint を作る
- [ ] `kizu ir <file>` の CLI を追加する
- [ ] IR dump を読みやすくする
- [ ] IR dump の snapshot test を追加する
- [ ] `examples/hello.kizu` を IR に lower する
- [ ] `examples/functions.kizu` を IR に lower する
- [ ] `examples/variables.kizu` を IR に lower する
- [ ] baseline 測定に `kizu ir examples/hello.kizu` を追加する

## Phase 8B: control flow lowering

- [ ] basic block を複数持てるようにする
- [ ] branch terminator を定義する
- [ ] `if` を basic block に lower する
- [ ] `while` を basic block に lower する
- [ ] phi node を定義する
- [ ] phi node が必要な代入を扱う
- [ ] `examples/if.kizu` を IR に lower する
- [ ] `examples/while.kizu` を IR に lower する

## Phase 8C: struct / arena lowering

- [ ] struct literal の IR 表現を定義する
- [ ] field access の IR 表現を定義する
- [ ] `arena<T>()` の IR 表現を定義する
- [ ] `arena.add(value)` の IR 表現を定義する
- [ ] `arena.get(handle)` の IR 表現を定義する
- [ ] `examples/arena.kizu` を IR に lower する

## Phase 8D: small optimization passes

- [ ] optimization pass の package 構成を決める
- [ ] constant folding を追加する
- [ ] dead code elimination を追加する
- [ ] simple copy propagation を追加する
- [ ] 最適化前後の IR dump を snapshot test で検証する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] `kizu ir examples/hello.kizu` が typed SSA IR を出す
- [ ] Phase 2 examples を IR に lower できる
- [ ] IR dump が snapshot test で検証される
- [ ] baseline 測定に `kizu ir` を追加できる

## Phase 8A の完了条件

- [ ] `pre-commit run --all-files` が通る
- [ ] `go run ./cmd/kizu ir examples/hello.kizu` が IR dump を出す
- [ ] `go run ./cmd/kizu ir examples/functions.kizu` が IR dump を出す
- [ ] `go run ./cmd/kizu ir examples/variables.kizu` が IR dump を出す
- [ ] IR dump の snapshot test がある
- [ ] `scripts/measure-baseline.sh` が `kizu ir examples/hello.kizu` を測定する

## 範囲外

- LLVM IR 生成
- WASM / WASI 生成
- unsafe / C ABI
- comptime
- Phase 8A では control flow、struct、arena、最適化 pass
