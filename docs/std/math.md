# std::math

f64 の初等関数と定数です。

```text
std::math::sqrt(value: f64) -> f64
std::math::floor(value: f64) -> f64
std::math::ceil(value: f64) -> f64
std::math::trunc(value: f64) -> f64
std::math::round(value: f64) -> f64
std::math::abs(value: f64) -> f64
std::math::copysign(magnitude: f64, sign: f64) -> f64
std::math::is_negative(value: f64) -> bool
std::math::min(a: f64, b: f64) -> f64
std::math::max(a: f64, b: f64) -> f64
std::math::clamp(value: f64, low: f64, high: f64) -> f64
std::math::hypot(x: f64, y: f64) -> f64
std::math::exp(value: f64) -> f64
std::math::exp2(value: f64) -> f64
std::math::expm1(value: f64) -> f64
std::math::log(value: f64) -> f64
std::math::log1p(value: f64) -> f64
std::math::log2(value: f64) -> f64
std::math::log10(value: f64) -> f64
std::math::pow(base: f64, exponent: f64) -> f64
std::math::fmod(value: f64, divisor: f64) -> f64
std::math::ldexp(fraction: f64, exponent: i64) -> f64
std::math::pi() -> f64
std::math::tau() -> f64
std::math::e() -> f64
std::math::infinity() -> f64
std::math::nan() -> f64
```

`sqrt` / `floor` / `ceil` / `trunc` は IEEE 754 が答えを 1 つに定める演算で、
trusted primitive です。backend はそれぞれを 1 命令(LLVM intrinsic、wasm の
`f64.sqrt` など)で出すので、正しく丸めた値がどの target でも同じ bit で返ります。
`sqrt` は負の値に NaN を、`-0.0` に `-0.0` を返します。

残りはこの 4 つと `std::float::bits` の上に Kizu で書いてあり、同じく target に
よりません。

- `round` は最も近い整数値で、半分は零から遠い側へ丸めます(`round(0.5)` は
  `1.0`、`round(-0.5)` は `-1.0`)。`0.49999999999999994` は `0.0` です。
- `abs` は符号 bit を消し、`copysign` は `sign` の符号 bit を `magnitude` に
  付けます。どちらも NaN を NaN のまま通します。
- `is_negative` は符号 bit を見るので、`-0.0` は負です。
- `min` / `max` は片方が NaN ならもう片方を返し、`-0.0` を `0.0` より小さいと
  扱います。C の `fmin` / `fmax` と同じで、Go の `math.Min` とは NaN の扱いが
  違います。
- `clamp` は NaN をそのまま返します。
- `hypot` は大きい方の絶対値で括り出してから平方根を取るので、`3e200` と
  `4e200` の二乗が溢れても `5e200` に近い値を返します。無限大が 1 つでもあれば、
  もう片方が NaN でも無限大です。
- `nan()` は符号 bit の立たない quiet NaN、`infinity()` は正の無限大です。

指数と対数は FreeBSD msun の算法(Go の `math` と同じ)を Kizu で書いたもので、
引数を 2 つに分けた ln 2 で小さな範囲に落とし、そこで短い有理式を評価して、2 の
べきで戻します。真の値から 1 ulp 以内で、これも target によりません。

- `exp` は 709.78 あたりを超えると無限大、-745.13 あたりを下回ると 0 です。
  `exp2` は同じ形で 2 のべき。
- `expm1` / `log1p` は `exp(x) - 1` / `log(1 + x)` を、引き算や足し算が桁を
  落とす小さな `x` でも正しく返します(`expm1(1e-10)` は `1.00000000005e-10`)。
- `log` は負の値に NaN、0 に負の無限大。`log2` は 2 のべきに正確な整数を返します。
- `pow` は C の `pow` の特別扱いをそのまま持ちます: `pow(x, 0)` と `pow(1, y)` は
  常に 1、負の底に整数でない指数は NaN、0 と無限大は指数の符号と偶奇に従います。
- `fmod` は C の `fmod` で、結果は `value` の符号を持ち、大きさは `|divisor|`
  未満です(`fmod(-5.5, 2.0)` は `-1.5`)。0 で割ると NaN です。
- `ldexp` は `fraction * 2^exponent` で、範囲を超えれば無限大か 0、正規化数の
  下は 1 回だけ丸めて非正規化数にします。

`cmd/kizu` の `TestMath` が Go の `math` と数千の値で突き合わせ、native と wasm の
両方で同じ bit を返すことを確かめています。IEEE の演算と bit 操作は Go と一致し、
残りは 1 ulp 以内です(Go は arm64 で乗算と加算を 1 回の丸めに融合するので、
最後の bit が動くことがあります)。

## まだ無いもの

- `sin` / `cos` / `tan` / `asin` / `acos` / `atan` / `atan2`、双曲線関数
- `frexp` / `modf`(値を 2 つ返す形が決まってから)
- 偶数丸めの `round_even`、`fma`、`cbrt`
- `f32` 版
