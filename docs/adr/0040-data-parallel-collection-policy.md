# ADR-0040: v0.1 data parallelism is range and partition based

Status: 採用

## 背景

Kizu v0.1 では `std::array::Array<T>`、general slice mutation、
allocator-backed collection をまだ実装しない。一方で、data parallel API の
memory-safety contract は早く固定したい。

## 決定

v0.1 の data parallelism は collection API に直接つながない。

採用する最小 API:

```text
std::task::parallel_for(io, start, end, worker)
std::task::partition_mut(init, count)
std::task::parallel_map(io, partition: &mut Partition, start, end, worker)
std::task::LocalBuffer(count, init)
```

`parallel_for` は range 専用とする。
`parallel_map` の output は `Partition` に限定する。
`Partition` は stdlib 内の trusted disjoint mutable output であり、ユーザーが任意の
shared mutable state を worker に渡す仕組みではない。

worker rule:

- `parallel_for` worker は `fn(i: i64) -> void` または `fn(i: i64) -> !void`
- `parallel_map` worker は `fn(i: i64) -> i64`
- worker は追加 capture を持たない
- shared mutable state は worker 引数として渡せない
- `parallel_for` は最初の `!void` error を `!void` として返す
- v0.1 interpreter は逐次実行でよいが、API contract は実並行 runtime と同じにする

## Collection との接続

`std::array::Array<T>` / mutable slice / allocator を導入するまでは、`parallel_map` を
collection に直接書き込ませない。

v0.2 以降で検討する API:

```text
std::mem::partition_mut(slice, parts)
std::array::parallel_map(io, array, worker)
```

これらは #24 `std::mem` / allocator 境界と #27 `std::array::Array<T>` の仕様が
固まった後に
設計する。

## 安全性

safe Kizu では worker に任意の mutable borrow、raw pointer、arena handle、
`Mutex<T>` などを capture させない。disjoint mutation は `Partition` だけが持つ
trusted stdlib boundary に閉じ込める。

これにより、v0.1 の範囲では data race を作る表現を safe code に提供しない。
