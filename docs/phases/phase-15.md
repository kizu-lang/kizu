# Phase 15: IR optimization pipeline

状態: 完了

## 目的

Phase 8 で追加した小さな最適化 pass を、実際の `kizu ir` / `kizu build` pipeline に接続する。

## 方針

- optimization level は v0.1 では `none` と `opt` の2段階にする
- 既定は `none` にして、最適化は明示的に opt-in する
- cache key には最適化有無を含める
- pass order は `ConstantFold`、`CopyPropagate`、`DeadCodeEliminate` の順に固定する

## TODO

- [x] optimization level の方針を決める
- [x] `kizu ir --opt <file>` を追加する
- [x] `kizu build` 側で opt-in 最適化を使えるようにする
- [x] cache key に optimization level を含める
- [x] constant folding / copy propagation / DCE の適用順序を固定する
- [x] 最適化前後の snapshot test を追加する
- [x] `just` に opt smoke command を追加する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `kizu ir --opt examples/hello.kizu` または同等コマンドが動く
- [x] `Optimize()` が test 以外の実 pipeline から呼ばれる
- [x] cache key が opt level の違いを区別する

## CLI

```sh
kizu ir --opt <file>
kizu build --emit-llvm --opt <file>
kizu build --target wasm32-wasi --opt <file>
```

## 範囲外

- 高度な最適化
- register allocation
- native code generation
- LLVM optimizer 連携
