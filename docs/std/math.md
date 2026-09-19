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

`cmd/kizu` の `TestMath` が Go の `math` と数千の値で突き合わせ、native と wasm の
両方で同じ bit を返すことを確かめています。

## まだ無いもの

- `pow` / `exp` / `log` / `sin` / `cos` / `tan` / `atan2` などの超越関数
- `fmod` / `rem`
- 偶数丸めの `round_even`、`fma`
- `f32` 版
