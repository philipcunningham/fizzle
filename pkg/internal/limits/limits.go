// Package limits holds shared upper bounds for untrusted-input reads
// across fizzle packages. Centralised to avoid drift between callers
// that would otherwise duplicate the same numeric literal.
package limits

// MaxRead is the maximum byte count fizzle will accept when reading
// untrusted input (WAV, SFZ, FZF, FZV). It exists to bound memory use
// on malformed or hostile input.
const MaxRead = 256 << 20 // 256 MiB
