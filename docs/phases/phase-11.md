# Phase 11: WASM / WASI backend

状態: 未着手

## 目的

typed SSA IR から WASM を生成し、WASI で実行できるようにする。

## TODO

- [ ] WASM の target subset を決める
- [ ] primitive type を WASM 型へ対応させる
- [ ] function / block / branch / loop を lower する
- [ ] WASI stdout の runtime ABI を決める
- [ ] `kizu build --target wasm32-wasi <file>` を追加する
- [ ] wasmtime で実行する smoke test を追加する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] Phase 2 examples が WASI で動く
- [ ] WASM generation の baseline を測定できる

## 範囲外

- browser WASM integration
- WASI filesystem API
- threads
