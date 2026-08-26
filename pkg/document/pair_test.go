package document

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

// Real FZ-1 images do not always stamp the total-wave marker, so the last
// voice's wave end remains an independent continuation bound.
func TestValidateContinuationCatchesShortAudioWithoutMarker(t *testing.T) {
	t.Parallel()
	voices := make([][]byte, 3)
	groups := make([]voicebuild.Keygroup, 3)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("MRK%02d", i+1), 40000)
		key := byte(50 + i)
		groups[i] = voicebuild.NewKeygroup(key, key, key)
	}
	dump, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := fzutil.ResolveStandaloneFZFLayout(dump)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateContinuation(dump, layout); err != nil {
		t.Fatalf("whole dump: %v", err)
	}
	withoutMarker := bytes.Clone(dump)
	binary.LittleEndian.PutUint32(withoutMarker[disk.BankTotalWaveOffset:], 0)
	if err := validateContinuation(withoutMarker, layout); err != nil {
		t.Fatalf("whole dump without marker: %v", err)
	}
	short := withoutMarker[:len(withoutMarker)-4*disk.SectorSize]
	if err := validateContinuation(short, layout); err == nil {
		t.Fatal("short markerless continuation passed validation")
	}
}
