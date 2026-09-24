// Package stdmath computes sin and cos the way Kizu's std::math does, bit
// for bit, so the Go seed folds `comptime math::cos<f64>(x)` to the value the
// Kizu compiler folds it to and the program computes at run time on the
// same target. It is a port of lib/kizu/std/src/math, not an approximation
// of it: the same reduction, tables, polynomials and rounding order.
//
// The std answers differ between targets. With a fused multiply-add
// instruction (native) a product and a sum round once; without one (wasm)
// they round in turn, and near a zero of the answer the kernel takes another
// route. Native says which.
//
// Go may fuse x*y + z on its own; every product that must round on its own
// is written float64(x * y), which the language defines to round.
package stdmath

import (
	"math"
	"math/bits"
)

// Sqrt is the correctly rounded square root, which every target computes.
func Sqrt(x float64) float64 {
	return math.Sqrt(x)
}

// Sin is std::math::sin<f64> on a target with or without a fused
// multiply-add.
func Sin(x float64, native bool) float64 {
	if (math.Float64bits(x)>>52)&0x7ff >= 0x410 {
		return sinHuge(x, native)
	}
	if x == 0 {
		return x
	}
	a := reduceArc(x, native)
	return sinAt(a.k, a.r, a.tail, native)
}

// Cos is std::math::cos<f64> on a target with or without a fused
// multiply-add.
func Cos(x float64, native bool) float64 {
	if (math.Float64bits(x)>>52)&0x7ff >= 0x410 {
		return cosHuge(x, native)
	}
	a := reduceArc(x, native)
	return cosAt(a.k, a.r, a.tail, native)
}

// fmaFast is std's fma_fast: one rounding on native, two on wasm.
func fmaFast(x, y, z float64, native bool) float64 {
	if native {
		return math.FMA(x, y, z)
	}
	return float64(x*y) + z
}

// arc is an angle taken apart as k pi/64 + (r + tail), k in [0, 128).
type arc struct {
	k       uint64
	r, tail float64
}

// point is sin and cos of k pi/64, each as a double-double.
type point struct {
	sinHi, sinLo, cosHi, cosLo float64
}

// pointOf reads sin and cos of the point k pi/64 off the tables.
func pointOf(k uint64) point {
	i := k & 127
	return point{
		sinHi: math.Float64frombits(circleSin[i]),
		sinLo: math.Float64frombits(circleSinTail[i]),
		cosHi: math.Float64frombits(circleCos[i]),
		cosLo: math.Float64frombits(circleCosTail[i]),
	}
}

// polynomials returns sin(r) - r and cos(r) - 1 as the kernels spell them.
func polynomials(r float64, native bool) (p, q float64) {
	z := float64(r * r)
	z2 := float64(z * z)
	p = float64(float64(r*z) * fmaFast(z2, -0.0001984126984126984,
		fmaFast(z, 0.008333333333333333, -0.16666666666666666, native), native))
	q = float64(z * fmaFast(z2, fmaFast(z, 2.48015873015873e-05, -0.001388888888888889, native),
		fmaFast(z, 0.041666666666666664, -0.5, native), native))
	return p, q
}

// sinAt is the sine of k pi/64 + (r + tail).
func sinAt(k uint64, r, tail float64, native bool) float64 {
	if !native && (k+3)&63 < 7 {
		return sinNearZero(k, r, tail, 0, native)
	}
	p, q := polynomials(r, native)
	pt := p + tail
	pnt := pointOf(k)
	inner := fmaFast(pnt.cosHi, pt, fmaFast(pnt.cosLo, r, pnt.sinLo, native), native)
	small := fmaFast(pnt.sinHi, q, inner, native)
	return sumOf(pnt.sinHi, pnt.cosHi, r, small, native)
}

// cosAt is the cosine of k pi/64 + (r + tail).
func cosAt(k uint64, r, tail float64, native bool) float64 {
	if !native && (k+3-32)&63 < 7 {
		return sinNearZero(k, r, tail, 32, native)
	}
	p, q := polynomials(r, native)
	pt := p + tail
	pnt := pointOf(k)
	inner := fmaFast(-pnt.sinHi, pt, fmaFast(-pnt.sinLo, r, pnt.cosLo, native), native)
	small := fmaFast(pnt.cosHi, q, inner, native)
	return sumOf(pnt.cosHi, -pnt.sinHi, r, small, native)
}

// sumOf is big + m r + small, rounded once when native.
func sumOf(big, m, r, small float64, native bool) float64 {
	if native {
		product := float64(m * r)
		productLo := fmaFast(m, r, -product, native)
		sum := big + product
		sumLo := (big - sum) + product
		return sum + ((sumLo + productLo) + small)
	}
	return big + fmaFast(m, r, small, native)
}

// sinNearZero is the sine of an angle within three points of a zero of
// sin (zeroK 0) or of cos (zeroK 32), from the distance to that zero.
func sinNearZero(k uint64, r, tail float64, zeroK uint64, native bool) float64 {
	shifted := (k + 3 - zeroK) & 127
	d := int64(shifted) - 3
	negative := zeroK == 32
	if shifted >= 64 {
		d -= 64
		negative = !negative
	}
	steps := float64(d)
	big := float64(steps * 0.04908738521234052)
	t := big + r
	tLo := (big - t) + r + fmaFast(steps, 1.9135106236677394e-18, tail, native)
	z := float64(t * t)
	z2 := float64(z * z)
	odd := fmaFast(z2, fmaFast(z2, -2.505210838544172e-08,
		fmaFast(z, 2.7557319223985893e-06, -0.0001984126984126984, native), native),
		fmaFast(z, 0.008333333333333333, -0.16666666666666666, native), native)
	y := t + fmaFast(float64(t*z), odd, tLo, native)
	if negative {
		return -y
	}
	return y
}

// reduceArc takes a value below 2^17 apart as k pi/64 + (r + tail).
func reduceArc(x float64, native bool) arc {
	const shift = 6755399441055744.0
	kd := fmaFast(x, 20.371832715762604, shift, native)
	k := math.Float64bits(kd) & 127
	steps := kd - shift
	r := fmaFast(-steps, 0.049087385239545256, x, native)
	w := float64(steps * -2.720473654845052e-11)
	lo := r - w
	exponent := int64((math.Float64bits(x) >> 52) & 0x7ff)
	lost := exponent - int64((math.Float64bits(lo)>>52)&0x7ff)
	if lost > 16 {
		t := r
		w = float64(steps * -2.7204736537502286e-11)
		r = t - w
		w = float64(steps*-1.0948232494031031e-20) - ((t - r) - w)
		lo = r - w
		lost = exponent - int64((math.Float64bits(lo)>>52)&0x7ff)
		if lost > 46 {
			t = r
			w = float64(steps * -1.0948232490483806e-20)
			r = t - w
			w = float64(steps*-3.547224564850688e-30) - ((t - r) - w)
			lo = r - w
		}
	}
	return arc{k: k, r: lo, tail: (r - lo) - w}
}

// sinHuge is sin of a value at least 2^17 in magnitude, or not finite.
func sinHuge(x float64, native bool) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	a := arcOf(reduceOctantHuge(math.Abs(x)))
	y := sinAt(a.k, a.r, a.tail, native)
	if x < 0 {
		return -y
	}
	return y
}

// cosHuge is cos of a value at least 2^17 in magnitude, or not finite.
func cosHuge(x float64, native bool) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	a := arcOf(reduceOctantHuge(math.Abs(x)))
	return cosAt(a.k, a.r, a.tail, native)
}

// octant is an angle taken apart as j pi/4 + (z + lo), j even.
type octant struct {
	j     uint64
	z, lo float64
}

// arcOf moves an angle from octants of pi/4 to points of pi/64.
func arcOf(o octant) arc {
	const shift = 6755399441055744.0
	kd := float64(o.z*20.371832715762604) + shift
	steps := kd - shift
	r := o.z - float64(steps*0.049087385239545256)
	w := float64(steps * -2.720473654845052e-11)
	lo := r - w
	k := uint64(int64(o.j*16)+int64(steps)) & 127
	return arc{k: k, r: lo, tail: ((r - lo) - w) + o.lo}
}

// piOver4 is pi/4 rounded, spelled as std spells it.
const piOver4 = 0.78539816339744830961566084581987572104929234984378

// reduceOctantHuge is Payne-Hanek reduction of x >= pi/4 by pi/4.
func reduceOctantHuge(x float64) octant {
	if x < piOver4 {
		return octant{j: 0, z: x}
	}
	j, fhi, flo, negative := octantBits(x)
	if fhi == 0 {
		return octant{j: j}
	}
	lz := uint64(bits.LeadingZeros64(fhi))
	e := uint64(1023) - (lz + 1)
	rest := (fhi << (lz + 1)) | (flo >> (64 - (lz + 1)))
	mantissa := rest >> 12
	dropped := int64(rest & 0xfff)
	if rest&0x800 != 0 {
		mantissa++
		dropped -= 4096
		if mantissa>>52 == 1 {
			mantissa = 0
			e++
		}
	}
	z := math.Float64frombits(mantissa | (e << 52))
	zLo := float64(float64(dropped) * math.Float64frombits((e-64)<<52))
	if negative {
		z = -z
		zLo = -zLo
	}
	zh := math.Float64frombits(math.Float64bits(z) &^ ((uint64(1) << 27) - 1))
	zl := z - zh
	const ph = 0.7853981554508209
	const pl = 7.946627356147928e-9
	angle := float64(z * piOver4)
	tail := ((float64(zh*ph) - angle) + float64(zh*pl) + float64(zl*ph)) + float64(zl*pl) +
		float64(z*3.061616997868383e-17) + float64(zLo*piOver4)
	return octant{j: j, z: angle, lo: tail}
}

// octantBits multiplies x by the digits of 4/pi that matter at its exponent
// and returns the octant j (even) and the fraction as 128 bits, negated and
// flagged when the octant was odd and measured from the next even one.
func octantBits(x float64) (j, fhi, flo uint64, negative bool) {
	ix := math.Float64bits(x)
	exp := int64((ix>>52)&2047) - 1023 - 52
	ix = (ix & 0x000fffffffffffff) | 0x0010000000000000
	digit := uint64((exp + 61) / 64)
	bitshift := uint64((exp + 61) % 64)
	z0 := (fourOverPiDigit(digit) << bitshift) | (fourOverPiDigit(digit+1) >> (64 - bitshift))
	z1 := (fourOverPiDigit(digit+1) << bitshift) | (fourOverPiDigit(digit+2) >> (64 - bitshift))
	z2 := (fourOverPiDigit(digit+2) << bitshift) | (fourOverPiDigit(digit+3) >> (64 - bitshift))
	z2hi, _ := bits.Mul64(z2, ix)
	z1hi, _ := bits.Mul64(z1, ix)
	z1lo := z1 * ix
	z0lo := z0 * ix
	lo := z1lo + z2hi
	var carry uint64
	if lo < z1lo {
		carry = 1
	}
	hi := z0lo + z1hi + carry
	j = hi >> 61
	fhi = (hi << 3) | (lo >> 61)
	flo = lo << 3
	if j&1 == 1 {
		j = (j + 1) & 7
		flo = -flo
		fhi = -fhi
		if flo != 0 {
			fhi--
		}
		negative = true
	}
	return j, fhi, flo, negative
}

// fourOverPiDigit is the index-th 64-bit digit of 4/pi, 0 past the table.
func fourOverPiDigit(index uint64) uint64 {
	if index < uint64(len(fourOverPiDigits)) {
		return fourOverPiDigits[index]
	}
	return 0
}
