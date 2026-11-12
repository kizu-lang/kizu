"""Write std_math.txt, the reference TestMath checks std::math with.

Run from the repository root with mpmath (nixpkgs python3Packages.mpmath):

    python3 cmd/kizu/testdata/std_math.py > cmd/kizu/testdata/std_math.txt

Every answer is the true value of the function rounded once to the nearest
double (ties to even), or to the nearest float for the `s` lines. mpmath
evaluates each at 200 and 400 bits and the two have to round to the same
double; where they do not, the precision doubles until two in a row agree.
The operations IEEE 754 defines exactly (floor, fma, fmod and the like) are
worked out in exact rationals instead. The answers at zeros, infinities and
NaN, and outside a function's domain, are C99 Annex F's, which
docs/std/math.md follows; where Annex F leaves one open, the comment at that
case says what docs/std/math.md promises instead.
"""

from fractions import Fraction
import struct
import sys

# float_text.py is imported for its values; leave no __pycache__ in testdata.
sys.dont_write_bytecode = True

import mpmath
from mpmath import mp, mpf

from float_text import bit_patterns

INF = float("inf")
NAN = float("nan")


def from_bits(raw):
    return struct.unpack("<d", struct.pack("<Q", raw))[0]


def to_bits(value):
    return struct.unpack("<Q", struct.pack("<d", value))[0]


def from_bits32(raw):
    return struct.unpack("<f", struct.pack("<I", raw))[0]


def to_bits32(value):
    return struct.unpack("<I", struct.pack("<f", value))[0]


def negative(value):
    return to_bits(value) >> 63 == 1


def is_nan(value):
    return value != value


def is_inf(value):
    return value in (INF, -INF)


# The binary formats: bits of significand, smallest exponent of a normal
# number, largest exponent.
DOUBLE = (53, -1022, 1023)
SINGLE = (24, -126, 127)

# The format answers are rounded to: DOUBLE but while the `s` lines are made.
FORM = DOUBLE


def round_rational(value):
    """Round an exact rational to the nearest value of FORM, ties to even."""
    precision, emin, emax = FORM
    if value == 0:
        return 0.0
    sign = -1.0 if value < 0 else 1.0
    value = abs(Fraction(value))
    # exponent = floor(log2(value))
    exponent = value.numerator.bit_length() - value.denominator.bit_length()
    if Fraction(2) ** exponent > value:
        exponent -= 1
    quantum = max(exponent, emin) - (precision - 1)
    scaled = value / Fraction(2) ** quantum
    whole = scaled.numerator // scaled.denominator
    rest = scaled - whole
    if rest > Fraction(1, 2) or (rest == Fraction(1, 2) and whole % 2 == 1):
        whole += 1
    if whole * Fraction(2) ** quantum >= Fraction(2) ** (emax + 1):
        return sign * INF
    # whole has at most `precision` bits, so the product is exact in Python's
    # double (which holds both formats).
    return sign * float(whole * Fraction(2) ** quantum)


def to_rational(value):
    """The exact rational value of a finite mpf."""
    sign, man, exp, _ = value._mpf_
    result = Fraction(int(man)) * Fraction(2) ** int(exp)
    return -result if sign else result


def rounded(function):
    """Round function() -- an mpf expression -- correctly, Ziv's way."""
    precision = 200
    previous = None
    while True:
        with mp.workprec(precision):
            value = function()
        if isinstance(value, mpmath.mpc):
            raise ValueError("complex answer")
        if mpmath.isinf(value):
            answer = INF if value > 0 else -INF
        elif value == 0:
            answer = 0.0
        else:
            answer = round_rational(to_rational(value))
        if previous is not None and to_bits(previous) == to_bits(answer):
            return answer
        previous = answer
        precision *= 2
        if precision > 1 << 16:
            raise ValueError("no agreement")


def is_integer(value):
    return not is_inf(value) and value == int(value)


def is_odd_integer(value):
    return is_integer(value) and abs(value) < 2.0**53 and int(value) % 2 == 1


def with_sign(magnitude, negative_sign):
    return -magnitude if negative_sign else magnitude


# --- the exactly defined operations -------------------------------------


def floor_(x):
    if is_nan(x) or is_inf(x) or x == 0:
        return x
    whole = Fraction(x).numerator // Fraction(x).denominator
    return with_sign(abs(float(whole)), negative(x)) if whole == 0 else float(whole)


def ceil_(x):
    if is_nan(x) or is_inf(x) or x == 0:
        return x
    whole = -((-Fraction(x)).numerator // (-Fraction(x)).denominator)
    return with_sign(0.0, negative(x)) if whole == 0 else float(whole)


def trunc_(x):
    if is_nan(x) or is_inf(x) or x == 0:
        return x
    whole = int(Fraction(x))
    return with_sign(0.0, negative(x)) if whole == 0 else float(whole)


def round_away(x):
    if is_nan(x) or is_inf(x) or x == 0:
        return x
    magnitude = abs(Fraction(x))
    whole = int(magnitude)
    if magnitude - whole >= Fraction(1, 2):
        whole += 1
    return with_sign(float(whole), negative(x))


def round_even(x):
    if is_nan(x) or is_inf(x) or x == 0:
        return x
    magnitude = abs(Fraction(x))
    whole = int(magnitude)
    rest = magnitude - whole
    if rest > Fraction(1, 2) or (rest == Fraction(1, 2) and whole % 2 == 1):
        whole += 1
    return with_sign(float(whole), negative(x))


def abs_(x):
    return from_bits(to_bits(x) & ~(1 << 63))


def copysign_(magnitude, sign):
    return from_bits((to_bits(magnitude) & ~(1 << 63)) | (to_bits(sign) & (1 << 63)))


def sqrt_(x):
    if is_nan(x) or x == 0 or x == INF:
        return x
    if x < 0:
        return NAN
    return rounded(lambda: mpmath.sqrt(mpf(x)))


def frexp_(x):
    """fraction in [1/2, 1) with x's sign, and the exponent. docs/std/math.md:
    `frexp` is `fraction * 2^exponent`. Annex F gives the fraction at zero,
    infinity and NaN (the value itself) and leaves the exponent open there;
    an exponent of 0 is the one that keeps `fraction * 2^exponent` the value."""
    if is_nan(x) or is_inf(x) or x == 0:
        return x, 0
    magnitude = abs(Fraction(x))
    exponent = magnitude.numerator.bit_length() - magnitude.denominator.bit_length() + 1
    fraction = magnitude / Fraction(2) ** exponent
    if fraction >= 1:
        exponent += 1
        fraction /= 2
    elif fraction < Fraction(1, 2):
        exponent -= 1
        fraction *= 2
    return with_sign(float(fraction), negative(x)), exponent


def modf_(x):
    """whole and fractional parts, each with x's sign (docs/std/math.md).
    Annex F F.9.3.12: modf(+-inf) is +-inf with a fraction of +-0."""
    if is_nan(x):
        return x, x
    if is_inf(x):
        return x, with_sign(0.0, negative(x))
    whole = trunc_(x)
    fraction = Fraction(x) - Fraction(whole)
    return whole, with_sign(abs(float(fraction)), negative(x))


def fmod_(x, y):
    """C's fmod (docs/std/math.md), F.9.7.1: exact, with x's sign."""
    if is_nan(x) or is_nan(y) or is_inf(x) or y == 0:
        return NAN
    if is_inf(y):
        return x
    if x == 0:
        return x
    a, b = Fraction(x), abs(Fraction(y))
    rest = abs(a) - b * int(abs(a) / b)
    return with_sign(float(rest), negative(x))


def fmin_(a, b):
    """C's fmin; -0 is below +0 (docs/std/math.md)."""
    if is_nan(a):
        return b
    if is_nan(b):
        return a
    if a == b == 0:
        return a if negative(a) else b
    return a if a < b else b


def fmax_(a, b):
    if is_nan(a):
        return b
    if is_nan(b):
        return a
    if a == b == 0:
        return b if negative(a) else a
    return a if a > b else b


def fma_(x, y, z):
    if is_nan(x) or is_nan(y) or is_nan(z):
        return NAN
    if (is_inf(x) and y == 0) or (is_inf(y) and x == 0):
        return NAN
    if is_inf(x) or is_inf(y):
        product = INF if negative(x) == negative(y) else -INF
        if is_inf(z) and z != product:
            return NAN
        return product
    if is_inf(z):
        return z
    exact = Fraction(x) * Fraction(y) + Fraction(z)
    if exact == 0:
        # IEEE 754 6.3: an exact zero sum is -0 only when both addends are.
        product_negative = negative(x) != negative(y)
        if Fraction(x) * Fraction(y) == 0 and z == 0:
            return -0.0 if product_negative and negative(z) else 0.0
        return 0.0
    return round_rational(exact)


def hypot_(x, y):
    if is_inf(x) or is_inf(y):
        return INF
    if is_nan(x) or is_nan(y):
        return NAN
    exact = Fraction(x) ** 2 + Fraction(y) ** 2
    if exact == 0:
        return 0.0
    return rounded(lambda: mpmath.sqrt(mpf(x) ** 2 + mpf(y) ** 2))


# --- the transcendental functions ----------------------------------------


def exp_(x):
    if is_nan(x):
        return x
    if x == INF:
        return INF
    if x == -INF:
        return 0.0
    if x > 1000:
        return INF
    if x < -1100:
        return 0.0
    return rounded(lambda: mpmath.exp(mpf(x)))


def exp2_(x):
    if is_nan(x):
        return x
    if x == INF:
        return INF
    if x == -INF:
        return 0.0
    if x > 1100:
        return INF
    if x < -1200:
        return 0.0
    return rounded(lambda: mpmath.power(2, mpf(x)))


def expm1_(x):
    if is_nan(x) or x == 0:
        return x
    if x == INF:
        return INF
    if x == -INF:
        return -1.0
    if x > 1000:
        return INF
    if x < -100:
        return -1.0
    return rounded(lambda: mpmath.expm1(mpf(x)))


def log_with(base):
    def log(x):
        if is_nan(x):
            return x
        if x == 0:
            return -INF
        if x < 0:
            return NAN
        if x == INF:
            return INF
        if x == 1:
            return 0.0
        if base is None:
            return rounded(lambda: mpmath.log(mpf(x)))
        return rounded(lambda: mpmath.log(mpf(x)) / mpmath.log(base))

    return log


def log1p_(x):
    if is_nan(x) or x == 0:
        return x
    if x == -1:
        return -INF
    if x < -1:
        return NAN
    if x == INF:
        return INF
    return rounded(lambda: mpmath.log1p(mpf(x)))


def trig(function, odd):
    def evaluate(x):
        if is_nan(x) or is_inf(x):
            return NAN
        if x == 0:
            return x if odd else 1.0
        return rounded(lambda: function(mpf(x)))

    return evaluate


def asin_(x):
    if is_nan(x) or x == 0:
        return x
    if abs(x) > 1:
        return NAN
    return rounded(lambda: mpmath.asin(mpf(x)))


def acos_(x):
    if is_nan(x) or abs(x) > 1:
        return NAN
    if x == 1:
        return 0.0
    return rounded(lambda: mpmath.acos(mpf(x)))


def atan_(x):
    if is_nan(x) or x == 0:
        return x
    if is_inf(x):
        return with_sign(rounded(lambda: mpmath.pi / 2), x < 0)
    return rounded(lambda: mpmath.atan(mpf(x)))


def sinh_(x):
    if is_nan(x) or x == 0 or is_inf(x):
        return x
    if abs(x) > 1000:
        return with_sign(INF, x < 0)
    return rounded(lambda: mpmath.sinh(mpf(x)))


def cosh_(x):
    if is_nan(x):
        return x
    if is_inf(x) or abs(x) > 1000:
        return INF
    return rounded(lambda: mpmath.cosh(mpf(x)))


def tanh_(x):
    if is_nan(x) or x == 0:
        return x
    if is_inf(x) or abs(x) > 50:
        # 1 - tanh(50) is below 2^-140, far under half an ulp of 1.
        return with_sign(1.0, x < 0)
    return rounded(lambda: mpmath.tanh(mpf(x)))


def asinh_(x):
    if is_nan(x) or x == 0 or is_inf(x):
        return x
    return rounded(lambda: mpmath.asinh(mpf(x)))


def acosh_(x):
    if is_nan(x) or x < 1:
        return NAN
    if x == 1:
        return 0.0
    if x == INF:
        return INF
    return rounded(lambda: mpmath.acosh(mpf(x)))


def atanh_(x):
    if is_nan(x) or x == 0:
        return x
    if abs(x) > 1:
        return NAN
    if abs(x) == 1:
        return with_sign(INF, x < 0)
    return rounded(lambda: mpmath.atanh(mpf(x)))


def cbrt_(x):
    if is_nan(x) or x == 0 or is_inf(x):
        return x
    return with_sign(rounded(lambda: mpmath.cbrt(abs(mpf(x)))), x < 0)


def pow_(x, y):
    """C's pow (docs/std/math.md), Annex F F.9.4.4."""
    if y == 0:
        return 1.0
    if x == 1:
        return 1.0
    if is_nan(x) or is_nan(y):
        return NAN
    odd = is_odd_integer(y)
    if x == 0:
        if y < 0:
            return with_sign(INF, odd and negative(x))
        return with_sign(0.0, odd and negative(x))
    if is_inf(y):
        if x == -1:
            return 1.0
        if (abs(x) < 1) == (y < 0):
            return INF
        return 0.0
    if is_inf(x):
        if x < 0:
            if y < 0:
                return with_sign(0.0, odd)
            return with_sign(INF, odd)
        return 0.0 if y < 0 else INF
    if x < 0 and not is_integer(y):
        return NAN
    sign = x < 0 and odd
    base = abs(x)
    scale = float(mpmath.log(mpf(base), 2) * mpf(y))
    if scale > 1100:
        return with_sign(INF, sign)
    if scale < -1200:
        return with_sign(0.0, sign)
    if is_integer(y) and abs(y) < 64:
        return with_sign(round_rational(Fraction(base) ** int(y)), sign)
    return with_sign(rounded(lambda: mpmath.power(mpf(base), mpf(y))), sign)


def atan2_(y, x):
    """C's atan2 (docs/std/math.md), Annex F F.9.1.4."""
    if is_nan(x) or is_nan(y):
        return NAN
    pi = rounded(lambda: mpmath.pi)
    half = rounded(lambda: mpmath.pi / 2)
    ynegative = negative(y)
    if y == 0:
        if negative(x) and (x < 0 or x == 0):
            return with_sign(pi, ynegative)
        return with_sign(0.0, ynegative)
    if x == 0:
        return with_sign(half, ynegative)
    if is_inf(x):
        if is_inf(y):
            if x < 0:
                return with_sign(rounded(lambda: 3 * mpmath.pi / 4), ynegative)
            return with_sign(rounded(lambda: mpmath.pi / 4), ynegative)
        return with_sign(pi if x < 0 else 0.0, ynegative)
    if is_inf(y):
        return with_sign(half, ynegative)
    return rounded(lambda: mpmath.atan2(mpf(y), mpf(x)))


UNARY = [
    ("sqrt", sqrt_),
    ("floor", floor_),
    ("ceil", ceil_),
    ("trunc", trunc_),
    ("round", round_away),
    ("abs", abs_),
    ("exp", exp_),
    ("exp2", exp2_),
    ("expm1", expm1_),
    ("log", log_with(None)),
    ("log1p", log1p_),
    ("log2", log_with(2)),
    ("log10", log_with(10)),
    ("sin", trig(mpmath.sin, True)),
    ("cos", trig(mpmath.cos, False)),
    ("sin_fused", trig(mpmath.sin, True)),
    ("cos_fused", trig(mpmath.cos, False)),
    ("exp_fused", exp_),
    ("log_fused", log_with(None)),
    ("tan", trig(mpmath.tan, True)),
    ("asin", asin_),
    ("acos", acos_),
    ("atan", atan_),
    ("sinh", sinh_),
    ("cosh", cosh_),
    ("tanh", tanh_),
    ("asinh", asinh_),
    ("acosh", acosh_),
    ("atanh", atanh_),
    ("cbrt", cbrt_),
    ("round_even", round_even),
    ("frexp_fraction", lambda x: frexp_(x)[0]),
    ("frexp_exponent", lambda x: float(frexp_(x)[1])),
    ("modf_whole", lambda x: modf_(x)[0]),
    ("modf_fraction", lambda x: modf_(x)[1]),
]

BINARY = [
    ("copysign", copysign_),
    ("hypot", hypot_),
    ("min", fmin_),
    ("max", fmax_),
    ("pow", pow_),
    ("pow_fused", pow_),
    ("fmod", fmod_),
    ("atan2", atan2_),
]

TERNARY = [("fma", fma_)]

# The functions a program calls on f32 directly: the unary ones above but the
# helpers the test program defines for the f64 path.
SINGLE_UNARY = [
    name for name, _ in UNARY
    if not name.endswith("_fused") and not name.startswith(("frexp", "modf"))
]


def math_values():
    """The float text patterns plus the halves and near-halves rounding is
    about, values near the edges of exp and pow, multiples of pi, both
    infinities, and NaN."""
    bits = bit_patterns()
    pi = mpmath.pi
    with mp.workprec(200):
        multiples = [round_rational(to_rational(+(k * pi / d))) for k, d in
                     [(1, 1), (-1, 1), (1, 2), (1, 4), (3, 4), (2, 1)]]
    values = [
        0.5, -0.5, 1.5, -1.5, 2.5, -2.5, 0.49999999999999994, -0.49999999999999994,
        4503599627370495.5, -4503599627370495.5, 4503599627370496, 9007199254740993,
        2, 3, 10, 0.001, 100.5, 1e-300, 1e300, 700, 709.7, 709.8, -745, -745.2,
        1023.5, 1024, -1074, -1074.5, 1e-10, -1e-10, 0.25, -0.75, 6.5, -7,
    ] + multiples + [
        0.66, 0.7, 2.5,
        536870911, 536870912, 536870913, 1e15, 1e22, 1e50, 1e100, 1e200, 1e300,
        0.1, 10, -1, 1e308, 1e-308, 4.9e-324, 3, 4, 0.5000000000000001, 21.5, 22, 0.625, 0.7,
    ]
    bits += [to_bits(float(value)) for value in values]
    bits += [to_bits(INF), to_bits(-INF), 0x7FF8000000000001]
    return bits


def single_values():
    """The f32 values: ordinary magnitudes, the edges of the format, and the
    specials, each the f32 nearest the decimal."""
    global FORM
    FORM = SINGLE
    texts = ["0.5", "-0.5", "1", "-1", "2", "3", "0.1", "0.7", "2.5", "-2.5", "100", "1e30",
             "1e-30", "0.7853982", "3.1415927", "1e-45", "3.4028235e38"]
    values = [0.0, -0.0] + [round_rational(Fraction(text)) for text in texts] + [INF, -INF]
    FORM = DOUBLE
    return [to_bits32(value) for value in values] + [0x7FC00000]


def hex64(value):
    return f"{to_bits(value):016X}"


def hex32(value):
    return f"{to_bits32(value):08X}"


def main():
    mp.prec = 200
    bits = math_values()
    values = [from_bits(raw) for raw in bits]
    print("# Generated by cmd/kizu/testdata/std_math.py; do not edit.")
    print("# u <x> <f(x) for each unary function>, b <x> <y> <f(x, y) for each binary")
    print("# function>, t <x> <y> <z> <fma>, s <f32 x> <f(x) as f32 for each single")
    print("# function>, all as IEEE 754 bits in hex.")
    print("unary " + " ".join(name for name, _ in UNARY))
    print("binary " + " ".join(name for name, _ in BINARY))
    print("ternary " + " ".join(name for name, _ in TERNARY))
    print("single " + " ".join(SINGLE_UNARY))
    for x in values:
        print("u " + hex64(x) + " " + " ".join(hex64(f(x)) for _, f in UNARY))
    for x, y in zip(values, values[1:]):
        print("b " + hex64(x) + " " + hex64(y) + " " + " ".join(hex64(f(x, y)) for _, f in BINARY))
    for x, y, z in zip(values, values[1:], values[2:]):
        print("t " + " ".join(hex64(v) for v in (x, y, z)) + " " +
              " ".join(hex64(f(x, y, z)) for _, f in TERNARY))
    global FORM
    singles = single_values()
    FORM = SINGLE
    functions = dict(UNARY)
    for raw in singles:
        x = from_bits32(raw)
        print(f"s {raw:08X} " + " ".join(hex32(functions[name](x)) for name in SINGLE_UNARY))


if __name__ == "__main__":
    main()
