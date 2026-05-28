package voiceedit

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

func buildTestFZV(t *testing.T) string {
	t.Helper()
	samples := make([]int16, 1000)
	for i := range samples {
		samples[i] = int16(i % 100)
	}
	data := voiceimport.Encode(samples, 0, "TEST", 0, voiceimport.NoLoop())
	path := filepath.Join(t.TempDir(), "test.fzv")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func extractTestFZF(t *testing.T, imgPath, dumpName string) string { //nolint:unparam // helper designed for reuse with other fixtures
	t.Helper()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "dump.fzf")
	if err := diskget.Get(imgPath, dumpName, fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	return fzfPath
}

func parseFZV(t *testing.T, path string) *fzvinfo.VoiceParams {
	t.Helper()
	params, err := fzvinfo.Parse(path)
	if err != nil {
		t.Fatalf("fzvinfo.Parse: %v", err)
	}
	return params
}

// ---------------------------------------------------------------------------
// Validation tests
// ---------------------------------------------------------------------------

func TestValidateByte(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		val     int
		min     int
		max     int
		wantErr bool
	}{
		{"in range", 64, 0, 127, false},
		{"at min", 0, 0, 127, false},
		{"at max", 127, 0, 127, false},
		{"below min", -1, 0, 127, true},
		{"above max", 128, 0, 127, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateByte("test", tc.val, tc.min, tc.max)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateByte(%d, %d, %d) error=%v, wantErr=%v", tc.val, tc.min, tc.max, err, tc.wantErr)
			}
		})
	}
}

func TestWaveformIndex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		want   int
		wantOK bool
	}{
		{"sine", disk.LFOSine, true},
		{"saw-up", disk.LFOSawUp, true},
		{"saw-down", disk.LFOSawDown, true},
		{"triangle", disk.LFOTriangle, true},
		{"rectangle", disk.LFORectangle, true},
		{"random", disk.LFORandom, true},
		{"Sine", disk.LFOSine, true},
		{"SAW-UP", disk.LFOSawUp, true},
		{"SaW-DoWn", disk.LFOSawDown, true},
		{"unknown", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := WaveformIndex(tt.name)
			if ok != tt.wantOK {
				t.Errorf("WaveformIndex(%q) ok = %v, want %v", tt.name, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("WaveformIndex(%q) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestValidateWaveform(t *testing.T) {
	t.Parallel()
	for i := range 6 {
		if err := ValidateWaveform(i); err != nil {
			t.Errorf("ValidateWaveform(%d) should be valid: %v", i, err)
		}
	}
	if err := ValidateWaveform(6); err == nil {
		t.Error("ValidateWaveform(6) should be invalid")
	}
	if err := ValidateWaveform(-1); err == nil {
		t.Error("ValidateWaveform(-1) should be invalid")
	}
}

// ---------------------------------------------------------------------------
// Patch building tests
// ---------------------------------------------------------------------------

func TestBuildLFOPatches(t *testing.T) {
	t.Parallel()
	patches, err := BuildLFOPatches(0, 25, 100, 127, 0, 0, 50, Unchanged, 0)
	if err != nil {
		t.Fatal(err)
	}
	offsets := make(map[int]uint16)
	for _, p := range patches {
		offsets[p.Offset] = p.Value
	}
	if offsets[disk.VoiceLFONameOffset] != 0 {
		t.Errorf("waveform: got %d, want 0 (sine)", offsets[disk.VoiceLFONameOffset])
	}
	if offsets[disk.VoiceLFORateOffset] != 25 {
		t.Errorf("rate: got %d, want 25", offsets[disk.VoiceLFORateOffset])
	}
	if offsets[disk.VoiceLFODCFOffset] != 50 {
		t.Errorf("filter depth: got %d, want 50", offsets[disk.VoiceLFODCFOffset])
	}
	if _, ok := offsets[disk.VoiceLFODCQOffset]; ok {
		t.Error("q should not be patched when -1")
	}
}

func TestBuildLFOPatchesAllSkipped(t *testing.T) {
	t.Parallel()
	patches, err := BuildLFOPatches(Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 0 {
		t.Errorf("expected 0 patches when all Unchanged, got %d", len(patches))
	}
}

func TestBuildLFOPatchesInvalidRate(t *testing.T) {
	t.Parallel()
	_, err := BuildLFOPatches(Unchanged, 200, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
	if err == nil {
		t.Error("expected error for rate 200")
	}
}

// TestBuildLFOPatchesPreservesPhaseFlag pins the spec §2-1 layout of the
// lfo_name byte: bits 0-6 are the waveform index, bit 7 is the phase-sync
// flag. When only the waveform index changes, the phase-sync flag must
// survive the byte-level write. A clean byte(wave) write (the prior
// implementation) silently cleared bit 7.
func TestBuildLFOPatchesPreservesPhaseFlag(t *testing.T) {
	t.Parallel()
	// origLFOName = 0x83: waveform 3 (Triangle) | bit 7 (phase sync).
	const origLFOName uint8 = 0x83
	patches, err := BuildLFOPatches(disk.LFOSine, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, origLFOName)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(patches))
	}
	p := patches[0]
	if p.Offset != disk.VoiceLFONameOffset {
		t.Errorf("offset: got 0x%x, want 0x%x", p.Offset, disk.VoiceLFONameOffset)
	}
	// LFOSine (0) | LFOPhaseFlag (0x80) == 0x80.
	if p.Value != 0x80 {
		t.Errorf("value: got 0x%02x, want 0x80 (sine + preserved phase-sync flag)", p.Value)
	}
}

// TestApplyToFZVLFOPreservesPhaseFlag exercises the full patch path: set
// bit 7 in the file, edit the waveform, read back the byte, and confirm
// bit 7 survives.
func TestApplyToFZVLFOPreservesPhaseFlag(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)

	// Force lfo_name = 0x83 (triangle + phase-sync flag) directly on disk.
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0x83}, int64(disk.VoiceLFONameOffset)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	patches, err := BuildLFOPatches(disk.LFOSine, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0x83)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}

	f2, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := f2.ReadAt(buf, int64(disk.VoiceLFONameOffset)); err != nil {
		_ = f2.Close()
		t.Fatal(err)
	}
	if err := f2.Close(); err != nil {
		t.Fatal(err)
	}
	if buf[0] != 0x80 {
		t.Errorf("lfo_name byte after wave edit: got 0x%02x, want 0x80 (sine + preserved phase flag)", buf[0])
	}
}

func TestBuildFilterPatches(t *testing.T) {
	t.Parallel()
	patches, err := BuildFilterPatches(64, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("expected 2 patches, got %d", len(patches))
	}
	offsets := make(map[int]uint16)
	for _, p := range patches {
		offsets[p.Offset] = p.Value
	}
	if offsets[disk.VoiceDCFOffset] != 64 {
		t.Errorf("cutoff: got %d, want 64", offsets[disk.VoiceDCFOffset])
	}
	if offsets[disk.VoiceDCQOffset] != 7 {
		t.Errorf("resonance: got %d, want 7", offsets[disk.VoiceDCQOffset])
	}
}

func TestBuildFilterPatchesResonanceEncoding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		resonance int
		wantByte  uint16
	}{
		{0, 0},
		{1, 1},
		{7, 7},
		{15, 15},
		{127, 127},
	}
	for _, tc := range cases {
		patches, err := BuildFilterPatches(Unchanged, tc.resonance)
		if err != nil {
			t.Fatal(err)
		}
		if patches[0].Value != tc.wantByte {
			t.Errorf("resonance %d: got %d, want %d", tc.resonance, patches[0].Value, tc.wantByte)
		}
	}
}

func TestBuildNamePatch(t *testing.T) {
	t.Parallel()
	patches, err := BuildNamePatch("my pad")
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(patches))
	}
	p := patches[0]
	if p.Offset != disk.VoiceNameOffset {
		t.Errorf("offset: got 0x%x, want 0x%x", p.Offset, disk.VoiceNameOffset)
	}
	if len(p.Bytes) != disk.VoiceNameFieldSize {
		t.Fatalf("payload length: got %d, want %d", len(p.Bytes), disk.VoiceNameFieldSize)
	}
	// BuildNamePatch stores the name verbatim: mixed case preserved.
	// (Factory disks such as "All Voices" use mixed case; upper-casing
	// on commit surprises users who Tab through unchanged fields.)
	if string(p.Bytes[:6]) != "my pad" {
		t.Errorf("payload prefix: got %q, want %q", p.Bytes[:6], "my pad")
	}
	// The 12-byte name field is space-padded; the trailing 2 bytes are nulls.
	for i := 6; i < disk.LabelSize; i++ {
		if p.Bytes[i] != ' ' {
			t.Errorf("byte %d: got 0x%x, want space", i, p.Bytes[i])
		}
	}
	for i := disk.LabelSize; i < disk.VoiceNameFieldSize; i++ {
		if p.Bytes[i] != 0 {
			t.Errorf("byte %d: got 0x%x, want 0", i, p.Bytes[i])
		}
	}
}

func TestBuildNamePatchTooLong(t *testing.T) {
	t.Parallel()
	_, err := BuildNamePatch("THIS IS WAY TOO LONG")
	if err == nil {
		t.Error("expected error for name exceeding 12 chars")
	}
}

// ---------------------------------------------------------------------------
// FZV patching tests
// ---------------------------------------------------------------------------

func TestApplyToFZV(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	before := parseFZV(t, path)
	if before.LFORate != 0 {
		t.Fatalf("expected initial LFO rate 0, got %d", before.LFORate)
	}

	patches, _ := BuildLFOPatches(Unchanged, 42, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}

	after := parseFZV(t, path)
	if after.LFORate != 42 {
		t.Errorf("LFO rate after patch: got %d, want 42", after.LFORate)
	}
}

func TestApplyToFZVPreservesOtherBytes(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	before := parseFZV(t, path)

	patches, _ := BuildLFOPatches(Unchanged, 42, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}

	after := parseFZV(t, path)
	if after.SampleRate != before.SampleRate {
		t.Errorf("sample rate changed: %d -> %d", before.SampleRate, after.SampleRate)
	}
	if after.Name != before.Name {
		t.Errorf("name changed: %q -> %q", before.Name, after.Name)
	}
	if after.FilterCutoff != before.FilterCutoff {
		t.Errorf("cutoff changed: %d -> %d", before.FilterCutoff, after.FilterCutoff)
	}
}

func TestApplyToFZVMultiplePatches(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)

	lfoPatches, _ := BuildLFOPatches(3, 25, Unchanged, 127, Unchanged, Unchanged, 50, Unchanged, 0)
	filterPatches, _ := BuildFilterPatches(64, 7)
	all := make([]Patch, 0, len(lfoPatches)+len(filterPatches))
	all = append(all, lfoPatches...)
	all = append(all, filterPatches...)

	if err := ApplyToFZV(path, all); err != nil {
		t.Fatal(err)
	}

	after := parseFZV(t, path)
	if after.LFORate != 25 {
		t.Errorf("LFO rate: got %d, want 25", after.LFORate)
	}
	if after.LFOAttack != 127 {
		t.Errorf("LFO attack: got %d, want 127", after.LFOAttack)
	}
	if after.LFODepthFilter != 50 {
		t.Errorf("LFO filter depth: got %d, want 50", after.LFODepthFilter)
	}
	if after.FilterCutoff != 64 {
		t.Errorf("cutoff: got %d, want 64", after.FilterCutoff)
	}
	if after.FilterQ != 7 {
		t.Errorf("resonance: got %d, want 7", after.FilterQ)
	}
}

func TestApplyToFZVInvalidPath(t *testing.T) {
	t.Parallel()
	err := ApplyToFZV("/nonexistent/path.fzv", []Patch{{Offset: 0xA0, Size: 1, Value: 42}})
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestApplyToFZVRejectsCorruptFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "corrupt.fzv")
	data := make([]byte, disk.SectorSize*2)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	err := ApplyToFZV(path, []Patch{{Offset: 0xA0, Size: 1, Value: 42}})
	if err == nil {
		t.Error("expected error for corrupt file")
	}
	if !errors.Is(err, ErrNotVoiceFile) {
		t.Errorf("expected ErrNotVoiceFile, got: %v", err)
	}
}

func TestApplyToFZVTooSmall(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "tiny.fzv")
	if err := os.WriteFile(path, make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	err := ApplyToFZV(path, []Patch{{Offset: 0xA0, Size: 1, Value: 42}})
	if err == nil {
		t.Error("expected error for file smaller than sector size")
	}
}

func TestApplyToFZVOutOfRange(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	err := ApplyToFZV(path, []Patch{{Offset: disk.VoiceHeaderUsed + 1, Size: 1, Value: 42}})
	if err == nil {
		t.Error("expected error for offset beyond header")
	}
}

// ---------------------------------------------------------------------------
// FZF voice patching tests
// ---------------------------------------------------------------------------

func TestApplyToFZFVoice(t *testing.T) {
	t.Parallel()
	fzfPath := extractTestFZF(t, "../../testdata/synthetic/TECHNO.img", "FULL-DATA-FZ")

	patches, _ := BuildLFOPatches(0, 30, Unchanged, Unchanged, Unchanged, Unchanged, 60, Unchanged, 0)
	if err := ApplyToFZFVoice(fzfPath, "COWBELL", patches); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := voiceunpack.Unpack(fzfPath, dir); err != nil {
		t.Fatal(err)
	}
	params := parseFZV(t, filepath.Join(dir, "COWBELL.fzv"))
	if params.LFORate != 30 {
		t.Errorf("LFO rate: got %d, want 30", params.LFORate)
	}
	if params.LFODepthFilter != 60 {
		t.Errorf("LFO filter depth: got %d, want 60", params.LFODepthFilter)
	}
}

func TestApplyToFZFVoicePreservesOthers(t *testing.T) {
	t.Parallel()
	fzfPath := extractTestFZF(t, "../../testdata/synthetic/TECHNO.img", "FULL-DATA-FZ")

	dir1 := t.TempDir()
	if err := voiceunpack.Unpack(fzfPath, dir1); err != nil {
		t.Fatal(err)
	}
	bellBefore := parseFZV(t, filepath.Join(dir1, "METAL-BELL.fzv"))

	patches, _ := BuildLFOPatches(0, 30, Unchanged, Unchanged, Unchanged, Unchanged, 60, Unchanged, 0)
	if err := ApplyToFZFVoice(fzfPath, "COWBELL", patches); err != nil {
		t.Fatal(err)
	}

	dir2 := t.TempDir()
	if err := voiceunpack.Unpack(fzfPath, dir2); err != nil {
		t.Fatal(err)
	}
	bellAfter := parseFZV(t, filepath.Join(dir2, "METAL-BELL.fzv"))

	if bellAfter.LFORate != bellBefore.LFORate {
		t.Errorf("METAL-BELL LFO rate changed: %d -> %d", bellBefore.LFORate, bellAfter.LFORate)
	}
	if bellAfter.FilterCutoff != bellBefore.FilterCutoff {
		t.Errorf("METAL-BELL cutoff changed: %d -> %d", bellBefore.FilterCutoff, bellAfter.FilterCutoff)
	}
}

func TestApplyToFZFVoiceNotFound(t *testing.T) {
	t.Parallel()
	fzfPath := extractTestFZF(t, "../../testdata/synthetic/TECHNO.img", "FULL-DATA-FZ")
	err := ApplyToFZFVoice(fzfPath, "NONEXISTENT", []Patch{{Offset: 0xA0, Size: 1, Value: 42}})
	if err == nil {
		t.Error("expected error for non-existent voice")
	}
}

// ---------------------------------------------------------------------------
// Round-trip tests
// ---------------------------------------------------------------------------

func TestLFOPatchRoundTrip(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)

	patches, _ := BuildLFOPatches(3, 25, 100, 127, 10, 20, 50, 5, 0)
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}

	p := parseFZV(t, path)
	if p.LFOWaveform != "Triangle" {
		t.Errorf("waveform: got %q, want Triangle", p.LFOWaveform)
	}
	if p.LFORate != 25 {
		t.Errorf("rate: got %d, want 25", p.LFORate)
	}
	if p.LFOAttack != 127 {
		t.Errorf("attack: got %d, want 127", p.LFOAttack)
	}
	if p.LFODepthPitch != 10 {
		t.Errorf("pitch: got %d, want 10", p.LFODepthPitch)
	}
	if p.LFODepthAmp != 20 {
		t.Errorf("amp: got %d, want 20", p.LFODepthAmp)
	}
	if p.LFODepthFilter != 50 {
		t.Errorf("filter: got %d, want 50", p.LFODepthFilter)
	}
}

func TestFilterPatchRoundTrip(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)

	patches, _ := BuildFilterPatches(64, 7)
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}

	p := parseFZV(t, path)
	if p.FilterCutoff != 64 {
		t.Errorf("cutoff: got %d, want 64", p.FilterCutoff)
	}
	if p.FilterQ != 7 {
		t.Errorf("resonance: got %d, want 7", p.FilterQ)
	}
}

func TestNamePatchRoundTrip(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)

	patches, _ := BuildNamePatch("MY PAD")
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}

	p := parseFZV(t, path)
	if p.Name != "MY PAD" {
		t.Errorf("name: got %q, want MY PAD", p.Name)
	}
}

// ---------------------------------------------------------------------------
// Audio preservation test
// ---------------------------------------------------------------------------

func TestApplyToFZVUint16Patch(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	patch := Patch{Offset: disk.VoiceLFODelayOffset, Size: 2, Value: 50}
	if err := ApplyToFZV(path, []Patch{patch}); err != nil {
		t.Fatal(err)
	}
	params := parseFZV(t, path)
	if params.LFODelay != 50 {
		t.Errorf("LFODelay: got %d, want 50", params.LFODelay)
	}
}

func TestApplyPatchUnsupportedSize(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	err := ApplyToFZV(path, []Patch{{Offset: 0xA0, Size: 3, Value: 42}})
	if err == nil {
		t.Fatal("expected error for unsupported patch size")
	}
	if !errors.Is(err, ErrUnsupportedPatch) {
		t.Errorf("expected ErrUnsupportedPatch, got %v", err)
	}
}

func TestApplyToFZFVoiceInvalidFZF(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bad.fzf")
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	err := ApplyToFZFVoice(path, "TEST", []Patch{{Offset: 0xA0, Size: 1, Value: 42}})
	if err == nil {
		t.Error("expected error for invalid FZF file")
	}
}

func TestApplyToFZFVoiceCaseInsensitive(t *testing.T) {
	t.Parallel()
	fzfPath := extractTestFZF(t, "../../testdata/synthetic/TECHNO.img", "FULL-DATA-FZ")
	patches, _ := BuildLFOPatches(Unchanged, 10, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
	if err := ApplyToFZFVoice(fzfPath, "cowbell", patches); err != nil {
		t.Fatalf("case-insensitive lookup failed: %v", err)
	}
}

// TestApplyToFZFVoiceSyncsBankKeyRange verifies that BuildKeyRangePatch edits
// propagate from the voice header into every bank site's per-split key-range
// array (spec §2-2: hwid[64]/lwid[64]/cent[64]). Hardware playback reads the
// bank arrays when the FZF is loaded as a bank, so a voice-header-only patch
// would be a silent no-op on the FZ-1. Regression guard for F14.
func TestApplyToFZFVoiceSyncsBankKeyRange(t *testing.T) {
	t.Parallel()
	fzfPath := extractTestFZF(t, "../../testdata/synthetic/TECHNO.img", "FULL-DATA-FZ")

	// Locate the voice slot for COWBELL and capture pre-edit bank bytes.
	dataBefore, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := fzutil.ParseFZFHeader(dataBefore)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}
	idx, err := findVoiceIndex(dataBefore, hdr, "COWBELL")
	if err != nil {
		t.Fatalf("findVoiceIndex: %v", err)
	}
	sites := fzutil.FindBankSitesForVoice(dataBefore, hdr, idx)
	if len(sites) == 0 {
		t.Fatalf("COWBELL voice (slot %d) has no bank sites; can't test propagation", idx)
	}

	keyLow, keyHigh, root := 40, 60, 50
	patches, err := BuildKeyRangePatch(keyLow, keyHigh, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToFZFVoice(fzfPath, "COWBELL", patches); err != nil {
		t.Fatal(err)
	}

	dataAfter, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	voiceOffset := disk.VoiceSlotOffset(hdr.VoiceAreaStart, idx)

	// 1. Voice-header bytes carry the new values.
	if got := int(dataAfter[voiceOffset+disk.VoiceKeyHighOffset]); got != keyHigh {
		t.Errorf("voice-header key-high: got %d, want %d", got, keyHigh)
	}
	if got := int(dataAfter[voiceOffset+disk.VoiceKeyLowOffset]); got != keyLow {
		t.Errorf("voice-header key-low: got %d, want %d", got, keyLow)
	}
	if got := int(dataAfter[voiceOffset+disk.VoiceKeyCentOffset]); got != root {
		t.Errorf("voice-header cent: got %d, want %d", got, root)
	}

	// 2. Every bank site for the slot carries the same new bytes.
	for _, site := range sites {
		base := site.BankIdx * disk.SectorSize
		if got := int(dataAfter[base+disk.BankKeyHighOffset+site.SplitIdx]); got != keyHigh {
			t.Errorf("bank %d split %d hwid: got %d, want %d", site.BankIdx, site.SplitIdx, got, keyHigh)
		}
		if got := int(dataAfter[base+disk.BankKeyLowOffset+site.SplitIdx]); got != keyLow {
			t.Errorf("bank %d split %d lwid: got %d, want %d", site.BankIdx, site.SplitIdx, got, keyLow)
		}
		if got := int(dataAfter[base+disk.BankKeyCentOffset+site.SplitIdx]); got != root {
			t.Errorf("bank %d split %d cent: got %d, want %d", site.BankIdx, site.SplitIdx, got, root)
		}
	}
}

// TestApplyToFZFVoiceNonKeyRangePatchSkipsBank confirms that patches with no
// bank counterpart (LFO, filter, envelope, etc.) do NOT touch the bank sector.
// Regression guard against over-eager bank propagation.
func TestApplyToFZFVoiceNonKeyRangePatchSkipsBank(t *testing.T) {
	t.Parallel()
	fzfPath := extractTestFZF(t, "../../testdata/synthetic/TECHNO.img", "FULL-DATA-FZ")

	dataBefore, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := fzutil.ParseFZFHeader(dataBefore)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}
	idx, err := findVoiceIndex(dataBefore, hdr, "COWBELL")
	if err != nil {
		t.Fatalf("findVoiceIndex: %v", err)
	}
	sites := fzutil.FindBankSitesForVoice(dataBefore, hdr, idx)
	if len(sites) == 0 {
		t.Fatal("COWBELL has no bank sites")
	}
	// Snapshot the three key-range bytes at every site.
	type siteSnap struct {
		hwid, lwid, cent byte
	}
	snaps := make([]siteSnap, len(sites))
	for i, site := range sites {
		base := site.BankIdx * disk.SectorSize
		snaps[i] = siteSnap{
			hwid: dataBefore[base+disk.BankKeyHighOffset+site.SplitIdx],
			lwid: dataBefore[base+disk.BankKeyLowOffset+site.SplitIdx],
			cent: dataBefore[base+disk.BankKeyCentOffset+site.SplitIdx],
		}
	}

	// Apply an LFO patch (no bank counterpart).
	patches, err := BuildLFOPatches(Unchanged, 42, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToFZFVoice(fzfPath, "COWBELL", patches); err != nil {
		t.Fatal(err)
	}

	dataAfter, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, site := range sites {
		base := site.BankIdx * disk.SectorSize
		if got := dataAfter[base+disk.BankKeyHighOffset+site.SplitIdx]; got != snaps[i].hwid {
			t.Errorf("bank %d split %d hwid changed unexpectedly: %d -> %d", site.BankIdx, site.SplitIdx, snaps[i].hwid, got)
		}
		if got := dataAfter[base+disk.BankKeyLowOffset+site.SplitIdx]; got != snaps[i].lwid {
			t.Errorf("bank %d split %d lwid changed unexpectedly: %d -> %d", site.BankIdx, site.SplitIdx, snaps[i].lwid, got)
		}
		if got := dataAfter[base+disk.BankKeyCentOffset+site.SplitIdx]; got != snaps[i].cent {
			t.Errorf("bank %d split %d cent changed unexpectedly: %d -> %d", site.BankIdx, site.SplitIdx, snaps[i].cent, got)
		}
	}
}

// TestApplyToFZFVoiceSyncsBankKeyRangeMultiBank exercises bank-sector sync on
// a synthetic FZF where the target voice slot is referenced by multiple banks
// at distinct key-splits; the case TECHNO.img's `COWBELL` may or may not hit
// depending on its bank topology. Mirrors fzfmidi/fzfoutput's multi-bank tests.
func TestApplyToFZFVoiceSyncsBankKeyRangeMultiBank(t *testing.T) {
	t.Parallel()

	// Build a 2-bank FZF where slot 0 is referenced from (bank 0, split 0)
	// and (bank 1, split 1): a slot that fans across banks.
	_, fzfPath := fzfbuilder.MakeTestFZF(t, []string{"ALPHA", "BRAVO"})
	data, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	// Stamp a second bank sector so the FZF has two banks, then point its
	// split 1 at voice slot 0 (ALPHA). voicebuild emits a 1-bank dump, so
	// we prepend a duplicate bank sector and shift the voice area.
	bank0 := make([]byte, disk.SectorSize)
	copy(bank0, data[:disk.SectorSize])
	// Modify bank 0 in place: bstep stays 2, vp[]=[0,1].
	// Create a clone bank 1 that maps split 0 -> slot 1, split 1 -> slot 0.
	bank1 := make([]byte, disk.SectorSize)
	copy(bank1, bank0)
	binary.LittleEndian.PutUint16(bank1[disk.BankVoiceNumOffset:], 1)
	binary.LittleEndian.PutUint16(bank1[disk.BankVoiceNumOffset+2:], 0)
	// Distinct stamped key-range bytes so we can prove they got overwritten.
	bank1[disk.BankKeyLowOffset+0] = 11
	bank1[disk.BankKeyLowOffset+1] = 22
	bank1[disk.BankKeyHighOffset+0] = 33
	bank1[disk.BankKeyHighOffset+1] = 44
	bank1[disk.BankKeyCentOffset+0] = 55
	bank1[disk.BankKeyCentOffset+1] = 66

	// New layout: [bank0][bank1][voice area...]
	newData := make([]byte, 0, len(data)+disk.SectorSize)
	newData = append(newData, bank0...)
	newData = append(newData, bank1...)
	newData = append(newData, data[disk.SectorSize:]...)
	if err := os.WriteFile(fzfPath, newData, 0644); err != nil { //nolint:gosec // G703: fzfPath comes from fzfbuilder.MakeTestFZF (t.TempDir())
		t.Fatal(err)
	}

	// Sanity: parser sees two banks and resolves the voice area correctly.
	hdr, err := fzutil.ParseFZFHeader(newData)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}
	if hdr.NBankSectors != 2 {
		t.Fatalf("expected 2 banks, got %d", hdr.NBankSectors)
	}
	idx, err := findVoiceIndex(newData, hdr, "ALPHA")
	if err != nil {
		t.Fatalf("findVoiceIndex: %v", err)
	}
	sites := fzutil.FindBankSitesForVoice(newData, hdr, idx)
	if len(sites) < 2 {
		t.Fatalf("expected ALPHA referenced from >=2 sites, got %v", sites)
	}

	keyLow, keyHigh, root := 30, 90, 60
	patches, _ := BuildKeyRangePatch(keyLow, keyHigh, root)
	if err := ApplyToFZFVoice(fzfPath, "ALPHA", patches); err != nil {
		t.Fatal(err)
	}

	dataAfter, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, site := range sites {
		base := site.BankIdx * disk.SectorSize
		if got := int(dataAfter[base+disk.BankKeyHighOffset+site.SplitIdx]); got != keyHigh {
			t.Errorf("bank %d split %d hwid: got %d, want %d", site.BankIdx, site.SplitIdx, got, keyHigh)
		}
		if got := int(dataAfter[base+disk.BankKeyLowOffset+site.SplitIdx]); got != keyLow {
			t.Errorf("bank %d split %d lwid: got %d, want %d", site.BankIdx, site.SplitIdx, got, keyLow)
		}
		if got := int(dataAfter[base+disk.BankKeyCentOffset+site.SplitIdx]); got != root {
			t.Errorf("bank %d split %d cent: got %d, want %d", site.BankIdx, site.SplitIdx, got, root)
		}
	}
}

func TestBuildLFOPatchesValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		wave   int
		rate   int
		delay  int
		attack int
		pitch  int
		amp    int
		filter int
		q      int
	}{
		{"invalid waveform", 10, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
		{"invalid rate", Unchanged, 200, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
		{"invalid delay", Unchanged, Unchanged, 70000, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
		{"invalid attack", Unchanged, Unchanged, Unchanged, 200, Unchanged, Unchanged, Unchanged, Unchanged},
		{"invalid pitch", Unchanged, Unchanged, Unchanged, Unchanged, 200, Unchanged, Unchanged, Unchanged},
		{"invalid amp", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 200, Unchanged, Unchanged},
		{"invalid filter", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 200, Unchanged},
		{"invalid q", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := BuildLFOPatches(tc.wave, tc.rate, tc.delay, tc.attack, tc.pitch, tc.amp, tc.filter, tc.q, 0)
			if err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestBuildFilterPatchesValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		cutoff    int
		resonance int
	}{
		{"invalid cutoff", 200, Unchanged},
		{"invalid resonance", Unchanged, 128},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := BuildFilterPatches(tc.cutoff, tc.resonance)
			if err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestLFODelayPatchRoundTrip(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	patches, err := BuildLFOPatches(Unchanged, Unchanged, 100, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}
	params := parseFZV(t, path)
	if params.LFODelay != 100 {
		t.Errorf("LFODelay: got %d, want 100", params.LFODelay)
	}
}

func TestEditPreservesFileSize(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	lfoPatches, _ := BuildLFOPatches(2, 50, 200, 100, 10, 20, 30, 5, 0)
	filterPatches, _ := BuildFilterPatches(80, 10)
	all := make([]Patch, 0, len(lfoPatches)+len(filterPatches))
	all = append(all, lfoPatches...)
	all = append(all, filterPatches...)
	if err := ApplyToFZV(path, all); err != nil {
		t.Fatal(err)
	}
	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if infoBefore.Size() != infoAfter.Size() {
		t.Errorf("file size changed: %d -> %d", infoBefore.Size(), infoAfter.Size())
	}
}

func TestPatchPreservesAudio(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)

	dataBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	audioBefore := make([]byte, len(dataBefore)-disk.SectorSize)
	copy(audioBefore, dataBefore[disk.SectorSize:])

	patches, _ := BuildLFOPatches(0, 42, 200, 127, 10, 20, 50, 5, 0)
	filterPatches, _ := BuildFilterPatches(64, 7)
	all := make([]Patch, 0, len(patches)+len(filterPatches))
	all = append(all, patches...)
	all = append(all, filterPatches...)
	if err := ApplyToFZV(path, all); err != nil {
		t.Fatal(err)
	}

	dataAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	audioAfter := dataAfter[disk.SectorSize:]

	if len(audioBefore) != len(audioAfter) {
		t.Fatalf("audio length changed: %d -> %d", len(audioBefore), len(audioAfter))
	}
	for i := range audioBefore {
		if audioBefore[i] != audioAfter[i] {
			t.Fatalf("audio byte %d changed: 0x%02x -> 0x%02x", i, audioBefore[i], audioAfter[i])
		}
	}
}

func extractBrassVoice(t *testing.T, voiceName string) *fzvinfo.VoiceParams {
	t.Helper()
	fzfPath := extractTestFZF(t, "../../testdata/synthetic/BRASS.img", "FULL-DATA-FZ")
	dir := t.TempDir()
	if err := voiceunpack.Unpack(fzfPath, dir); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	return parseFZV(t, filepath.Join(dir, voiceName+".fzv"))
}

// ---------------------------------------------------------------------------
// Rate/stop conversion tests
// ---------------------------------------------------------------------------

func TestRateDisplayToByte(t *testing.T) {
	t.Parallel()
	cases := []struct {
		display int
		want    uint8
	}{
		{0, 0x00},
		{99, 127},
		{50, 64},
		{1, 2},
		{25, 32},
		{75, 96},
	}
	for _, tc := range cases {
		got := disk.RateDisplayToByte(tc.display)
		if got != tc.want {
			t.Errorf("disk.RateDisplayToByte(%d) = 0x%02x, want 0x%02x", tc.display, got, tc.want)
		}
	}
}

func TestStopDisplayToByte(t *testing.T) {
	t.Parallel()
	cases := []struct {
		display int
		want    uint8
	}{
		{0, 0},
		{99, 255},
		{50, 127},
		{1, 1},
		{85, 217},
	}
	for _, tc := range cases {
		got := disk.StopDisplayToByte(tc.display)
		if got != tc.want {
			t.Errorf("disk.StopDisplayToByte(%d) = %d, want %d", tc.display, got, tc.want)
		}
	}
}

func TestRateByteToDisplay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		b    uint8
		want int
	}{
		{127, 99},
		{0, 0},
		{0xC0, 50},
		{0xFD, 97},
		{0x80, 0},
		{64, 50},
		{1, 0},
		{1 | 0x80, 0},
		{0xFF, 99},
		{32, 25},
		{96, 75},
		{126, 98},
	}
	for _, tc := range cases {
		got := disk.RateByteToDisplay(tc.b)
		if got != tc.want {
			t.Errorf("disk.RateByteToDisplay(0x%02x) = %d, want %d", tc.b, got, tc.want)
		}
	}
}

func TestRateByteToDisplayIgnoresSignBit(t *testing.T) {
	t.Parallel()
	for mag := uint8(0); mag <= disk.RateMagMask; mag++ {
		rising := disk.RateByteToDisplay(mag)
		falling := disk.RateByteToDisplay(mag | disk.RateSignBit)
		if rising != falling {
			t.Errorf("sign bit changes display: mag=%d rising=%d falling=%d", mag, rising, falling)
		}
	}
}

func TestHardwareRateKFValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mag  uint8
		want int
	}{
		{0, 0},
		{1, 0},
		{32, 25},
		{63, 49},
		{64, 50},
		{96, 75},
		{126, 98},
		{127, 99},
	}
	for _, tc := range cases {
		got := disk.RateByteToDisplay(tc.mag)
		if got != tc.want {
			t.Errorf("disk.RateByteToDisplay(%d) = %d, want %d (hardware)", tc.mag, got, tc.want)
		}
	}
}

func TestHardwareLevelScaleValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		b    uint8
		want int
	}{
		{0, 0},
		{25, 10},
		{50, 20},
		{56, 22},
		{66, 26},
		{75, 30},
		{100, 39},
		{150, 59},
		{200, 78},
		{218, 85},
		{255, 99},
	}
	for _, tc := range cases {
		got := disk.StopByteToDisplay(tc.b)
		if got != tc.want {
			t.Errorf("disk.StopByteToDisplay(%d) = %d, want %d (hardware)", tc.b, got, tc.want)
		}
	}
}

func TestStopByteToDisplay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		b    uint8
		want int
	}{
		{255, 99},
		{0, 0},
		{218, 85},
		{66, 26},
		{129, 51},
		{150, 59},
		{75, 30},
		{50, 20},
		{25, 10},
	}
	for _, tc := range cases {
		got := disk.StopByteToDisplay(tc.b)
		if got != tc.want {
			t.Errorf("disk.StopByteToDisplay(%d) = %d, want %d", tc.b, got, tc.want)
		}
	}
}

func TestConversionRoundTripRates(t *testing.T) {
	t.Parallel()
	for d := range 100 {
		b := disk.RateDisplayToByte(d)
		back := disk.RateByteToDisplay(b)
		if testutil.Abs(back-d) > 1 {
			t.Errorf("rate round-trip %d -> 0x%02x -> %d (diff %d)", d, b, back, testutil.Abs(back-d))
		}
	}
}

func TestConversionRoundTripStops(t *testing.T) {
	t.Parallel()
	for d := range 100 {
		b := disk.StopDisplayToByte(d)
		back := disk.StopByteToDisplay(b)
		if testutil.Abs(back-d) > 1 {
			t.Errorf("stop round-trip %d -> %d -> %d (diff %d)", d, b, back, testutil.Abs(back-d))
		}
	}
}

func TestBrassEnvelopeDisplayValues(t *testing.T) {
	t.Parallel()
	p := extractBrassVoice(t, "BRASS1 D3 1")
	if got := disk.RateByteToDisplay(p.DCARates[0]); got != 99 {
		t.Errorf("DCARates[0] display = %d, want 99", got)
	}
	if got := disk.StopByteToDisplay(p.DCAStops[0]); got != 85 {
		t.Errorf("DCAStops[0] display = %d, want 85", got)
	}
	if got := disk.StopByteToDisplay(p.DCAStops[1]); got != 99 {
		t.Errorf("DCAStops[1] display = %d, want 99", got)
	}
}

// ---------------------------------------------------------------------------
// DCA envelope patch tests
// ---------------------------------------------------------------------------

func TestBuildDCAPatches(t *testing.T) {
	t.Parallel()
	rates := [disk.EnvelopeStages]int{99, 50, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	stops := [disk.EnvelopeStages]int{85, 99, 0, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	origRates := [disk.EnvelopeStages]uint8{0x7F, 0xC0, 0, 0, 0, 0, 0, 0}
	patches, err := BuildDCAPatches(2, 3, rates, stops, origRates)
	if err != nil {
		t.Fatal(err)
	}
	offsets := make(map[int]uint16)
	for _, p := range patches {
		offsets[p.Offset] = p.Value
	}
	if offsets[disk.VoiceDCASusOffset] != 2 {
		t.Errorf("sustain: got %d, want 2", offsets[disk.VoiceDCASusOffset])
	}
	if offsets[disk.VoiceDCAEndOffset] != 3 {
		t.Errorf("end: got %d, want 3", offsets[disk.VoiceDCAEndOffset])
	}
	if offsets[disk.VoiceDCARateOffset] != uint16(disk.RateDisplayToByte(99)) {
		t.Errorf("rate[0]: got %d, want %d", offsets[disk.VoiceDCARateOffset], disk.RateDisplayToByte(99))
	}
	rate1 := offsets[disk.VoiceDCARateOffset+1]
	if rate1&uint16(disk.RateSignBit) == 0 {
		t.Error("rate[1] should preserve falling sign bit from origRates")
	}
	if rate1&uint16(disk.RateMagMask) != uint16(disk.RateDisplayToByte(50)) {
		t.Errorf("rate[1] magnitude: got %d, want %d", rate1&uint16(disk.RateMagMask), disk.RateDisplayToByte(50))
	}
	if _, ok := offsets[disk.VoiceDCARateOffset+2]; ok {
		t.Error("rate[2] should not be patched (Unchanged)")
	}
	if _, ok := offsets[disk.VoiceDCAStopOffset+3]; ok {
		t.Error("stop[3] should not be patched (Unchanged)")
	}
}

func TestBuildDCAPatchesAllSkipped(t *testing.T) {
	t.Parallel()
	rates := [disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	stops := [disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	var origRates [disk.EnvelopeStages]uint8
	patches, err := BuildDCAPatches(Unchanged, Unchanged, rates, stops, origRates)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 0 {
		t.Errorf("expected 0 patches, got %d", len(patches))
	}
}

func TestBuildDCAPatchesValidation(t *testing.T) {
	t.Parallel()
	unchanged := [disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	var origRates [disk.EnvelopeStages]uint8
	cases := []struct {
		name    string
		sustain int
		end     int
		rates   [disk.EnvelopeStages]int
		stops   [disk.EnvelopeStages]int
	}{
		{"sustain out of range", 8, Unchanged, unchanged, unchanged},
		{"end out of range", Unchanged, 8, unchanged, unchanged},
		{"rate too high", Unchanged, Unchanged, [disk.EnvelopeStages]int{100, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}, unchanged},
		{"rate negative", Unchanged, Unchanged, [disk.EnvelopeStages]int{-1, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}, unchanged},
		{"stop too high", Unchanged, Unchanged, unchanged, [disk.EnvelopeStages]int{100, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}},
		{"stop negative", Unchanged, Unchanged, unchanged, [disk.EnvelopeStages]int{-1, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := BuildDCAPatches(tc.sustain, tc.end, tc.rates, tc.stops, origRates)
			if err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DCF envelope patch tests
// ---------------------------------------------------------------------------

func TestBuildDCFPatches(t *testing.T) {
	t.Parallel()
	rates := [disk.EnvelopeStages]int{99, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	stops := [disk.EnvelopeStages]int{26, 22, 0, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	var origRates [disk.EnvelopeStages]uint8
	origRates[0] = 0x7F
	patches, err := BuildDCFPatches(1, 2, rates, stops, origRates)
	if err != nil {
		t.Fatal(err)
	}
	offsets := make(map[int]uint16)
	for _, p := range patches {
		offsets[p.Offset] = p.Value
	}
	if offsets[disk.VoiceDCFSusOffset] != 1 {
		t.Errorf("sustain: got %d, want 1", offsets[disk.VoiceDCFSusOffset])
	}
	if offsets[disk.VoiceDCFEndOffset] != 2 {
		t.Errorf("end: got %d, want 2", offsets[disk.VoiceDCFEndOffset])
	}
	if offsets[disk.VoiceDCFRateOffset] != uint16(disk.RateDisplayToByte(99)) {
		t.Errorf("rate[0]: got %d, want %d", offsets[disk.VoiceDCFRateOffset], disk.RateDisplayToByte(99))
	}
	if _, ok := offsets[disk.VoiceDCFRateOffset+1]; ok {
		t.Error("rate[1] should not be patched (Unchanged)")
	}
	stop0 := offsets[disk.VoiceDCFStopOffset]
	if disk.StopByteToDisplay(uint8(stop0)) != 26 { //nolint:gosec // test value fits in uint8
		t.Errorf("stop[0] display: got %d, want 26", disk.StopByteToDisplay(uint8(stop0))) //nolint:gosec // test value fits in uint8
	}
	stop1 := offsets[disk.VoiceDCFStopOffset+1]
	if disk.StopByteToDisplay(uint8(stop1)) != 22 { //nolint:gosec // test value fits in uint8
		t.Errorf("stop[1] display: got %d, want 22", disk.StopByteToDisplay(uint8(stop1))) //nolint:gosec // test value fits in uint8
	}
	stop2 := offsets[disk.VoiceDCFStopOffset+2]
	if stop2 != 0 {
		t.Errorf("stop[2]: got %d, want 0", stop2)
	}
}

func TestBuildDCFPatchesValidation(t *testing.T) {
	t.Parallel()
	unchanged := [disk.EnvelopeStages]int{Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	var origRates [disk.EnvelopeStages]uint8
	_, err := BuildDCFPatches(8, Unchanged, unchanged, unchanged, origRates)
	if err == nil {
		t.Error("expected error for sustain=8")
	}
}

// ---------------------------------------------------------------------------
// DCA/DCF patch round-trip tests
// ---------------------------------------------------------------------------

func TestDCAPatchRoundTrip(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	origRates := [disk.EnvelopeStages]uint8{0x7F, 0xC0, 0xC0, 0, 0, 0, 0, 0}
	rates := [disk.EnvelopeStages]int{99, 50, 7, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	stops := [disk.EnvelopeStages]int{85, 99, 0, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	patches, err := BuildDCAPatches(2, 3, rates, stops, origRates)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}
	p := parseFZV(t, path)
	if p.DCASustain != 2 {
		t.Errorf("DCASustain = %d, want 2", p.DCASustain)
	}
	if p.DCAEnd != 3 {
		t.Errorf("DCAEnd = %d, want 3", p.DCAEnd)
	}
	if disk.RateByteToDisplay(p.DCARates[0]) != 99 {
		t.Errorf("DCARates[0] display = %d, want 99", disk.RateByteToDisplay(p.DCARates[0]))
	}
	if disk.RateByteToDisplay(p.DCARates[1]) != 50 {
		t.Errorf("DCARates[1] display = %d, want 50", disk.RateByteToDisplay(p.DCARates[1]))
	}
	if p.DCARates[1]&disk.RateSignBit == 0 {
		t.Error("DCARates[1] should preserve falling sign bit")
	}
	if disk.RateByteToDisplay(p.DCARates[2]) != 7 {
		t.Errorf("DCARates[2] display = %d, want 7", disk.RateByteToDisplay(p.DCARates[2]))
	}
	if disk.StopByteToDisplay(p.DCAStops[0]) != 85 {
		t.Errorf("DCAStops[0] display = %d, want 85", disk.StopByteToDisplay(p.DCAStops[0]))
	}
	if disk.StopByteToDisplay(p.DCAStops[1]) != 99 {
		t.Errorf("DCAStops[1] display = %d, want 99", disk.StopByteToDisplay(p.DCAStops[1]))
	}
	if p.DCAStops[2] != 0 {
		t.Errorf("DCAStops[2] = %d, want 0", p.DCAStops[2])
	}
}

func TestDCFPatchRoundTrip(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	origRates := [disk.EnvelopeStages]uint8{0x7F, 0x89, 0x94, 0, 0, 0, 0, 0}
	rates := [disk.EnvelopeStages]int{99, 7, 15, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	stops := [disk.EnvelopeStages]int{26, 22, 0, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	patches, err := BuildDCFPatches(1, 2, rates, stops, origRates)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}
	p := parseFZV(t, path)
	if p.DCFSustain != 1 {
		t.Errorf("DCFSustain = %d, want 1", p.DCFSustain)
	}
	if p.DCFEnd != 2 {
		t.Errorf("DCFEnd = %d, want 2", p.DCFEnd)
	}
	if disk.RateByteToDisplay(p.DCFRates[0]) != 99 {
		t.Errorf("DCFRates[0] display = %d, want 99", disk.RateByteToDisplay(p.DCFRates[0]))
	}
	if disk.RateByteToDisplay(p.DCFRates[1]) != 7 {
		t.Errorf("DCFRates[1] display = %d, want 7", disk.RateByteToDisplay(p.DCFRates[1]))
	}
	if p.DCFRates[1]&disk.RateSignBit == 0 {
		t.Error("DCFRates[1] should preserve falling sign bit")
	}
	if disk.StopByteToDisplay(p.DCFStops[0]) != 26 {
		t.Errorf("DCFStops[0] display = %d, want 26", disk.StopByteToDisplay(p.DCFStops[0]))
	}
	if disk.StopByteToDisplay(p.DCFStops[1]) != 22 {
		t.Errorf("DCFStops[1] display = %d, want 22", disk.StopByteToDisplay(p.DCFStops[1]))
	}
	if p.DCFStops[2] != 0 {
		t.Errorf("DCFStops[2] = %d, want 0", p.DCFStops[2])
	}
}

func TestDCAEditPreservesOtherParams(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)

	lfoPatches, _ := BuildLFOPatches(2, 50, Unchanged, 100, 10, 20, 30, Unchanged, 0)
	filterPatches, _ := BuildFilterPatches(64, 7)
	setup := make([]Patch, 0, len(lfoPatches)+len(filterPatches))
	setup = append(setup, lfoPatches...)
	setup = append(setup, filterPatches...)
	if err := ApplyToFZV(path, setup); err != nil {
		t.Fatal(err)
	}
	before := parseFZV(t, path)

	rates := [disk.EnvelopeStages]int{99, 50, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	stops := [disk.EnvelopeStages]int{85, 99, 0, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged}
	origRates := [disk.EnvelopeStages]uint8{0x7F, 0xC0, 0, 0, 0, 0, 0, 0}
	dcaPatches, err := BuildDCAPatches(2, 3, rates, stops, origRates)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToFZV(path, dcaPatches); err != nil {
		t.Fatal(err)
	}
	after := parseFZV(t, path)

	if after.LFORate != before.LFORate {
		t.Errorf("LFORate changed: %d -> %d", before.LFORate, after.LFORate)
	}
	if after.LFOAttack != before.LFOAttack {
		t.Errorf("LFOAttack changed: %d -> %d", before.LFOAttack, after.LFOAttack)
	}
	if after.LFODepthPitch != before.LFODepthPitch {
		t.Errorf("LFODepthPitch changed: %d -> %d", before.LFODepthPitch, after.LFODepthPitch)
	}
	if after.LFODepthAmp != before.LFODepthAmp {
		t.Errorf("LFODepthAmp changed: %d -> %d", before.LFODepthAmp, after.LFODepthAmp)
	}
	if after.LFODepthFilter != before.LFODepthFilter {
		t.Errorf("LFODepthFilter changed: %d -> %d", before.LFODepthFilter, after.LFODepthFilter)
	}
	if after.FilterCutoff != before.FilterCutoff {
		t.Errorf("FilterCutoff changed: %d -> %d", before.FilterCutoff, after.FilterCutoff)
	}
	if after.FilterQ != before.FilterQ {
		t.Errorf("FilterQ changed: %d -> %d", before.FilterQ, after.FilterQ)
	}
}

// ---------------------------------------------------------------------------
// Modulation patch tests
// ---------------------------------------------------------------------------

func TestBuildModulationPatchesAllSet(t *testing.T) {
	t.Parallel()
	patches, err := BuildModulationPatches(5, -3, 10, -15, 90, 100, 50, -50, 127)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 9 {
		t.Fatalf("expected 9 patches, got %d", len(patches))
	}
	offsets := make(map[int]uint16)
	for _, p := range patches {
		offsets[p.Offset] = p.Value
	}
	if offsets[disk.VoiceDCAKFOffset] != uint16(disk.KFDisplayToByte(5)) {
		t.Errorf("dca-level-kf: got %d, want %d", offsets[disk.VoiceDCAKFOffset], disk.KFDisplayToByte(5))
	}
	if offsets[disk.VoiceDCARSOffset] != uint16(disk.KFDisplayToByte(-3)) {
		t.Errorf("dca-rate-kf: got %d, want %d", offsets[disk.VoiceDCARSOffset], disk.KFDisplayToByte(-3))
	}
	if offsets[disk.VoiceDCFKFOffset] != uint16(disk.KFDisplayToByte(10)) {
		t.Errorf("dcf-level-kf: got %d, want %d", offsets[disk.VoiceDCFKFOffset], disk.KFDisplayToByte(10))
	}
	if offsets[disk.VoiceDCFRSOffset] != uint16(disk.KFDisplayToByte(-15)) {
		t.Errorf("dcf-rate-kf: got %d, want %d", offsets[disk.VoiceDCFRSOffset], disk.KFDisplayToByte(-15))
	}
	if offsets[disk.VoiceVelDCAKFOffset] != 90 {
		t.Errorf("vel-dca-kf: got %d, want 90", offsets[disk.VoiceVelDCAKFOffset])
	}
	if offsets[disk.VoiceVelDCFKFOffset] != 100 {
		t.Errorf("vel-dcf-kf: got %d, want 100", offsets[disk.VoiceVelDCFKFOffset])
	}
	wantDCQKF := uint16(50)
	negFifty := int8(-50)
	wantDCARS := uint16(uint8(negFifty)) //nolint:gosec // G115: two's complement reinterpretation
	wantDCFRS := uint16(127)
	if offsets[disk.VoiceVelDCQKFOffset] != wantDCQKF {
		t.Errorf("vel-dcq-kf: got %d, want %d", offsets[disk.VoiceVelDCQKFOffset], wantDCQKF)
	}
	if offsets[disk.VoiceVelDCARSOffset] != wantDCARS {
		t.Errorf("vel-dca-rs: got %d, want %d", offsets[disk.VoiceVelDCARSOffset], wantDCARS)
	}
	if offsets[disk.VoiceVelDCFRSOffset] != wantDCFRS {
		t.Errorf("vel-dcf-rs: got %d, want %d", offsets[disk.VoiceVelDCFRSOffset], wantDCFRS)
	}
}

func TestBuildModulationPatchesAllUnchanged(t *testing.T) {
	t.Parallel()
	patches, err := BuildModulationPatches(Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 0 {
		t.Errorf("expected 0 patches when all Unchanged, got %d", len(patches))
	}
}

func TestBuildModulationPatchesPartial(t *testing.T) {
	t.Parallel()
	patches, err := BuildModulationPatches(Unchanged, Unchanged, Unchanged, Unchanged, 40, 60, Unchanged, Unchanged, Unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("expected 2 patches, got %d", len(patches))
	}
	offsets := make(map[int]uint16)
	for _, p := range patches {
		offsets[p.Offset] = p.Value
	}
	if offsets[disk.VoiceVelDCAKFOffset] != 40 {
		t.Errorf("vel-dca-kf: got %d, want 40", offsets[disk.VoiceVelDCAKFOffset])
	}
	if offsets[disk.VoiceVelDCFKFOffset] != 60 {
		t.Errorf("vel-dcf-kf: got %d, want 60", offsets[disk.VoiceVelDCFKFOffset])
	}
}

func TestBuildModulationPatchesValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		dcaKF    int
		dcaRS    int
		dcfKF    int
		dcfRS    int
		velDCAKF int
		velDCFKF int
		velDCQKF int
		velDCARS int
		velDCFRS int
	}{
		{"dca-level-kf too high", 16, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
		{"dca-rate-kf too high", Unchanged, 16, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
		{"dcf-level-kf too high", Unchanged, Unchanged, 16, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
		{"dcf-rate-kf too high", Unchanged, Unchanged, Unchanged, 16, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged},
		{"vel-dca-kf too high", Unchanged, Unchanged, Unchanged, Unchanged, 128, Unchanged, Unchanged, Unchanged, Unchanged},
		{"vel-dcf-kf too high", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 128, Unchanged, Unchanged, Unchanged},
		{"vel-dcq-kf too high", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 128, Unchanged, Unchanged},
		{"vel-dcq-kf too low", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, -128, Unchanged, Unchanged},
		{"vel-dca-rs too high", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 128, Unchanged},
		{"vel-dca-rs too low", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, -128, Unchanged},
		{"vel-dcf-rs too high", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 128},
		{"vel-dcf-rs too low", Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, -128},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := BuildModulationPatches(tc.dcaKF, tc.dcaRS, tc.dcfKF, tc.dcfRS, tc.velDCAKF, tc.velDCFKF, tc.velDCQKF, tc.velDCARS, tc.velDCFRS)
			if err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

// TestApplyToFZVSignedVelDCAKFAndVelDCFKFRoundTrip writes negative values
// to vel_dca_kf and vel_dcf_kf and verifies they round-trip via fzvinfo.
// Per spec §2-1 both bytes are signed -127..+127 (a minus number assigns
// the lower velocity to generate the bigger volume / filter cutoff). This
// guards against F17 regressing: previously these two fields rejected
// negative values at the CLI/voiceedit layer even though fzvinfo parsed
// them as int8.
func TestApplyToFZVSignedVelDCAKFAndVelDCFKFRoundTrip(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)
	patches, err := BuildModulationPatches(
		Unchanged, Unchanged, Unchanged, Unchanged,
		-50, -75,
		Unchanged, Unchanged, Unchanged,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToFZV(path, patches); err != nil {
		t.Fatal(err)
	}
	after := parseFZV(t, path)
	if int(after.VelDCAKF) != -50 {
		t.Errorf("VelDCAKF: got %d, want -50", after.VelDCAKF)
	}
	if int(after.VelDCFKF) != -75 {
		t.Errorf("VelDCFKF: got %d, want -75", after.VelDCFKF)
	}
}

// TestApplyToFZVSignedVelModulationRoundTrip writes the three signed
// initial-touch velocity modulation fields (vel_dcq_kf, vel_dca_rs,
// vel_dcf_rs) through ApplyToFZV and verifies they round-trip via fzvinfo
// for the full -127..+127 range that the spec allows.
func TestApplyToFZVSignedVelModulationRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                         string
		velDCQKF, velDCARS, velDCFRS int
	}{
		{"positives", 50, 25, 100},
		{"negatives", -50, -25, -100},
		{"extremes", 127, -127, 127},
		{"mixed", -1, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := buildTestFZV(t)
			patches, err := BuildModulationPatches(
				Unchanged, Unchanged, Unchanged, Unchanged,
				Unchanged, Unchanged,
				tc.velDCQKF, tc.velDCARS, tc.velDCFRS,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := ApplyToFZV(path, patches); err != nil {
				t.Fatal(err)
			}
			after := parseFZV(t, path)
			if int(after.VelDCQKF) != tc.velDCQKF {
				t.Errorf("VelDCQKF: got %d, want %d", after.VelDCQKF, tc.velDCQKF)
			}
			if int(after.VelDCARS) != tc.velDCARS {
				t.Errorf("VelDCARS: got %d, want %d", after.VelDCARS, tc.velDCARS)
			}
			if int(after.VelDCFRS) != tc.velDCFRS {
				t.Errorf("VelDCFRS: got %d, want %d", after.VelDCFRS, tc.velDCFRS)
			}
		})
	}
}

// TestConcurrentApplyToFZVSafe is a regression test for the lost-write race
// between two processes (or goroutines) calling ApplyToFZV on the same file.
// Without the fileutil.WithFileLock guard around the read-modify-write cycle,
// concurrent calls can read the same baseline, each apply their own patch
// independently, and the last writer's atomic rename clobbers the other
// writer's changes. With the lock, every writer's edit is observable in the
// final file because each read-modify-write runs in its own critical section.
//
// The test asserts the weaker property that the file is coherent after the
// race: it is still a valid FZV and contains exactly one of the writers'
// LFORate values (no torn header). The acceptance check (one-of writers won)
// matches diskadd_test.go's TestConcurrentAddSafe pattern.
func TestConcurrentApplyToFZVSafe(t *testing.T) {
	t.Parallel()
	path := buildTestFZV(t)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	rates := make([]int, n)
	for i := range n {
		rates[i] = 10 + i // distinct non-zero values within 0..127
	}

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			patches, err := BuildLFOPatches(Unchanged, rates[idx], Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, Unchanged, 0)
			if err != nil {
				errs[idx] = err
				return
			}
			errs[idx] = ApplyToFZV(path, patches)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	// The file must still parse and its LFORate must equal one of the
	// writers' values. A torn header would either fail to parse or carry
	// a rate value none of the writers wrote.
	after := parseFZV(t, path)
	final := int(after.LFORate)
	wonBy := -1
	for i, r := range rates {
		if r == final {
			wonBy = i
			break
		}
	}
	if wonBy < 0 {
		t.Errorf("final LFORate=%d matches no writer; possible torn write or lost-write race (lock missing?)", final)
	}
}
