package morse

// A tiny, fast, deterministic RNG that replaces math/rand.
// Produces uniform floats and ints without pulling in the heavy rand package.

var yrandSeed uint64 = 88172645463325252

// fastRandUint32 returns a 32-bit random number.
func fastRandUint32() uint32 {
	yrandSeed = yrandSeed*6364136223846793005 + 1
	return uint32(yrandSeed >> 32)
}

// FastIntn returns an integer in [0, n).
func FastIntn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(fastRandUint32() % uint32(n))
}

// FastFloat32 returns a float32 in [0, 1).
func FastFloat32() float32 {
	return float32(fastRandUint32()) / 4294967296.0
}

// FastFloat64 returns a float64 in [0, 1).
func FastFloat64() float64 {
	return float64(fastRandUint32()) / 4294967296.0
}

// FastFloatSigned32 returns a float32 in [-1, +1].
func FastFloatSigned32() float32 {
	return (FastFloat32() * 2.0) - 1.0
}

// FastFloatSigned64 returns a float64 in [-1, +1].
func FastFloatSigned64() float64 {
	return (FastFloat64() * 2.0) - 1.0
}
