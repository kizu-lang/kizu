# Phase 6: arena / handle

状態: 未着手

## 目的

長寿命の参照を lifetime ではなく `arena<T>` と `handle<T>` で扱えるようにする。

## TODO

- [ ] `arena<T>()` の構文扱いを決める
- [ ] `arena<T>.add(value)` を実装する
- [ ] `arena<T>.get(handle)` を実装する
- [ ] `handle<T>` を opaque ID として扱う
- [ ] arena より handle が長生きしないことを検査する方針を決める

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] `examples/arena.kizu` が期待出力を返す
- [ ] arena からの `get` がローカル borrow として扱われる

## 範囲外

- arena からの削除
- generational index
- compacting arena
- concurrent arena
