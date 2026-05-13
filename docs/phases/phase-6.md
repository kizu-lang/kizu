# Phase 6: arena / handle

状態: 未着手

## 目的

長寿命の参照を lifetime ではなく `arena<T>` と `handle<T>` で扱えるようにする。

## TODO

- [ ] `arena<T>()` の構文扱いを決める
- [ ] `arena<T>.add(value)` を実装する
- [ ] `arena<T>.get(handle)` を実装する
- [ ] `handle<T>` を opaque ID として扱う
- [ ] `arena<T>.add(value)` が value を move することを検査する
- [ ] `arena<T>.get(handle)` がローカル borrow を返すことを検査する
- [ ] handle provenance を記録する
- [ ] arena より handle が長生きしないことを検査する
- [ ] handle を raw pointer として扱えないことを検査する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] `examples/arena.kizu` が期待出力を返す
- [ ] arena からの `get` がローカル borrow として扱われる
- [ ] 別 arena の handle を渡すと error になる
- [ ] arena より長生きする handle 使用が error になる

## 範囲外

- arena からの削除
- generational index
- compacting arena
- concurrent arena
- raw pointer
