package disk

// Panel display conversions, read off the FZ-1 firmware running under
// an emulator: set the value on the panel, save, and read the bytes
// back. llm-wiki/topics/display-scales.md carries each mapping with its
// evidence.
//
// None of this is confirmed on a physical FZ. A measurement taken on a
// real device outranks it and replaces the rule here.

const (
	// MinTuneDisplay is the TUNE row's floor. The panel reads roughly as
	// cents, one semitone either way.
	MinTuneDisplay = -100
	// MaxTuneDisplay is the TUNE row's ceiling.
	MaxTuneDisplay = 100

	// MaxLFODelayDisplay is the LFO DELAY row's ceiling.
	MaxLFODelayDisplay = 127
	// LFODelayStep is the stored-word step behind one display unit.
	LFODelayStep = 16

	// MaxVelDCQDisplay is the VELOCITY SENS resonance row's ceiling.
	// The row carries no sign column and refuses to go below zero.
	MaxVelDCQDisplay = 127

	// MaxAreaLevel is the largest AREA LEVEL the panel displays, and the
	// largest bvol byte. The two run in opposite directions.
	MaxAreaLevel = 127
)

// TuneDisplayToWord converts a TUNE display value to the stored dcp
// word, clamping to the panel's span first so this never returns a word
// the panel couldn't dial.
func TuneDisplayToWord(display int) int16 {
	if display < MinTuneDisplay {
		display = MinTuneDisplay
	}
	if display > MaxTuneDisplay {
		display = MaxTuneDisplay
	}
	// Truncates toward zero, matching the panel's own stepping.
	return int16(display * 255 / 100) //nolint:gosec // bounded by the clamp above
}

// TuneWordToDisplay renders a stored dcp word the way the panel's TUNE
// row renders it. The panel reads the low byte for magnitude and takes
// the sign from the word, so a word beyond the panel's span wraps
// rather than clamping.
func TuneWordToDisplay(word int16) int {
	low := int(word) & 0xFF
	negative := word < 0
	mag := low
	if negative {
		mag = 256 - low
		if low == 0 {
			mag = 256
		}
	}
	display := (mag*100 + 254) / 255 // ceil
	if negative {
		return -display
	}
	return display
}

// LFODelayDisplayToWord converts an LFO DELAY display value to the
// stored word, clamped to the panel's span.
func LFODelayDisplayToWord(display int) uint16 {
	if display < 0 {
		display = 0
	}
	if display > MaxLFODelayDisplay {
		display = MaxLFODelayDisplay
	}
	return uint16(display * LFODelayStep) //nolint:gosec // bounded by the clamp above
}

// LFODelayWordToDisplay renders a stored LFO delay word the way the
// panel's DELAY row renders it. A word beyond the panel's span renders
// past 127 rather than clamping, which is what the panel does.
func LFODelayWordToDisplay(word uint16) int {
	return int(word) / LFODelayStep
}

// LFOAttackForDelay returns the lfo_atck byte the panel writes
// alongside a DELAY display value. The panel has no independent attack
// row: moving DELAY writes both bytes.
func LFOAttackForDelay(display int) uint8 {
	if display < 0 {
		display = 0
	}
	if display > MaxLFODelayDisplay {
		display = MaxLFODelayDisplay
	}
	steps := (display + 7) / 8 // ceil(display / 8)
	return uint8(18 - steps)   //nolint:gosec // steps is 0 to 16
}

// VelDCQByteToDisplay renders the velocity-to-resonance byte the way the
// panel's RESONANCE row renders it: the magnitude of the signed byte,
// with no sign column.
func VelDCQByteToDisplay(b uint8) int {
	if b > 127 {
		return 256 - int(b)
	}
	return int(b)
}

// AreaLevelFromByte converts a stored bvol byte to the AREA LEVEL value
// the panel shows. The panel counts the opposite way to the byte: a
// stored 0 displays as 127, and a stored 127 displays as 0.
func AreaLevelFromByte(b uint8) int {
	return MaxAreaLevel - int(b)
}

// AreaLevelToByte converts an AREA LEVEL display value to the stored
// bvol byte. It is the inverse of AreaLevelFromByte.
func AreaLevelToByte(display int) uint8 {
	if display < 0 {
		display = 0
	}
	if display > MaxAreaLevel {
		display = MaxAreaLevel
	}
	return uint8(MaxAreaLevel - display) //nolint:gosec // bounded by the clamp above
}
