package webcore

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// importedSession returns a session holding one imported voice.
func importedSession(t *testing.T) (*Session, string) {
	t.Helper()
	s := NewSession()
	if _, cerr := s.NewDisk("EDITME"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.ImportWAV("Edit Me.wav", wavBytes(t, 3000), 18000, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAV: %v", cerr)
	}
	return s, "EDIT ME"
}

func paramValue(t *testing.T, s *Session, file, field string) any {
	t.Helper()
	snap := s.Snapshot()
	if snap.Disk == nil {
		t.Fatal("no disk")
	}
	for _, f := range snap.Disk.Files {
		if f.Name == file {
			v, ok := f.Params[field]
			if !ok {
				t.Fatalf("param %q missing from %q: %+v", field, file, f.Params)
			}
			return v
		}
	}
	t.Fatalf("file %q not in snapshot", file)
	return nil
}

func TestSchemaShape(t *testing.T) {
	fields := Schema()
	if len(fields) < 20 {
		t.Fatalf("schema has %d fields, expected the flat voice surface", len(fields))
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if f.ID == "" || f.Label == "" || f.Group == "" {
			t.Fatalf("incomplete field: %+v", f)
		}
		if seen[f.ID] {
			t.Fatalf("duplicate field id %q", f.ID)
		}
		seen[f.ID] = true
		switch f.Kind {
		case kindKnob, kindStepper, kindNote:
			if f.Min >= f.Max {
				t.Fatalf("field %q has empty range [%d,%d]", f.ID, f.Min, f.Max)
			}
		case kindSelect:
			if len(f.Options) < 2 {
				t.Fatalf("select %q has %d options", f.ID, len(f.Options))
			}
		default:
			t.Fatalf("field %q has unknown kind %q", f.ID, f.Kind)
		}
	}
	// R15: the parameters some prior tools hide are present.
	if !seen["lfoAttack"] || !seen["lfoQ"] {
		t.Fatal("R15 fields missing from the schema")
	}
}

// R14: ranges and clamping are defined solely by the core's schema.
// Property test: any integer sent to any numeric field lands clamped
// inside the field's declared range, and in-range values land exactly.
func TestSetParamClampsEveryField(t *testing.T) {
	s, voice := importedSession(t)
	rng := rand.New(rand.NewSource(1)) // #nosec G404 -- deterministic test data

	for _, f := range Schema() {
		if f.Kind == kindSelect {
			continue
		}
		t.Run(f.ID, func(t *testing.T) {
			for i := 0; i < 12; i++ {
				raw := rng.Intn(f.Max-f.Min+1) + f.Min
				if i%3 == 0 {
					raw = f.Min - rng.Intn(1000) - 1 // below range
				}
				if i%3 == 1 {
					raw = f.Max + rng.Intn(1000) + 1 // above range
				}
				snap, cerr := s.SetParamNumber(voice, f.ID, raw)
				if cerr != nil {
					t.Fatalf("SetParamNumber(%s, %d): %v", f.ID, raw, cerr)
				}
				if snap.Revision == 0 {
					t.Fatal("no revision")
				}
				got, ok := paramValue(t, s, voice, f.ID).(int)
				if !ok {
					t.Fatalf("param %q is not an int", f.ID)
				}
				want := raw
				if want < f.Min {
					want = f.Min
				}
				if want > f.Max {
					want = f.Max
				}
				if got != want {
					t.Fatalf("field %s: sent %d, stored %d, want %d", f.ID, raw, got, want)
				}
			}
		})
	}
}

func TestSetParamOptionRoundTrips(t *testing.T) {
	s, voice := importedSession(t)
	for _, f := range Schema() {
		if f.Kind != kindSelect {
			continue
		}
		for _, opt := range f.Options {
			if _, cerr := s.SetParamOption(voice, f.ID, opt); cerr != nil {
				t.Fatalf("SetParamOption(%s, %s): %v", f.ID, opt, cerr)
			}
			if got := paramValue(t, s, voice, f.ID); got != opt {
				t.Fatalf("field %s: set %q, stored %v", f.ID, opt, got)
			}
		}
	}
}

func TestSetParamInvalidLeavesStateUntouched(t *testing.T) {
	s, voice := importedSession(t)
	before := s.Snapshot()
	beforeImage, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}

	const codeInvalidField = "invalid-field"
	cases := []struct {
		name string
		run  func() *Error
		code string
	}{
		{"unknown field", func() *Error { _, e := s.SetParamNumber(voice, "warp", 1); return e }, codeInvalidField},
		{"option to numeric field", func() *Error { _, e := s.SetParamOption(voice, "cutoff", "high"); return e }, codeInvalidField},
		{"number to select field", func() *Error { _, e := s.SetParamNumber(voice, "playbackMode", 1); return e }, codeInvalidField},
		{"unknown option", func() *Error { _, e := s.SetParamOption(voice, "playbackMode", "sideways"); return e }, "invalid-value"},
		{"missing file", func() *Error { _, e := s.SetParamNumber("NOWHERE", "cutoff", 1); return e }, codeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cerr := tc.run()
			if cerr == nil || cerr.Code != tc.code {
				t.Fatalf("cerr = %v, want %s", cerr, tc.code)
			}
		})
	}

	after := s.Snapshot()
	if after.Revision != before.Revision {
		t.Fatal("rejected ops advanced the revision")
	}
	afterImage, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	if string(beforeImage) != string(afterImage) {
		t.Fatal("rejected ops changed the image bytes")
	}
}

// Q1 again: an edit through the session equals the CLI pipeline (disk
// get, fzv edit, replace on image) on the same input.
func TestSetParamMatchesCLIPipeline(t *testing.T) {
	wavData := wavBytes(t, 4000)

	// CLI reference.
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "Edit Me.wav")
	fzvPath := filepath.Join(dir, "EDIT ME.fzv")
	imgPath := filepath.Join(dir, "ref.img")
	if err := os.WriteFile(wavPath, wavData, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := diskformat.Format(imgPath, "EDITME"); err != nil {
		t.Fatalf("Format: %v", err)
	}
	if err := voiceimport.Import(wavPath, fzvPath, 18000); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if err := diskadd.Add(imgPath, fzvPath, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	filterPatches, perr := voiceedit.BuildFilterPatches(90, 3)
	if perr != nil {
		t.Fatalf("BuildFilterPatches: %v", perr)
	}
	if err := voiceedit.ApplyToFZV(fzvPath, filterPatches); err != nil {
		t.Fatalf("ApplyToFZV: %v", err)
	}
	patched, err := os.ReadFile(fzvPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := diskadd.ReplaceOnImage(imgPath, "EDIT ME", patched, 0); err != nil {
		t.Fatalf("ReplaceOnImage: %v", err)
	}
	ref, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Session path.
	s := NewSession()
	if _, cerr := s.NewDisk("EDITME"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.ImportWAV("Edit Me.wav", wavData, 18000, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAV: %v", cerr)
	}
	if _, cerr := s.SetParamNumber("EDIT ME", "cutoff", 90); cerr != nil {
		t.Fatalf("SetParamNumber: %v", cerr)
	}
	if _, cerr := s.SetParamNumber("EDIT ME", "resonance", 3); cerr != nil {
		t.Fatalf("SetParamNumber: %v", cerr)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	if string(out) != string(ref) {
		t.Fatal("session edit differs from the CLI pipeline")
	}
}
