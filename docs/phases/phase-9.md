# Phase 9: LLVM IR backend

状態: 完了

## 目的

typed SSA IR から LLVM IR を生成する。

## TODO

- [x] LLVM IR の出力形式を決める
- [x] primitive type を LLVM 型へ対応させる
- [x] function を LLVM IR に lower する
- [x] basic block / branch / return を lower する
- [x] integer arithmetic を lower する
- [x] comparison を lower する
- [x] `print` の runtime ABI を決める
- [x] `kizu build --emit-llvm <file>` を追加する
- [x] LLVM IR の snapshot test を追加する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `kizu build --emit-llvm examples/hello.kizu` が LLVM IR を出す
- [x] Phase 2 examples の LLVM IR を生成できる
- [x] LLVM IR generation の baseline を測定できる

## 実装メモ

Phase 9 の LLVM IR は text backend として実装する。
native linker や runtime 実体は扱わない。

`print` は次の runtime ABI 宣言へ lower する。

```llvm
declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)
```

`string` は `ptr` と length の組を print ABI に渡す。
整数 literal は `i64` として lower する。

struct / arena / handle は Phase 9 では LLVM の具体 layout へ lower しない。
必要になった値は opaque pointer 相当として扱い、native 実行は後続 phase で扱う。

## 範囲外

- native linker
- WASM / WASI
- unsafe / C ABI
- C header import
