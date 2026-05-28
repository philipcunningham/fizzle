package model

import (
	"errors"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

// ErrAuditionVoiceMissing is returned by VoiceFZVBytes when the in-memory
// FZF does not produce an extractable FZV for the requested slot. This
// happens when the slot is out of range, the voice is a NoSound placeholder
// (no audio bytes), or the audio lives on a companion disk image that isn't
// loaded.
var ErrAuditionVoiceMissing = errors.New("model: voice has no extractable audio")

// VoiceFZVBytes returns a standalone FZV byte slice for the given voice
// slot, suitable for handing to voiceextract / audioplayer for audition.
//
// The voice header's wavst/waved/genst/gened addresses are rewritten so
// they're relative to the extracted audio (the same transform
// voiceunpack performs on the disk-extract path). The resulting byte
// slice parses cleanly through fzvinfo.Parse and voiceextract.
//
// VoiceFZVBytes uses the in-memory bytes, so unsaved edits are audible
// before save (spec §4).
func (m *Model) VoiceFZVBytes(slot int) ([]byte, error) {
	if m.header == nil {
		return nil, errors.New("model: header not loaded")
	}
	if slot < 0 || slot >= m.header.NVoice {
		return nil, fmt.Errorf("model: voice slot %d out of range (0-%d)", slot, m.header.NVoice-1)
	}
	voices, slotIndices, err := voiceunpack.UnpackDataFromBytes(m.bytes)
	if err != nil {
		return nil, fmt.Errorf("model: unpacking voice %d: %w", slot, err)
	}
	for i, s := range slotIndices {
		if s == slot {
			return voices[i], nil
		}
	}
	// Slot is in range but voiceunpack dropped it (NoSound placeholder, or
	// audio lives on disk 2). Surface a sentinel so the app shell can show
	// a status line message rather than fail loudly.
	return nil, fmt.Errorf("%w: slot %d", ErrAuditionVoiceMissing, slot)
}
