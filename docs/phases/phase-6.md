# Phase 6: arena / handle

状態: 完了

## 目的

長寿命の参照を lifetime ではなく `arena<T>` と `handle<T>` で扱えるようにする。

## TODO

- [x] `arena<T>()` の構文扱いを決める
- [x] `arena<T>.add(value)` を実装する
- [x] `arena<T>.get(handle)` を実装する
- [x] `handle<T>` を opaque ID として扱う
- [x] `arena<T>.add(value)` が value を move することを検査する
- [x] `arena<T>.get(handle)` がローカル borrow を返すことを検査する
- [x] handle provenance を記録する
- [x] arena より handle が長生きしないことを検査する
- [x] handle を raw pointer として扱えないことを検査する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `examples/arena.kizu` が期待出力を返す
- [x] arena からの `get` がローカル borrow として扱われる
- [x] 別 arena の handle を渡すと error になる
- [x] arena より長生きする handle 使用が error になる

## 実装メモ

v0 では `arena<T>()`、`arena.add(value)`、`arena.get(handle)` を実装する。

`handle<T>` は runtime では arena と index の組で、Kizu プログラムから raw pointer としては扱えない。
ownership checker は `arena.add(value)` の receiver を記録し、`arena.get(handle)` で別 arena の handle を拒否する。

`arena.get(handle)` は値を所有物として move せず、ローカル borrow 相当の read として扱う。
明示 lifetime annotation は導入しない。

arena より長生きする handle は、v0 ではローカル arena から作った handle を関数から返す形を拒否する。

## 範囲外

- arena からの削除
- generational index
- compacting arena
- concurrent arena
- raw pointer
