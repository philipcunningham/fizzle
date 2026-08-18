// Package limits holds shared upper bounds for untrusted-input reads
// across fizzle packages, so callers can't drift on the literal.
package limits

// MaxRead is the most fizzle will read from untrusted input (WAV, SFZ,
// FZF, FZV). It bounds memory use on malformed or hostile input.
const MaxRead = 256 << 20 // 256 MiB
