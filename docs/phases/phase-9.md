# Phase 9: LLVM IR backend

状態: 未着手

## 目的

typed SSA IR から LLVM IR を生成する。

## TODO

- [ ] LLVM IR の出力形式を決める
- [ ] primitive type を LLVM 型へ対応させる
- [ ] function を LLVM IR に lower する
- [ ] basic block / branch / return を lower する
- [ ] integer arithmetic を lower する
- [ ] comparison を lower する
- [ ] `print` の runtime ABI を決める
- [ ] `kizu build --emit-llvm <file>` を追加する
- [ ] LLVM IR の snapshot test を追加する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] `kizu build --emit-llvm examples/hello.kizu` が LLVM IR を出す
- [ ] Phase 2 examples の LLVM IR を生成できる
- [ ] LLVM IR generation の baseline を測定できる

## 範囲外

- native linker
- WASM / WASI
- unsafe / C ABI
- C header import
