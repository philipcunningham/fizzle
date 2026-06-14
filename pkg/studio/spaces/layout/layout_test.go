package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/studio/loader"
)

// TestBank2VoiceNamesUseVPTable is a regression test for the multi-
// bank vp[] bug surfaced by Solo-Tenor-Sax-2M-Byte.fzf. Before the
// fix, Layout's area-list computed slot indices by summing prior
// banks' bstep counts; on disks where a later bank's vp[] points
// into voices that earlier banks "own", that arithmetic walked into
// the middle of unrelated voice headers and rendered control bytes
// as voice names. Pool was unaffected because voiceunpack already
// walked vp[].
//
// This test loads the offending fixture, asks the Layout for every
// populated Area's voice name across all banks, and asserts the
// names are printable ASCII. A regression would resurrect the garbage
// bytes the user reported.
func TestBank2VoiceNamesUseVPTable(t *testing.T) {
	fixture := filepath.Join("..", "..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-13",
		"solo-tenor-sax-2m-byte", "Solo-Tenor-Sax-2M-Byte.fzf")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("missing Solo-Tenor-Sax fixture: %v", err)
	}
	m, info, err := loader.LoadContainer(fixture)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}

	lm := New()
	lm.SetContainer(m, info)

	// Walk every bank's areas. We don't know the exact name layout,
	// but we DO know the names must be printable ASCII (the FZ-1's
	// voice-name field) and contain at least one letter. The buggy
	// version produced strings with control characters (0x00..0x1f,
	// 0x7f, high bytes from binary fields).
	for bankIdx := 0; bankIdx < info.BankCount; bankIdx++ {
		count := lm.bankVoiceCount(bankIdx)
		for areaIdx := 0; areaIdx < count; areaIdx++ {
			name := lm.VoiceName(bankIdx, areaIdx)
			if name == "" {
				continue // empty slot is fine
			}
			if !isPrintableASCII(name) {
				t.Errorf("Bank %d Area %d voice name has non-printable bytes: %q",
					bankIdx+1, areaIdx+1, name)
			}
		}
	}
}

// TestBank2Area1MapsViaVP pins the specific symptom: Bank 2's first
// Area on the Solo-Tenor-Sax fixture used to render garbage; the vp[]
// lookup must now produce a sensible TENOR/BLOW/SUB-family name (the
// three voice prefixes this fixture's sample bank uses).
func TestBank2Area1MapsViaVP(t *testing.T) {
	fixture := filepath.Join("..", "..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-13",
		"solo-tenor-sax-2m-byte", "Solo-Tenor-Sax-2M-Byte.fzf")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("missing fixture: %v", err)
	}
	m, info, err := loader.LoadContainer(fixture)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	lm := New()
	lm.SetContainer(m, info)

	if info.BankCount < 2 {
		t.Skipf("fixture has %d banks, need at least 2", info.BankCount)
	}
	name := lm.VoiceName(1, 0) // Bank 2 Area 1, zero-indexed
	if name == "" {
		t.Fatalf("Bank 2 Area 1 voice name empty; expected a populated voice")
	}
	if !isPrintableASCII(name) {
		t.Fatalf("Bank 2 Area 1 voice name not printable ASCII: %q", name)
	}
	if !strings.Contains(name, "TENOR") &&
		!strings.Contains(name, "BLOW") &&
		!strings.Contains(name, "SUB") {
		t.Errorf("Bank 2 Area 1 voice name %q is printable but not a "+
			"recognised Solo-Tenor-Sax family; manually verify the "+
			"vp[] lookup still resolves correctly", name)
	}
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}
