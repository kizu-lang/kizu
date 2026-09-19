# std::math

初等関数と定数です。`T` は `f32` か `f64` で、`math::sin<f64>(x)` のように static
引数で選びます(`array::new<T>` と同じ綴り)。それ以外の型は body で type error に
なります。

```text
std::math::sqrt<T>(value: T) -> T
std::math::floor<T>(value: T) -> T
std::math::ceil<T>(value: T) -> T
std::math::trunc<T>(value: T) -> T
std::math::round<T>(value: T) -> T
std::math::abs<T>(value: T) -> T
std::math::copysign<T>(magnitude: T, sign: T) -> T
std::math::is_negative<T>(value: T) -> bool
std::math::min<T>(a: T, b: T) -> T
std::math::max<T>(a: T, b: T) -> T
std::math::clamp<T>(value: T, low: T, high: T) -> T
std::math::hypot<T>(x: T, y: T) -> T
std::math::exp<T>(value: T) -> T
std::math::exp2<T>(value: T) -> T
std::math::expm1<T>(value: T) -> T
std::math::log<T>(value: T) -> T
std::math::log1p<T>(value: T) -> T
std::math::log2<T>(value: T) -> T
std::math::log10<T>(value: T) -> T
std::math::pow<T>(base: T, exponent: T) -> T
std::math::fmod<T>(value: T, divisor: T) -> T
std::math::ldexp<T>(fraction: T, exponent: i64) -> T
std::math::sin<T>(value: T) -> T
std::math::cos<T>(value: T) -> T
std::math::tan<T>(value: T) -> T
std::math::asin<T>(value: T) -> T
std::math::acos<T>(value: T) -> T
std::math::atan<T>(value: T) -> T
std::math::atan2<T>(y: T, x: T) -> T
std::math::sinh<T>(value: T) -> T
std::math::cosh<T>(value: T) -> T
std::math::tanh<T>(value: T) -> T
std::math::asinh<T>(value: T) -> T
std::math::acosh<T>(value: T) -> T
std::math::atanh<T>(value: T) -> T
std::math::cbrt<T>(value: T) -> T
std::math::round_even<T>(value: T) -> T
std::math::fma<T>(x: T, y: T, z: T) -> T
std::math::frexp<T>(value: T) -> Scaled      // { fraction: f64, exponent: i64 }
std::math::modf<T>(value: T) -> Parts        // { whole: f64, fraction: f64 }
std::math::pi<T>() -> T
std::math::tau<T>() -> T
std::math::e<T>() -> T
std::math::infinity<T>() -> T
std::math::nan<T>() -> T
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

三角関数は Cephes の算法(これも Go と同じ)です。引数が 2^29 未満なら 3 つに
分けた pi/4 を引いて円の 8 分の 1 に落とし、それ以上なら 4/pi を 1217 bit 持った
Payne–Hanek reduction で落とすので、`sin(1e22)` も `sin(1e300)` も正しい値です。

- `sin` / `cos` / `tan` は radian を取り、無限大に NaN を返します。
- `asin` / `acos` は [-1, 1] の外に NaN、`atan` は [-pi/2, pi/2] を返します。
- `atan2(y, x)` は点 (x, y) の角度を (-pi, pi] で返し、象限を両方の符号から
  決めます(`atan2(1, -1)` は 3pi/4)。0 と無限大の組は C の `atan2` と同じです。

双曲線関数は `exp` / `log1p` の上に書いた Cephes / FreeBSD の式です。`acosh` は 1
未満に NaN、`atanh` は [-1, 1] の外に NaN、両端に無限大を返します。`cbrt` は符号を
保ち(`cbrt(-8.0)` は `-2.0`)、0.667 ulp 以内です。

- `round_even` は最も近い整数値で、半分は偶数側へ丸めます(`round_even(0.5)` は
  `0.0`、`round_even(2.5)` は `2.0`)。`round` との違いはこの半分の扱いだけです。
- `fma(x, y, z)` は `x * y + z` を 1 回の丸めで返します。書き下した `x * y + z`
  は 2 回丸めるので、`fma(0.1, 10.0, -1.0)` は `0.1 * 10.0` の誤差
  `5.551115123125783e-17` を返し、書き下した式は `0.0` です。積を 128 bit で
  正確に作り、`z` を揃えて 1 度だけ偶数丸めするので、hardware の fma と同じ bit
  です。
- `frexp` は `fraction * 2^exponent`(fraction は [1/2, 1))に、`modf` は整数部と
  小数部(どちらも元の符号)に分け、struct で 2 つの値を返します。

`cmd/kizu` の `TestMath` が Go の `math` と数千の値で突き合わせ、native と wasm の
両方で同じ bit を返すことを確かめています。IEEE の演算と bit 操作、`fma`、
`round_even`、`frexp` / `modf` は Go と一致し、残りは 1 ulp 以内(`tan` は逆数を
取るので 2 ulp)です。Go は arm64 で乗算と加算を 1 回の丸めに融合するので、最後の
bit が動くことがあります。

## f32

`f32` は f64 で計算して 1 回 `cast<f32>` で戻します。`sqrt` / `floor` / `ceil` /
`trunc` / `round` / `round_even` / `fma` はこの 2 段の丸めでも正しく丸まった `f32`
です(f64 の 53 bit は f32 の 24 bit の 2 倍と 2 bit 以上あるため)。残りは f64 で
1 ulp 以内の値を丸めるので、`f32` で 1 ulp を超えることは事実上ありません。
`frexp` / `modf` は `T` によらず f64 の部品を返します。`f32` はそこに正確に収まります。
単精度専用の速い算法は、それを要る利用者が現れてから差し替えます。API は変わりません。
