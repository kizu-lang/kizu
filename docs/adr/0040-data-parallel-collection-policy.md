# ADR-0040: range / partition ベースの data parallelism を撤回する

Status: 撤回（ADR-0025 の改訂に伴う）

## 背景

この ADR は「`std::array::Array<T>` も general slice mutation も allocator-backed
collection も、まだ実装していない」ことを前提に、data parallelism を range と
`Partition` に限定した。

```text
std::task::parallel_for(io, start, end, worker)
std::task::partition_mut(init, count)
std::task::parallel_map(io, partition: &var Partition, start, end, worker)
std::task::LocalBuffer(count, init)
```

`Partition` と `LocalBuffer` は「collection がないので、disjoint な可変出力を
stdlib 内の trusted な箱に閉じ込める」ための代替物だった。

前提は 2 つとも失効した。

- `std::array::Array<T>` は実装済みで動く（`kizu run examples/std_array.kizu`）
- `std::mem` の allocator 境界も決まった

この ADR 自身が「これらは #24 `std::mem` / allocator 境界と #27
`std::array::Array<T>` の仕様が固まった後に設計する」と書いていた。その条件が
満たされた時点で、`Partition` / `LocalBuffer` は存在理由を失っている。

そして ADR-0025 が記録したとおり、`parallel_for` / `parallel_map` は IR lowering を
一度も持たなかった。逐次実行すらしていない。

## 決定

`parallel_for` / `parallel_map` / `partition_mut` / `LocalBuffer` を撤回する。

data parallelism は、ADR-0025 が定めた順番に従って設計し直す。実行系が先で、
安全規則は動く thread の上でだけ書く。collection への接続も、代替の箱を先に
作るのではなく、`Array<T>` と slice に対して直接設計する。

worker が capture を持てない制約（Kizu に closure がないことに由来する）は、
race safety にとって有利な性質なので、次の設計でも維持する候補として残す。

## 影響

- `Partition` / `LocalBuffer` という trusted な中間型がなくなる
- 予約名 `Partition` / `LocalBuffer` が解放される
- data parallelism の設計は `Array<T>` / slice を出発点にやり直す
