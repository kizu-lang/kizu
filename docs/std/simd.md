# std::simd

明示的な 128-bit SIMD vector 操作と、連続 slice 向けの SIMD カーネルを提供します。

vector 型(`f64x2` / `f32x4` / `i64x2` / `i32x4` / `i16x8` / `i8x16` / `u64x2` / `u32x4` /
`u16x8` / `u8x16`)そのものの定義と演算子は `SPEC.md` §7.3 にあります。この module は、
その上の load / store、table lookup、FMA、8-bit dot product、型変換、popcount、
および連続 slice の内積カーネルを提供します。

```kizu
import std::simd;

let lut = u8x16 { 10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160 };
let idx = u8x16 { 15, 0, 3, 16, 255, 7, 1, 2, 4, 5, 6, 8, 9, 10, 11, 12 };
let out = simd::tbl16(lut, idx); // out[0] == 160, out[3] == 0

let dot = simd::dot_q8_f32(weights_u8, input_f32);
```

## API

### broadcast (splat)

```kizu
pub fn splat_f32x4(value: f32) -> f32x4
pub fn splat_f64x2(value: f64) -> f64x2
pub fn splat_i32x4(value: i32) -> i32x4
pub fn splat_u32x4(value: u32) -> u32x4
pub fn splat_i8x16(value: i8) -> i8x16
pub fn splat_u8x16(value: u8) -> u8x16
```

### 連続 slice の load / store

```kizu
pub fn load_f32x4(slice: []f32, offset: i64) -> f32x4
pub fn store_f32x4(slice: &var []f32, offset: i64, value: f32x4) -> void
pub fn load_f64x2(slice: []f64, offset: i64) -> f64x2
pub fn store_f64x2(slice: &var []f64, offset: i64, value: f64x2) -> void
pub fn load_i32x4(slice: []i32, offset: i64) -> i32x4
pub fn store_i32x4(slice: &var []i32, offset: i64, value: i32x4) -> void
pub fn load_u8x16(slice: []u8, offset: i64) -> u8x16
pub fn store_u8x16(slice: &var []u8, offset: i64, value: u8x16) -> void
pub fn load_i8x16(slice: []i8, offset: i64) -> i8x16
pub fn store_i8x16(slice: &var []i8, offset: i64, value: i8x16) -> void
pub fn load_i8x16_from_u8(slice: []u8, offset: i64) -> i8x16
```

いずれも境界検査を行い、`offset < 0` または `offset + lanes > count` なら panic
します。`load_i8x16_from_u8` は `[]u8` の 16 byte を符号付き `i8x16` として
読み込みます。

### 16 エントリ並列テーブル引き: `tbl16`

```kizu
pub fn tbl16(table: u8x16, indices: u8x16) -> u8x16
```

各 lane `j` について、`indices[j] < 16` なら `table[indices[j]]`、`indices[j] >= 16`
なら `0` を返します。

- ARM64 LLVM: NEON `tbl` (`llvm.aarch64.neon.tbl1.v16i8`)
- x86-64 LLVM (SSSE3 / AVX2、例 `--cpu native` / `--cpu x86-64-v3`): `vpshufb` / `pshufb` (範囲外マスク付き)
- wasm32: `i8x16.swizzle`

### FMA・8-bit 内積・型変換・popcount・縮約

```kizu
pub fn fma_f32(a: f32, b: f32, c: f32) -> f32
pub fn fma_f32x4(a: f32x4, b: f32x4, c: f32x4) -> f32x4
pub fn fma_f64x2(a: f64x2, b: f64x2, c: f64x2) -> f64x2

pub fn sdot_i8x16(acc: i32x4, a: i8x16, b: i8x16) -> i32x4

pub fn i8x16_to_f32x4_0(v: i8x16) -> f32x4
pub fn i8x16_to_f32x4_1(v: i8x16) -> f32x4
pub fn i8x16_to_f32x4_2(v: i8x16) -> f32x4
pub fn i8x16_to_f32x4_3(v: i8x16) -> f32x4
pub fn f32x4_from_i32x4(v: i32x4) -> f32x4
pub fn i32x4_from_f32x4(v: f32x4) -> i32x4
pub fn i8x16_from_u8x16(v: u8x16) -> i8x16
pub fn u8x16_from_i8x16(v: i8x16) -> u8x16

pub fn popcount_u32(value: u32) -> u32
pub fn popcount_u64(value: u64) -> u64
pub fn popcount_u8x16(value: u8x16) -> u8x16

pub fn reduce_add_f32x4(v: f32x4) -> f32
pub fn reduce_add_f64x2(v: f64x2) -> f64
pub fn reduce_add_i32x4(v: i32x4) -> i32
```

- `fma_*` は単一丸め (`a * b + c`) を行います。LLVM `--backend llvm --opt --cpu native`
  では ARM64 `fmla` / x86-64 `vfmadd*` に落ちます。
- `sdot_i8x16` は各 lane `k ∈ 0..4` に対して `acc[k] + Σ_{j=0..3} i32(a[4k+j]) * i32(b[4k+j])`
  を計算します。ARM64 macOS や `--cpu native` (dotprod 対応 CPU) では NEON `sdot`、
  x86-64 AVX2 では `vpmovsxbw` + `vpmaddwd` に落ちます。
- `i32x4_from_f32x4` は飽和切り捨て (`llvm.fptosi.sat.v4i32.v4f32`) を行います。
- `reduce_add_f32x4` は `(v[0] + v[1]) + (v[2] + v[3])` の決定論的な順序で加算します。

### 連続 slice 内積カーネル

```kizu
pub fn dot_f32(a: []f32, b: []f32) -> f32
pub fn dot_i8_f32(weights: []i8, input: []f32) -> f32
pub fn dot_q8_f32(weights: []u8, input: []f32) -> f32
pub fn dot_i8_i8(a: []i8, b: []i8) -> i32
```

両引数の長さが一致しない場合は panic します。`dot_q8_f32` は 2 の補数符号付き
`i8` を格納した `[]u8` 重み列と `[]f32` 入力の内積を 4x 展開の `f32x4` FMA で
計算します。

## ターゲット CPU 最適化 (`--cpu`)

`kizu build --target native --backend llvm --opt --cpu native` のように `--cpu <name>`
を指定すると、LLVM コード生成と clang にターゲット CPU (ARM64 は `-mcpu=<name>`、
x86-64 は `-march=<name>`) を渡し、NEON dotprod / AVX2 / FMA を有効化します。
既定の IEEE-754 `f32` / `f64` 演算の再現性(暗黙の fast-math なし)は維持されます。

## まだ無いもの

- 256-bit / 512-bit 固定幅 vector 型 (`f32x8` など)
- vector 比較マスク型と lane select
