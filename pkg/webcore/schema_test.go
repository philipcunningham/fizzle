package webcore

import (
	"encoding/binary"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
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
	// R15 used to require lfoAttack and lfoQ here, because other tools
	// hide them. Measurement retired it. The panel derives attack from
	// its DELAY row and has no row at all for the resonance depth, and
	// the resonance depth can't be used on a physical unit.
	if seen["lfoAttack"] || seen["lfoQ"] {
		t.Fatal("lfoAttack and lfoQ have no panel control and should not be in the schema")
	}
	// The panel's LFO SYNC row does earn a control.
	if !seen["lfoSync"] {
		t.Fatal("lfoSync missing from the schema")
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

// Every converted field has a read path and a write path, and the two
// live in different files. A value set on the panel's scale must read
// back as the same number, or one of the two is missing its conversion.
func TestConvertedFieldsRoundTripOnThePanelScale(t *testing.T) {
	for _, c := range []struct {
		field  string
		values []int
	}{
		{fieldTune, []int{-100, -50, -1, 0, 1, 50, 100}},
		{fieldLfoDelay, []int{0, 1, 50, 127}},
		{fieldVelDcqKF, []int{0, 1, 64, 127}},
	} {
		for _, v := range c.values {
			s := twoVoiceSession(t)
			if _, cerr := s.SetSlotParamNumber(0, c.field, v); cerr != nil {
				t.Fatalf("SetSlotParamNumber(%s, %d): %v", c.field, v, cerr)
			}
			if got := instrument(t, s).Voices[0].Params[c.field]; got != v {
				t.Errorf("%s set to %d reads back %v", c.field, v, got)
			}
		}
	}
}

// A round trip alone can't catch a scale that is wrong the same way on
// both sides: it round trips perfectly. These pin the stored bytes a
// panel value has to produce.
func TestConvertedFieldsStoreThePanelsBytes(t *testing.T) {
	for _, c := range []struct {
		name  string
		field string
		value int
		check func(t *testing.T, hdr []byte)
	}{
		{"tune +50 stores the word 127", fieldTune, 50, func(t *testing.T, hdr []byte) {
			if got := int16(binary.LittleEndian.Uint16(hdr[disk.VoiceDCPOffset:])); got != 127 { //nolint:gosec // signed reinterpretation
				t.Errorf("stored tune word = %d, want 127", got)
			}
		}},
		{"tune -100 stores the word -255", fieldTune, -100, func(t *testing.T, hdr []byte) {
			if got := int16(binary.LittleEndian.Uint16(hdr[disk.VoiceDCPOffset:])); got != -255 { //nolint:gosec // signed reinterpretation
				t.Errorf("stored tune word = %d, want -255", got)
			}
		}},
		{"delay 50 stores the word 800", fieldLfoDelay, 50, func(t *testing.T, hdr []byte) {
			if got := binary.LittleEndian.Uint16(hdr[disk.VoiceLFODelayOffset:]); got != 800 {
				t.Errorf("stored delay word = %d, want 800", got)
			}
		}},
		{"velDcqKF 50 stores the byte 50", fieldVelDcqKF, 50, func(t *testing.T, hdr []byte) {
			if got := hdr[disk.VoiceVelDCQKFOffset]; got != 50 {
				t.Errorf("stored velDcqKF byte = %d, want 50", got)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := twoVoiceSession(t)
			if _, cerr := s.SetSlotParamNumber(0, c.field, c.value); cerr != nil {
				t.Fatalf("SetSlotParamNumber: %v", cerr)
			}
			c.check(t, unpackSlot(t, s, 0))
		})
	}
}

// The panel's LFO SYNC row shares a byte with the waveform, so setting
// one must leave the other alone.
func TestLfoSyncAndWaveformShareAByteSafely(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetSlotParamOption(0, fieldLfoSync, "on"); cerr != nil {
		t.Fatalf("SetSlotParamOption(lfoSync): %v", cerr)
	}
	if _, cerr := s.SetSlotParamOption(0, fieldLfoWave, "triangle"); cerr != nil {
		t.Fatalf("SetSlotParamOption(lfoWave): %v", cerr)
	}
	params := instrument(t, s).Voices[0].Params
	if params[fieldLfoSync] != "on" {
		t.Errorf("sync reads %v after a waveform change, want on", params[fieldLfoSync])
	}
	if params[fieldLfoWave] != "triangle" {
		t.Errorf("waveform reads %v, want triangle", params[fieldLfoWave])
	}
}

// Editing the delay writes the attack byte too, the way the panel's
// DELAY row does. Nothing else offers a way to set the attack.
func TestSettingDelayAlsoWritesTheAttack(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetSlotParamNumber(0, fieldLfoDelay, 50); cerr != nil {
		t.Fatalf("SetSlotParamNumber(lfoDelay): %v", cerr)
	}
	hdr := unpackSlot(t, s, 0)
	if got := binary.LittleEndian.Uint16(hdr[disk.VoiceLFODelayOffset:]); got != 800 {
		t.Errorf("delay word = %d, want 800", got)
	}
	if got := hdr[disk.VoiceLFOAtckOffset]; got != 11 {
		t.Errorf("attack byte = %d, want 11, the value the panel derives", got)
	}
}
