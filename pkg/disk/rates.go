package disk

import "fmt"

// SampleRates maps the FZ series rate index byte to a sample rate in Hz.
// Index 0 = 36 kHz, 1 = 18 kHz, 2 = 9 kHz. The FZ hardware accepts no other
// rates; this list is the canonical truth.
var SampleRates = [...]uint32{36000, 18000, 9000}

var rateIndex = map[uint32]uint8{36000: 0, 18000: 1, 9000: 2}

// NumSampleRates returns the number of supported sample rates.
func NumSampleRates() int { return len(SampleRates) }

// SampleRatesSlice returns a copy of the supported sample rates ordered from
// highest to lowest quality.
func SampleRatesSlice() []uint32 {
	s := make([]uint32, len(SampleRates))
	copy(s, SampleRates[:])
	return s
}

// RateIndexFor returns the rate index byte for the given sample rate, and a
// boolean indicating whether the rate is valid.
func RateIndexFor(rate uint32) (uint8, bool) {
	idx, ok := rateIndex[rate]
	return idx, ok
}

// SampleRate returns the sample rate in Hz for a rate index byte. Returns 0
// for unknown index values.
func SampleRate(idx uint8) uint32 {
	if int(idx) < len(SampleRates) {
		return SampleRates[idx]
	}
	return 0
}

// ValidateRate returns an error if rate is not a supported FZ sample rate.
func ValidateRate(rate uint32) error {
	if _, ok := RateIndexFor(rate); !ok {
		return fmt.Errorf("disk: unsupported rate %d (use 36000, 18000, or 9000)", rate)
	}
	return nil
}
