package fzfinfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

// A stamped dump lists all its voices, matching the browser.
func TestParseHonoursVoiceCountMarker(t *testing.T) {
	t.Parallel()
	dump := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(dump, fzfbuilder.BanklessDumpVoices)
	path := filepath.Join(t.TempDir(), "stamped.fzf")
	if err := os.WriteFile(path, dump, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Voices) != fzfbuilder.BanklessDumpVoices {
		t.Errorf("voices = %d, want %d", len(info.Voices), fzfbuilder.BanklessDumpVoices)
	}
}
