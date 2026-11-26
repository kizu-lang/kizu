# std::gpu

`export "gpu" fn`(SPEC §12.5)の本体と、そこから呼ぶ関数が使う API です。CPU と wasm の
build は、どれかに到達すると拒否します。

```text
std::gpu::thread_index() -> i64
std::gpu::group_index() -> i64
std::gpu::local_index() -> i64
std::gpu::simdgroup_index() -> i64
std::gpu::lane_index() -> i64
std::gpu::barrier() -> void
std::gpu::shuffle_xor(value: f32, mask: i64) -> f32
std::gpu::simd_max(value: f32) -> f32
std::gpu::simd_sum(value: f32) -> f32
std::gpu::sqrt(value: f32) -> f32
std::gpu::exp(value: f32) -> f32
std::gpu::log(value: f32) -> f32
std::gpu::fma(left: f32, right: f32, addend: f32) -> f32
std::gpu::f32_from_half(bits: u16) -> f32
std::gpu::half_from_f32(value: f32) -> u16
std::gpu::group_array<[N]T>() -> [N]T
std::gpu::simd_matrix_zero() -> std::gpu::SimdMatrix8x8F32
std::gpu::simd_matrix_load_f16(src: []u16, offset: i64, stride: i64, transpose: bool) -> std::gpu::SimdMatrix8x8F16
std::gpu::simd_matrix_mma_f16(acc: std::gpu::SimdMatrix8x8F32, a: std::gpu::SimdMatrix8x8F16, b: std::gpu::SimdMatrix8x8F16) -> std::gpu::SimdMatrix8x8F32
std::gpu::simd_matrix_store_f32(acc: std::gpu::SimdMatrix8x8F32, dst: &var []f32, offset: i64, stride: i64) -> void
```

番号は 0 から数えます。`thread_index` は dispatch 全体での通し番号、`group_index` は
threadgroup の番号、`local_index` は threadgroup の中での番号、`simdgroup_index` と
`lane_index` は threadgroup の中の SIMD group の番号と、その中での番号です。SPIR-V では SIMD group は
subgroup で、その大きさは device が決めます。

`barrier` は threadgroup の全 thread がそこに着くまで待ち、それまでの threadgroup memory
への書き込みを見えるようにします。threadgroup の全 thread が同じ回数だけ呼ぶ必要があります。

`shuffle_xor` は同じ SIMD group の `lane_index() ^ mask` 番の thread が持つ `value` を返し、
`simd_max` は SIMD group の全 thread の `value` の最大値、`simd_sum` は合計を返します。

`sqrt` / `exp` / `log` は GPU の f32 の関数です。`sqrt` は正しく丸めますが、`exp` と `log` は
CPU の `std::math` と bit が一致するとは限りません。`fma` は `left * right + addend` を
1 回だけ丸めて返します。

`f32_from_half` は IEEE 754 binary16 の bit を `u16` で受け取り、その値を `f32` で返します。
変換は正確です。`half_from_f32` は `f32` を binary16 に丸めた bit を返します。binary16 で
表せる値は正確に変換しますが、表せない値の丸め方は device によって違います。threadgroup
memory に値を半分の大きさで置くときに使います。

`group_array<[N]T>()` は threadgroup の全 thread が共有する配列を返します。`T` は整数か
`f32` です。`var a = gpu::group_array<[N]T>();` と束縛し、`a.as_mut_slice()` の view で
読み書きします。`export "gpu" fn` の本体でだけ使え、中身は初期化されません。thread 間の
書き込みの衝突は検査しないので、`barrier` で順序を付けます。

`SimdMatrix8x8F32` と `SimdMatrix8x8F16` は、SIMD group 全体で協調してレジスタ上に持つ
8×8 行列の opaque な copy 値型です。現在の対応 target は `--target metal` のみで、
SPIR-V・CPU・wasm では拒否します。`simd_matrix_zero` は全要素 0.0 の 8×8 `f32` 行列を
返します。`simd_matrix_load_f16` は device または threadgroup の `[]u16` view から
`offset` を先頭に行幅 `stride` で 8×8 の binary16 タイルを読み込み、`transpose` が `true`
なら転置して返します。`simd_matrix_mma_f16` は `a * b + acc` を `f32` 精度で計算して
返します。`simd_matrix_store_f32` は device または threadgroup の `&var []f32` view へ
`offset` と行幅 `stride` で 8×8 の `f32` タイルを書き出します。load / store とも
`offset >= 0`・`stride >= 8`・8×8 タイル全体が slice の範囲内にあることを検査し、
範囲外なら kernel を失敗させて戻ります。thread-local 配列の view に対する load / store は
compile 時に拒否します。

## まだ無いもの

- 2 次元・3 次元の dispatch
- f32 以外の `shuffle_xor` / `simd_max` / `simd_sum`、他の SIMD group 演算
- SPIR-V 向けの協調行列演算
