package webcore

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
)

// R20 to R22: audition PCM returns at the declared rate and length,
// decoded by the same code as the CLI's extract.
func TestAuditionPCMMatchesExtract(t *testing.T) {
	s, voice := importedSession(t)

	a, cerr := s.AuditionPCM(voice)
	if cerr != nil {
		t.Fatalf("AuditionPCM: %v", cerr)
	}
	if a.SampleRate != 18000 {
		t.Fatalf("rate = %d, want 18000", a.SampleRate)
	}
	if a.Root != 60 {
		t.Fatalf("root = %d, want the import default C4 (60)", a.Root)
	}

	// The same bytes the CLI would extract.
	img, err := disk.ReadImage(bytes.NewReader(mustExport(t, s)))
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	fzv, gerr := diskget.FromImage(img, voice)
	if gerr != nil {
		t.Fatalf("FromImage: %v", gerr)
	}
	rate, samples, derr := voiceextract.Decode(fzv)
	if derr != nil {
		t.Fatalf("Decode: %v", derr)
	}
	if int(rate) != a.SampleRate || len(samples) != len(a.PCM) {
		t.Fatalf("audition %d/%d differs from extract %d/%d", a.SampleRate, len(a.PCM), rate, len(samples))
	}
	for i := range samples {
		if samples[i] != a.PCM[i] {
			t.Fatalf("sample %d differs", i)
		}
	}

	if _, cerr := s.AuditionPCM("NOWHERE"); cerr == nil || cerr.Code != codeNotFound {
		t.Fatalf("missing: %v", cerr)
	}
}

// Instrument voices audition by slot, through the same unpack path the
// CLI's fzf unpack uses.
func TestAuditionSlot(t *testing.T) {
	s := twoVoiceSession(t)

	a, cerr := s.AuditionSlot(1)
	if cerr != nil {
		t.Fatalf("AuditionSlot: %v", cerr)
	}
	if len(a.PCM) != 2100 {
		t.Fatalf("pcm frames = %d, want 2100 (voice HIGH)", len(a.PCM))
	}
	if a.Root != 72 {
		t.Fatalf("root = %d, want keygroup centre 72", a.Root)
	}

	if _, cerr := s.AuditionSlot(9); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("slot 9: %v", cerr)
	}
	if _, cerr := NewSession().AuditionSlot(0); cerr == nil || cerr.Code != codeNoDisk {
		t.Fatalf("no disk: %v", cerr)
	}
}
