# Phase 11: WASM / WASI backend

状態: 完了

## 目的

typed SSA IR から WASM を生成し、WASI で実行できるようにする。

## TODO

- [x] WASM の target subset を決める
- [x] primitive type を WASM 型へ対応させる
- [x] function / block / branch / loop を lower する
- [x] WASI stdout の runtime ABI を決める
- [x] `kizu build --target wasm32-wasi <file>` を追加する
- [x] wasmtime で実行する smoke test を追加する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] Phase 2 examples が WASI で動く
- [x] WASM generation の baseline を測定できる

## 実装メモ

Phase 11 では binary `.wasm` ではなく、WASI-compatible な WAT を生成する。

```sh
kizu build --target wasm32-wasi examples/hello.kizu
```

生成物は `_start` を export し、WASI `fd_write` で stdout に出力する。
開発環境では `wasmtime` を使って smoke test する。

```sh
just wasi-smoke
```

対応する target subset:

- `i64` は WebAssembly `i64`
- `bool` は `i32`
- `string` は linear memory 上の data segment
- function call
- `if`
- `while`
- `print`

WASM generation の baseline は `scripts/measure-baseline.sh` と
`scripts/measure-cache.sh` で測定できる。

## 範囲外

- browser WASM integration
- WASI filesystem API
- threads
- binary `.wasm` emission
- arena / handle lowering
