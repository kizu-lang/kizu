# Phase 17: C ABI layout / link / native execution policy

状態: 完了

## 目的

Phase 12-14 で構文と extern 宣言は扱えるようになったが、実際の C linking や struct layout は未実装。

この phase では、C ABI と native 実行に必要な最小境界を決める。

## 方針

- actual native linker はこの phase では実装しない
- C ABI layout と link 指定は暗黙にしない
- 通常 `struct` は C layout を約束しない
- 将来 `extern struct` または `repr(c)` 相当を導入する
- runtime symbol は `kizu_` prefix で予約する

## TODO

- [x] native linking をいつ扱うか決める
- [x] runtime symbol の扱いを決める
- [x] `extern "c" fn` の link name / library 指定を検討する
- [x] C struct layout / alignment の最小仕様を決める
- [x] `repr(c)` 相当を採用するか判断する
- [x] LLVM IR backend で extern call をどう出すか確認する
- [x] actual C linking の smoke test 方針を決める

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] C ABI layout / link 方針が ADR にある
- [x] 小さな extern C call の lowering 方針が決まっている
- [x] native 実行を実装する場合、最小 smoke test がある

## 実装メモ

link name / library は将来 attribute として扱う。

```kizu
@link_name("puts")
@link_lib("c")
extern "c" fn c_puts(s: ptr<const u8>) -> i32
```

C layout は通常 struct には付与しない。
候補は `extern struct` または `@repr("c") struct` とする。

actual native linking を実装する phase では、次の smoke test を置く。

```text
Kizu source -> LLVM IR -> object -> native executable -> run
```

## 範囲外

- C++ ABI
- package manager
- cross compilation 完全対応
- system linker 全対応
- native linker 実装
