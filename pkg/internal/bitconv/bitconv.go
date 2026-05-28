// Package bitconv provides intentional bit-pattern and narrowing conversions
// used across the codebase. It exists as a single place to suppress gosec
// G115 noise: each helper documents the invariant the caller is asserting.
package bitconv

import "encoding/binary"

// ReadInt16LE reads a little-endian int16 sample from b[0:2]. b must be at
// least 2 bytes; the caller is responsible for bounds.
func ReadInt16LE(b []byte) int16 {
	return int16(binary.LittleEndian.Uint16(b)) //nolint:gosec // intentional PCM bit-pattern conversion
}

// WriteInt16LE writes v as a little-endian int16 sample to b[0:2]. b must be
// at least 2 bytes; the caller is responsible for bounds.
func WriteInt16LE(b []byte, v int16) {
	binary.LittleEndian.PutUint16(b, uint16(v)) //nolint:gosec // intentional PCM bit-pattern conversion
}

// NarrowU8 narrows v to uint8. The caller must have validated v fits in
// [0, 255]; this helper does not check. Centralizes gosec G115 suppression
// for documented, validated narrowings.
func NarrowU8(v int) uint8 {
	return uint8(v) //nolint:gosec // G115: caller guarantees v fits in uint8
}

// NarrowU16 narrows v to uint16. The caller must have validated v fits in
// [0, 65535]; this helper does not check.
func NarrowU16(v int) uint16 {
	return uint16(v) //nolint:gosec // G115: caller guarantees v fits in uint16
}

// NarrowU32 narrows v to uint32. The caller must have validated v >= 0 and
// fits in [0, 2^32-1]; this helper does not check.
func NarrowU32(v int) uint32 {
	return uint32(v) //nolint:gosec // G115: caller guarantees v fits in uint32
}

// LenU32 returns len(s) as uint32. Use when a binary format field is
// uint32 and the slice length is known by construction to fit (audio sample
// buffers, sector arrays bounded by disk capacity, etc.).
func LenU32[E any](s []E) uint32 {
	return uint32(len(s)) //nolint:gosec // G115: len is non-negative and bounded by container
}
