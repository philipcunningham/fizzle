package sound

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// TestLoopAddrBounds_ClampsToWaveRegion pins F-QA-16: loop addresses are
// bounded by the sample's wave region, so a wildly out-of-range typed
// value (the report's 999999) lands at the wave end instead of past it.
func TestLoopAddrBounds_ClampsToWaveRegion(t *testing.T) {
	data := make([]byte, 4096)
	const (
		ws = 96346
		we = 106445
	)
	binary.LittleEndian.PutUint32(data[disk.VoiceWaveStartOffset:], ws)
	binary.LittleEndian.PutUint32(data[disk.VoiceWaveEndOffset:], we)

	lo, hi := loopAddrBounds(data, 0, int(disk.LoopStartAddressMask))
	if lo != ws || hi != we {
		t.Fatalf("loopAddrBounds = (%d, %d), want (%d, %d)", lo, hi, ws, we)
	}
	if got := clampInt(999999, lo, hi); got != we {
		t.Errorf("clamp(999999) = %d, want %d (wave end)", got, we)
	}
	if got := clampInt(0, lo, hi); got != ws {
		t.Errorf("clamp(0) = %d, want %d (wave start)", got, ws)
	}
}

// TestLoopAddrBounds_UnsetWaveKeepsMask pins that a voice with unset wave
// bounds keeps the field's mask ceiling (no accidental clamp-to-zero).
func TestLoopAddrBounds_UnsetWaveKeepsMask(t *testing.T) {
	data := make([]byte, 4096)
	mask := int(disk.LoopEndAddressMask)
	lo, hi := loopAddrBounds(data, 0, mask)
	if lo != 0 || hi != mask {
		t.Errorf("loopAddrBounds with unset wave = (%d, %d), want (0, %d)", lo, hi, mask)
	}
}
