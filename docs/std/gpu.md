# std::gpu

`export "gpu" fn`(SPEC §12.5)の本体が使う、GPU の dispatch についての問い合わせです。

```text
std::gpu::thread_index() -> i64
```

`thread_index` は dispatch した thread の通し番号を 0 から返します。`build --target metal`
では Metal の `thread_position_in_grid` です。CPU と wasm の build は、この関数に到達すると
拒否します。

## まだ無いもの

- threadgroup memory、threadgroup 内の barrier、SIMD group の shuffle
- 2 次元・3 次元の dispatch
- `export "gpu"` から呼ぶ関数への view の受け渡し
