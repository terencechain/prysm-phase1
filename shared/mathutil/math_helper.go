// Package mathutil includes important helpers for eth2 such as fast integer square roots.
package mathutil

import (
	"errors"
	"math"
	"math/bits"
)

// Common square root values.
var squareRootTable = map[uint64]uint64{
	4:       2,
	16:      4,
	64:      8,
	256:     16,
	1024:    32,
	4096:    64,
	16384:   128,
	65536:   256,
	262144:  512,
	1048576: 1024,
	4194304: 2048,
}

// IntegerSquareRoot defines a function that returns the
// largest possible integer root of a number using go's standard library.
func IntegerSquareRoot(n uint64) uint64 {
	if v, ok := squareRootTable[n]; ok {
		return v
	}

	return uint64(math.Sqrt(float64(n)))
}

// CeilDiv8 divides the input number by 8
// and takes the ceiling of that number.
func CeilDiv8(n int) int {
	ret := n / 8
	if n%8 > 0 {
		ret++
	}

	return ret
}

// IsPowerOf2 returns true if n is an
// exact power of two. False otherwise.
func IsPowerOf2(n uint64) bool {
	return n != 0 && (n&(n-1)) == 0
}

// PowerOf2 returns an integer that is the provided
// exponent of 2. Can only return powers of 2 till 63,
// after that it overflows
func PowerOf2(n uint64) uint64 {
	if n >= 64 {
		panic("integer overflow")
	}
	return 1 << n
}

// ClosestPowerOf2 returns an integer that is the closest
// power of 2 that is less than or equal to the argument.
func ClosestPowerOf2(n uint64) uint64 {
	if n == 0 {
		return uint64(1)
	}
	exponent := math.Floor(math.Log2(float64(n)))
	return PowerOf2(uint64(exponent))
}

// Max returns the larger integer of the two
// given ones.This is used over the Max function
// in the standard math library because that max function
// has to check for some special floating point cases
// making it slower by a magnitude of 10.
func Max(a uint64, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// Min returns the smaller integer of the two
// given ones. This is used over the Min function
// in the standard math library because that min function
// has to check for some special floating point cases
// making it slower by a magnitude of 10.
func Min(a uint64, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// Mul64 multiples 2 64-bit unsigned integers and checks if they
// lead to an overflow. If they do not, it returns the result
// without an error.
func Mul64(a, b uint64) (uint64, error) {
	overflows, val := bits.Mul64(a, b)
	if overflows > 0 {
		return 0, errors.New("multiplication overflows")
	}
	return val, nil
}

// Add64 adds 2 64-bit unsigned integers and checks if they
// lead to an overflow. If they do not, it returns the result
// without an error.
func Add64(a, b uint64) (uint64, error) {
	res, carry := bits.Add64(a, b, 0 /* carry */)
	if carry > 0 {
		return 0, errors.New("addition overflows")
	}
	return res, nil
}
