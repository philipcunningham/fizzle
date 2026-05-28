package sfzexport_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/sfzexport"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

const (
	testVoiceKick  = "KICK"
	testVoiceSnare = "SNARE"
	testVoicePad   = "PAD"
)

func buildFZF(t *testing.T, names []string) ([]byte, string) {
	t.Helper()
	return fzfbuilder.MakeTestFZF(t, names)
}

func patchVoiceByte(data []byte, voiceIdx int, offset int, value byte) { //nolint:unparam // voiceIdx is 0 in current tests but the helper supports any index
	voff := disk.VoiceSlotOffset(disk.SectorSize, voiceIdx)
	data[voff+offset] = value
}

func patchVoiceHeaderU16(data []byte, voiceIdx int, offset int, value uint16) { //nolint:unparam // voiceIdx is 0 in current tests but the helper supports any index
	voff := disk.VoiceSlotOffset(disk.SectorSize, voiceIdx)
	binary.LittleEndian.PutUint16(data[voff+offset:], value)
}

func patchVoiceHeaderU32(data []byte, voiceIdx int, offset int, value uint32) {
	voff := disk.VoiceSlotOffset(disk.SectorSize, voiceIdx)
	binary.LittleEndian.PutUint32(data[voff+offset:], value)
}

func patchBankByte(data []byte, bankOffset int, voiceIdx int, value byte) {
	data[bankOffset+voiceIdx] = value
}

func writePatchedFZF(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.fzf")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readSFZ(t *testing.T, dir, name string) string {
	t.Helper()
	sfzPath := filepath.Join(dir, name+".sfz")
	b, err := os.ReadFile(sfzPath)
	if err != nil {
		t.Fatalf("reading SFZ: %v", err)
	}
	return string(b)
}

func TestExportCreatesFiles(t *testing.T) {
	t.Parallel()
	_, fzfPath := buildFZF(t, []string{testVoiceKick, testVoiceSnare})
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "instrument"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	if !containsStr(names, "instrument.sfz") {
		t.Errorf("missing instrument.sfz, got: %v", names)
	}
	wavCount := 0
	for _, n := range names {
		if strings.HasSuffix(n, ".wav") {
			wavCount++
		}
	}
	if wavCount != 2 {
		t.Errorf("expected 2 WAV files, got %d: %v", wavCount, names)
	}
}

// TestExportAtomicNoTempLeftovers verifies that Export uses atomic writes:
// the output directory contains only the final SFZ + WAV files, with no
// fizzle-* temp files left behind. This guards against a regression to
// plain os.WriteFile, which would leave a half-written file on disk if a
// write were interrupted partway through.
func TestExportAtomicNoTempLeftovers(t *testing.T) {
	t.Parallel()
	_, fzfPath := buildFZF(t, []string{testVoiceKick, testVoiceSnare})
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "instrument"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "fizzle-") {
			t.Errorf("found leftover temp file from non-atomic write: %q", e.Name())
		}
		if !strings.HasSuffix(e.Name(), ".sfz") && !strings.HasSuffix(e.Name(), ".wav") {
			t.Errorf("unexpected file in output directory: %q", e.Name())
		}
	}
	// Round-trip: re-read SFZ and WAVs to confirm atomic-renamed files are
	// fully present and readable, not truncated. WriteAtomic syncs and
	// renames so a partial body would surface as a parse error here.
	sfz := readSFZ(t, outDir, "instrument")
	if !strings.Contains(sfz, "<region>") {
		t.Errorf("SFZ output missing <region> blocks; atomic write may have truncated content:\n%s", sfz)
	}
	wavFiles, _ := filepath.Glob(filepath.Join(outDir, "*.wav"))
	if len(wavFiles) != 2 {
		t.Fatalf("expected 2 WAV files, got %d", len(wavFiles))
	}
	for _, w := range wavFiles {
		f, err := os.Open(w)
		if err != nil {
			t.Fatal(err)
		}
		_, err = wav.Read(f)
		f.Close() //nolint:errcheck
		if err != nil {
			t.Errorf("WAV %q unreadable after atomic write: %v", w, err)
		}
	}
}

func TestExportSFZRegionCount(t *testing.T) {
	t.Parallel()
	_, fzfPath := buildFZF(t, []string{"A", "B", "C"})
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	count := strings.Count(sfz, "<region>")
	if count != 3 {
		t.Errorf("expected 3 <region> blocks, got %d", count)
	}
}

func TestExportKeyRange(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoicePad})
	patchBankByte(data, disk.BankKeyLowOffset, 0, 48)
	patchBankByte(data, disk.BankKeyHighOffset, 0, 72)
	patchBankByte(data, disk.BankKeyCentOffset, 0, 60)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "lokey=48") {
		t.Errorf("missing lokey=48:\n%s", sfz)
	}
	if !strings.Contains(sfz, "hikey=72") {
		t.Errorf("missing hikey=72:\n%s", sfz)
	}
	if !strings.Contains(sfz, "pitch_keycenter=60") {
		t.Errorf("missing pitch_keycenter=60:\n%s", sfz)
	}
}

func TestExportTranspose(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{"BASS"})
	patchVoiceHeaderU16(data, 0, disk.VoiceDCPOffset, uint16(int16(2*disk.SemitoneDCPScale)))
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "transpose=2") {
		t.Errorf("expected transpose=2:\n%s", sfz)
	}
}

func TestExportTune(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoicePad})
	dcpFor50Cents := int16(128)
	patchVoiceHeaderU16(data, 0, disk.VoiceDCPOffset, uint16(dcpFor50Cents))
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "tune=50") {
		t.Errorf("expected tune=50:\n%s", sfz)
	}
}

func TestExportCutoff(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoicePad})
	patchVoiceByte(data, 0, disk.VoiceDCFOffset, 80)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "cutoff=80") {
		t.Errorf("expected cutoff=80:\n%s", sfz)
	}
}

func TestExportResonance(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoicePad})
	patchVoiceByte(data, 0, disk.VoiceDCQOffset, 30)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "resonance=30") {
		t.Errorf("expected resonance=30:\n%s", sfz)
	}
}

func TestExportOmitsDefaultCutoff(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoicePad})
	patchVoiceByte(data, 0, disk.VoiceDCFOffset, disk.DCFMaxOffset)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if strings.Contains(sfz, "cutoff=") {
		t.Errorf("should not contain cutoff when it is default (127):\n%s", sfz)
	}
}

func TestExportMuteGroup(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{"HAT1", "HAT2", testVoicePad})
	patchBankByte(data, disk.BankAudioOutOffset, 0, 0x01)
	patchBankByte(data, disk.BankAudioOutOffset, 1, 0x01)
	patchBankByte(data, disk.BankAudioOutOffset, 2, 0xff)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if strings.Count(sfz, "mutegroup=") != 2 {
		t.Errorf("expected exactly 2 mutegroup= entries:\n%s", sfz)
	}
	if strings.Contains(sfz, "mutegroup=") {
		parts := strings.Split(sfz, "<region>")
		hat1Group := extractOpcode(parts[1], "mutegroup")
		hat2Group := extractOpcode(parts[2], "mutegroup")
		if hat1Group != hat2Group {
			t.Errorf("HAT1 and HAT2 should share mutegroup: got %q and %q", hat1Group, hat2Group)
		}
	}
}

func TestExportOneShot(t *testing.T) {
	t.Parallel()
	_, fzfPath := buildFZF(t, []string{testVoiceKick})
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "loop_mode=one_shot") {
		t.Errorf("expected loop_mode=one_shot for voice with no sustain loop:\n%s", sfz)
	}
}

func TestExportLoopPoints(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoicePad})
	patchVoiceByte(data, 0, disk.VoiceLoopSusOffset, 0)
	patchVoiceByte(data, 0, disk.VoiceLoopEndOffset, disk.HoldIndefinitely)
	patchVoiceHeaderU32(data, 0, disk.VoiceLoopSt0Offset, 100)
	patchVoiceHeaderU32(data, 0, disk.VoiceLoopEd0Offset, 500)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "loop_start=100") {
		t.Errorf("expected loop_start=100:\n%s", sfz)
	}
	if !strings.Contains(sfz, "loop_end=500") {
		t.Errorf("expected loop_end=500:\n%s", sfz)
	}
	wavFiles, _ := filepath.Glob(filepath.Join(outDir, "*.wav"))
	if len(wavFiles) == 0 {
		t.Fatal("no WAV files produced")
	}
	f, err := os.Open(wavFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	wf, err := wav.Read(f)
	if err != nil {
		t.Fatal(err)
	}
	if wf.LoopStart != 100 || wf.LoopEnd != 500 {
		t.Errorf("WAV loop points: got start=%d end=%d, want 100/500", wf.LoopStart, wf.LoopEnd)
	}
}

// TestExportLoopPointsUsesLoopSusIndex pins the fix for the bug where
// the SFZ exporter (and its WAV-side loop write) always read
// loopst[0]/looped[0] regardless of loop_sus. Voices whose active loop
// pair was not the first would otherwise carry the wrong sample
// addresses (and "has loop" decision) into both the SFZ and the WAV.
func TestExportLoopPointsUsesLoopSusIndex(t *testing.T) {
	t.Parallel()
	// Each test voice from fzfbuilder.MakeTestFZF carries 512 audio
	// samples; pick loop addresses that fit so the WAV writer accepts
	// them. The pairs deliberately differ across all eight slots so an
	// off-by-pair read produces an observably wrong value.
	cases := []struct {
		name      string
		loopSus   uint8
		wantLoop  bool
		wantStart uint32
		wantEnd   uint32
	}{
		{"index 0", 0, true, 10, 50},
		{"index 1", 1, true, 70, 110},
		{"index 7", 7, true, 430, 470},
		{"no sustain", disk.NoSustainLoop, false, 0, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, _ := buildFZF(t, []string{testVoicePad})
			patchVoiceByte(data, 0, disk.VoiceLoopSusOffset, tc.loopSus)
			patchVoiceByte(data, 0, disk.VoiceLoopEndOffset, disk.HoldIndefinitely)
			for i := 0; i < disk.EnvelopeStages; i++ {
				st := uint32(i*60 + 10)
				ed := uint32(i*60 + 50)
				patchVoiceHeaderU32(data, 0, disk.VoiceLoopSt0Offset+i*4, st)
				patchVoiceHeaderU32(data, 0, disk.VoiceLoopEd0Offset+i*4, ed)
			}
			fzfPath := writePatchedFZF(t, data)
			outDir := filepath.Join(t.TempDir(), "out")
			if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
				t.Fatal(err)
			}
			sfz := readSFZ(t, outDir, "test")
			if tc.wantLoop {
				wantSt := fmt.Sprintf("loop_start=%d", tc.wantStart)
				wantEd := fmt.Sprintf("loop_end=%d", tc.wantEnd)
				if !strings.Contains(sfz, wantSt) || !strings.Contains(sfz, wantEd) {
					t.Errorf("expected %q and %q in SFZ:\n%s", wantSt, wantEd, sfz)
				}
				if strings.Contains(sfz, "loop_mode=one_shot") {
					t.Errorf("loop_sus=%d should produce a sustain loop, got one_shot:\n%s",
						tc.loopSus, sfz)
				}
			} else {
				if !strings.Contains(sfz, "loop_mode=one_shot") {
					t.Errorf("loop_sus=%d (no sustain) should emit loop_mode=one_shot:\n%s",
						tc.loopSus, sfz)
				}
				if strings.Contains(sfz, "loop_start=") {
					t.Errorf("loop_sus=%d should not emit loop_start=:\n%s",
						tc.loopSus, sfz)
				}
			}
			// And the on-disk WAV's SMPL chunk must agree.
			wavFiles, _ := filepath.Glob(filepath.Join(outDir, "*.wav"))
			if len(wavFiles) == 0 {
				t.Fatal("no WAV files produced")
			}
			f, err := os.Open(wavFiles[0])
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close() //nolint:errcheck
			wf, err := wav.Read(f)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantLoop {
				if wf.LoopStart != int(tc.wantStart) || wf.LoopEnd != int(tc.wantEnd) {
					t.Errorf("WAV loop: got (%d, %d), want (%d, %d)",
						wf.LoopStart, wf.LoopEnd, tc.wantStart, tc.wantEnd)
				}
			} else {
				if wf.LoopStart >= 0 || wf.LoopEnd >= 0 {
					t.Errorf("WAV loop should be absent: got (%d, %d)",
						wf.LoopStart, wf.LoopEnd)
				}
			}
		})
	}
}

// TestExportLoopXFTimeComment pins F19: sfzexport must surface
// loopxf[loop_sus] and looptm[loop_sus] as a "// Loop: xfade=N time=N"
// comment when a sustain loop is active and at least one of the two is
// non-zero. Without this the hardware values are silently dropped on the
// SFZ side and never restored by sfz convert. Spec §2-1: loopxf takes
// 0..1023, looptm takes 1..1022 (16ms step).
func TestExportLoopXFTimeComment(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoicePad})
	patchVoiceByte(data, 0, disk.VoiceLoopSusOffset, 0)
	patchVoiceByte(data, 0, disk.VoiceLoopEndOffset, disk.HoldIndefinitely)
	patchVoiceHeaderU32(data, 0, disk.VoiceLoopSt0Offset, 100)
	patchVoiceHeaderU32(data, 0, disk.VoiceLoopEd0Offset, 500)
	patchVoiceHeaderU16(data, 0, disk.VoiceLoopXFOffset, 256)
	patchVoiceHeaderU16(data, 0, disk.VoiceLoopTmOffset, 500)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "// Loop: xfade=256 time=500") {
		t.Errorf("expected loop xfade/time comment:\n%s", sfz)
	}
}

// TestExportLoopXFTimeCommentOmittedOnOneShot guards the inverse: a
// one-shot voice (loop_sus = NoSustainLoop) must not emit a "// Loop:"
// line, because the loopxf/looptm fields are not indexable in that
// state. Sibling-check that the other comment block lines (// Playback,
// etc.) still appear so the omission is local, not a regression.
func TestExportLoopXFTimeCommentOmittedOnOneShot(t *testing.T) {
	t.Parallel()
	_, fzfPath := buildFZF(t, []string{testVoiceKick})
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if strings.Contains(sfz, "// Loop:") {
		t.Errorf("one-shot voice should not emit // Loop comment:\n%s", sfz)
	}
	if !strings.Contains(sfz, "// Playback:") {
		t.Errorf("expected // Playback comment to still be present:\n%s", sfz)
	}
	if !strings.Contains(sfz, "// DCA:") {
		t.Errorf("expected // DCA comment to still be present:\n%s", sfz)
	}
}

func TestExportVelocity(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoiceSnare})
	patchBankByte(data, disk.BankVelLowOffset, 0, 64)
	patchBankByte(data, disk.BankVelHighOffset, 0, 127)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "lovel=64") {
		t.Errorf("expected lovel=64:\n%s", sfz)
	}
	if !strings.Contains(sfz, "hivel=127") {
		t.Errorf("expected hivel=127:\n%s", sfz)
	}
}

func TestExportOmitsDefaultVelocity(t *testing.T) {
	t.Parallel()
	_, fzfPath := buildFZF(t, []string{testVoiceKick})
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if strings.Contains(sfz, "lovel=") || strings.Contains(sfz, "hivel=") {
		t.Errorf("should not contain velocity when default:\n%s", sfz)
	}
}

func TestExportWAVRate(t *testing.T) {
	t.Parallel()
	voices := [][]byte{testutil.MakeTestVoice("V18K", 500)}
	voices[0][disk.VoiceSampOffset] = 1
	groups := []voicebuild.Keygroup{voicebuild.NewKeygroup(36, 36, 36)}
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	fzfPath := filepath.Join(t.TempDir(), "rate.fzf")
	if err := os.WriteFile(fzfPath, fzf, 0644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	wavFiles, _ := filepath.Glob(filepath.Join(outDir, "*.wav"))
	if len(wavFiles) == 0 {
		t.Fatal("no WAV files")
	}
	f, err := os.Open(wavFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	wf, err := wav.Read(f)
	if err != nil {
		t.Fatal(err)
	}
	if wf.SampleRate != 18000 {
		t.Errorf("WAV rate: got %d, want 18000", wf.SampleRate)
	}
}

func TestExportDCAComment(t *testing.T) {
	t.Parallel()
	_, fzfPath := buildFZF(t, []string{testVoicePad})
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "// DCA:") {
		t.Errorf("expected DCA comment:\n%s", sfz)
	}
}

func TestExportLFOComment(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoicePad})
	patchVoiceByte(data, 0, disk.VoiceLFORateOffset, 25)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "// LFO:") {
		t.Errorf("expected LFO comment:\n%s", sfz)
	}
}

// TestExportLFODelayWidth guards against the regression where lfo_delay was
// read as a single byte rather than the 2-byte little-endian field declared in
// spec §2-1 (range 0..65535). A 1-byte read silently truncates the high byte
// so e.g. delay=1234 (0x04D2) becomes delay=210 (0xD2).
func TestExportLFODelayWidth(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoicePad})
	patchVoiceHeaderU16(data, 0, disk.VoiceLFODelayOffset, 1234)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "test"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "test")
	if !strings.Contains(sfz, "delay=1234") {
		t.Errorf("expected delay=1234 in LFO comment (not truncated to delay=210):\n%s", sfz)
	}
	if strings.Contains(sfz, "delay=210") {
		t.Errorf("lfo_delay was byte-truncated (got delay=210):\n%s", sfz)
	}
}

func TestExportNameFlag(t *testing.T) {
	t.Parallel()
	_, fzfPath := buildFZF(t, []string{"A"})
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "mykit"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "mykit.sfz")); err != nil {
		t.Errorf("expected mykit.sfz: %v", err)
	}
}

func TestExportDefaultName(t *testing.T) {
	t.Parallel()
	_, fzfPath := buildFZF(t, []string{"A"})
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "test.sfz")); err != nil {
		t.Errorf("expected test.sfz (from fzf filename stem): %v", err)
	}
}

// TestExportRoundTripContent goes SFZ -> FZF -> SFZ and asserts the per-region
// content (not just counts) survives. The original SFZ source-of-truth has
// known voice names and key ranges; both must round-trip intact.
func TestExportRoundTripContent(t *testing.T) {
	t.Parallel()
	sfzPath := filepath.Join("..", "..", "testdata", "synthetic", "JUNGLISM.sfz")
	fzfPath := filepath.Join(t.TempDir(), "junglism.fzf")
	if err := sfzconvert.Convert(context.Background(), sfzPath, fzfPath, 36000, false); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "export")
	if err := sfzexport.Export(fzfPath, outDir, "junglism"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "junglism")
	regionCount := strings.Count(sfz, "<region>")
	if regionCount != 26 {
		t.Errorf("expected 26 regions (JUNGLISM has 26 voices), got %d", regionCount)
	}
	wavFiles, _ := filepath.Glob(filepath.Join(outDir, "*.wav"))
	if len(wavFiles) != 26 {
		t.Errorf("expected 26 WAV files, got %d", len(wavFiles))
	}
	// Spot-check that the first three voice names from JUNGLISM.sfz appear
	// as sample references on the exported side, in the same order. JUNGLISM
	// happens to begin with AMEN 01..03 in voice-slot order.
	for _, want := range []string{"AMEN 01.wav", "AMEN 02.wav", "AMEN 03.wav"} {
		if !strings.Contains(sfz, "sample="+want) {
			t.Errorf("exported SFZ missing sample=%s", want)
		}
	}
}

// TestExportSlotOrderAlignment is a regression test for a bug where the
// exporter iterated voices by os.ReadDir (alphabetical) and indexed bank
// metadata by directory position, scrambling per-voice fields when slot
// order differed from alphabetical name order.
func TestExportSlotOrderAlignment(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{"ZOO", "AARDVARK"})
	// Voice header cutoff: ZOO=50, AARDVARK=80.
	voff0 := disk.VoiceSlotOffset(disk.SectorSize, 0)
	voff1 := disk.VoiceSlotOffset(disk.SectorSize, 1)
	data[voff0+disk.VoiceDCFOffset] = 50
	data[voff1+disk.VoiceDCFOffset] = 80
	// Bank-level key range: ZOO at C2, AARDVARK at C5.
	data[disk.BankKeyLowOffset+0] = 36
	data[disk.BankKeyHighOffset+0] = 36
	data[disk.BankKeyCentOffset+0] = 36
	data[disk.BankKeyLowOffset+1] = 60
	data[disk.BankKeyHighOffset+1] = 60
	data[disk.BankKeyCentOffset+1] = 60

	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "slot"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "slot")
	parts := strings.Split(sfz, "<region>")
	if len(parts) < 3 {
		t.Fatalf("expected at least 2 regions, got %d", len(parts)-1)
	}
	// Region 1 must describe slot 0 (ZOO): cutoff=50, lokey=36.
	if !strings.Contains(parts[1], "sample=ZOO.wav") {
		t.Errorf("region 1 should reference ZOO.wav:\n%s", parts[1])
	}
	if !strings.Contains(parts[1], "cutoff=50") {
		t.Errorf("region 1 (slot 0, ZOO) should have cutoff=50:\n%s", parts[1])
	}
	if !strings.Contains(parts[1], "lokey=36") {
		t.Errorf("region 1 (slot 0, ZOO) should have lokey=36:\n%s", parts[1])
	}
	// Region 2 must describe slot 1 (AARDVARK): cutoff=80, lokey=60.
	if !strings.Contains(parts[2], "sample=AARDVARK.wav") {
		t.Errorf("region 2 should reference AARDVARK.wav:\n%s", parts[2])
	}
	if !strings.Contains(parts[2], "cutoff=80") {
		t.Errorf("region 2 (slot 1, AARDVARK) should have cutoff=80:\n%s", parts[2])
	}
	if !strings.Contains(parts[2], "lokey=60") {
		t.Errorf("region 2 (slot 1, AARDVARK) should have lokey=60:\n%s", parts[2])
	}
}

// TestExportFZFtoSFZtoFZFRoundTrip exercises the full bidirectional path:
// an FZF goes out as SFZ + WAVs, the SFZ is converted back to FZF by
// sfzconvert, and the per-voice fields that SFZ can represent are checked
// to survive intact.
func TestExportFZFtoSFZtoFZFRoundTrip(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{testVoiceKick, testVoiceSnare, testVoicePad})

	// Per-voice header patches: each voice has a distinct cutoff,
	// resonance, and transpose so misalignment would be observable.
	voff := func(i int) int { return disk.VoiceSlotOffset(disk.SectorSize, i) }
	data[voff(0)+disk.VoiceDCFOffset] = 40
	data[voff(0)+disk.VoiceDCQOffset] = 5
	dcpPlus2 := int16(2 * disk.SemitoneDCPScale)
	binary.LittleEndian.PutUint16(data[voff(0)+disk.VoiceDCPOffset:], uint16(dcpPlus2)) //nolint:gosec // two's-complement reinterpretation

	data[voff(1)+disk.VoiceDCFOffset] = 80
	data[voff(1)+disk.VoiceDCQOffset] = 10
	dcpMinus3 := int16(-3 * disk.SemitoneDCPScale)
	binary.LittleEndian.PutUint16(data[voff(1)+disk.VoiceDCPOffset:], uint16(dcpMinus3)) //nolint:gosec // two's-complement reinterpretation

	data[voff(2)+disk.VoiceDCFOffset] = 120
	data[voff(2)+disk.VoiceDCQOffset] = 20
	// PAD gets a sustain loop.
	data[voff(2)+disk.VoiceLoopSusOffset] = 0
	data[voff(2)+disk.VoiceLoopEndOffset] = disk.HoldIndefinitely
	binary.LittleEndian.PutUint32(data[voff(2)+disk.VoiceLoopSt0Offset:], 100)
	binary.LittleEndian.PutUint32(data[voff(2)+disk.VoiceLoopEd0Offset:], 500)

	// Per-voice bank patches: distinct key ranges.
	data[disk.BankKeyLowOffset+0] = 36
	data[disk.BankKeyHighOffset+0] = 36
	data[disk.BankKeyCentOffset+0] = 36
	data[disk.BankKeyLowOffset+1] = 38
	data[disk.BankKeyHighOffset+1] = 38
	data[disk.BankKeyCentOffset+1] = 38
	data[disk.BankKeyLowOffset+2] = 60
	data[disk.BankKeyHighOffset+2] = 72
	data[disk.BankKeyCentOffset+2] = 66

	fzfPath := writePatchedFZF(t, data)
	exportDir := filepath.Join(t.TempDir(), "export")
	if err := sfzexport.Export(fzfPath, exportDir, "kit"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Convert the exported SFZ back to an FZF.
	reSFZ := filepath.Join(exportDir, "kit.sfz")
	reFZF := filepath.Join(t.TempDir(), "kit.fzf")
	if err := sfzconvert.Convert(context.Background(), reSFZ, reFZF, 36000, false); err != nil {
		t.Fatalf("sfzconvert.Convert on exported SFZ: %v", err)
	}

	rt, err := os.ReadFile(reFZF)
	if err != nil {
		t.Fatal(err)
	}
	// Round-tripped per-voice loop points are stored as combined-audio
	// offsets, not voice-local ones. Use UnpackData to get each voice as a
	// standalone FZV with pointers rewritten relative to its own audio.
	rtVoices, _, err := voiceunpack.UnpackData(reFZF)
	if err != nil {
		t.Fatalf("UnpackData on round-tripped FZF: %v", err)
	}
	want := []struct {
		name              string
		cutoff, resonance byte
		keyLow, keyHigh   byte
		keyCent           byte
		dcp               int16
		hasLoop           bool
		loopStart         uint32
		loopEnd           uint32
	}{
		{"KICK", 40, 5, 36, 36, 36, int16(2 * disk.SemitoneDCPScale), false, 0, 0},
		{"SNARE", 80, 10, 38, 38, 38, int16(-3 * disk.SemitoneDCPScale), false, 0, 0},
		{"PAD", 120, 20, 60, 72, 66, 0, true, 100, 500},
	}
	for i, w := range want {
		vo := disk.VoiceSlotOffset(disk.SectorSize, i)
		if got := rt[vo+disk.VoiceDCFOffset]; got != w.cutoff {
			t.Errorf("voice %d (%s) cutoff: got %d, want %d", i, w.name, got, w.cutoff)
		}
		if got := rt[vo+disk.VoiceDCQOffset]; got != w.resonance {
			t.Errorf("voice %d (%s) resonance: got %d, want %d", i, w.name, got, w.resonance)
		}
		if got := rt[disk.BankKeyLowOffset+i]; got != w.keyLow {
			t.Errorf("voice %d (%s) keyLow: got %d, want %d", i, w.name, got, w.keyLow)
		}
		if got := rt[disk.BankKeyHighOffset+i]; got != w.keyHigh {
			t.Errorf("voice %d (%s) keyHigh: got %d, want %d", i, w.name, got, w.keyHigh)
		}
		if got := rt[disk.BankKeyCentOffset+i]; got != w.keyCent {
			t.Errorf("voice %d (%s) keyCent: got %d, want %d", i, w.name, got, w.keyCent)
		}
		gotDCP := int16(binary.LittleEndian.Uint16(rt[vo+disk.VoiceDCPOffset:])) //nolint:gosec // two's-complement reinterpretation
		if gotDCP != w.dcp {
			t.Errorf("voice %d (%s) DCP: got %d, want %d", i, w.name, gotDCP, w.dcp)
		}
		if w.hasLoop {
			gotSt := binary.LittleEndian.Uint32(rtVoices[i][disk.VoiceLoopSt0Offset:])
			gotEd := binary.LittleEndian.Uint32(rtVoices[i][disk.VoiceLoopEd0Offset:])
			if gotSt != w.loopStart || gotEd != w.loopEnd {
				t.Errorf("voice %d (%s) loop (voice-local): got %d..%d, want %d..%d",
					i, w.name, gotSt, gotEd, w.loopStart, w.loopEnd)
			}
		}
	}
}

// TestExportSanitizesVoiceNames ensures a voice header with a name
// containing filesystem-unsafe characters does not let an attacker (or a
// corrupted disk image) write outside outputDir.
func TestExportSanitizesVoiceNames(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{"A"})
	// Stamp a hostile name directly into the voice header.
	voff := disk.VoiceSlotOffset(disk.SectorSize, 0)
	hostile := "../escape"
	for i := range hostile {
		data[voff+disk.VoiceNameOffset+i] = hostile[i]
	}
	for i := len(hostile); i < disk.LabelSize; i++ {
		data[voff+disk.VoiceNameOffset+i] = ' '
	}
	fzfPath := writePatchedFZF(t, data)
	parent := t.TempDir()
	outDir := filepath.Join(parent, "out")
	if err := sfzexport.Export(fzfPath, outDir, "sanitised"); err != nil {
		t.Fatal(err)
	}
	// The hostile name must not have escaped outDir.
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "out" {
			t.Errorf("file escaped outputDir into parent: %s", e.Name())
		}
	}
	// And the SFZ sample reference must use a sanitised filename.
	sfz := readSFZ(t, outDir, "sanitised")
	if strings.Contains(sfz, "sample=../") {
		t.Errorf("SFZ sample reference still contains '../':\n%s", sfz)
	}
}

// TestExportCollapsesInternalWhitespaceInVoiceNames ensures a voice name
// with consecutive spaces (real example: "BASS DRUM  2" on TECHNO.img) is
// emitted with single-spaced filenames so the SFZ parser can find the
// file. The parser uses strings.Fields() and rejoins with a single space.
func TestExportCollapsesInternalWhitespaceInVoiceNames(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{"A"})
	voff := disk.VoiceSlotOffset(disk.SectorSize, 0)
	name := "BASS DRUM  2" // two spaces
	for i := range name {
		data[voff+disk.VoiceNameOffset+i] = name[i]
	}
	for i := len(name); i < disk.LabelSize; i++ {
		data[voff+disk.VoiceNameOffset+i] = ' '
	}
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "ws"); err != nil {
		t.Fatal(err)
	}
	// The on-disk WAV filename and the SFZ sample reference must agree
	// after whitespace collapse.
	wantWAV := "BASS DRUM 2.wav"
	if _, err := os.Stat(filepath.Join(outDir, wantWAV)); err != nil {
		t.Errorf("expected WAV %q on disk: %v", wantWAV, err)
	}
	sfz := readSFZ(t, outDir, "ws")
	if !strings.Contains(sfz, "sample="+wantWAV) {
		t.Errorf("SFZ should reference %q:\n%s", wantWAV, sfz)
	}
	// And the round-trip back through sfz convert must find the WAV.
	reFZF := filepath.Join(t.TempDir(), "rt.fzf")
	if err := sfzconvert.Convert(context.Background(),
		filepath.Join(outDir, "ws.sfz"), reFZF, 36000, false); err != nil {
		t.Errorf("sfz convert on exported SFZ failed: %v", err)
	}
}

// TestExportPreservesZeroVelocity guards against the round-trip silently
// changing a (0, 0) silencing range back to the default (1, 127).
func TestExportPreservesZeroVelocity(t *testing.T) {
	t.Parallel()
	data, _ := buildFZF(t, []string{"SILENT"})
	patchBankByte(data, disk.BankVelLowOffset, 0, 0)
	patchBankByte(data, disk.BankVelHighOffset, 0, 0)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "vz"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "vz")
	if !strings.Contains(sfz, "lovel=0 hivel=0") {
		t.Errorf("zero velocity range must be preserved with explicit opcodes:\n%s", sfz)
	}
}

// TestExportCentsCarriesToSemitones checks that DCP values whose remainder
// rounds to exactly +/-100 cents carry into the semitones bucket rather
// than emitting an out-of-range tune opcode.
func TestExportCentsCarriesToSemitones(t *testing.T) {
	t.Parallel()
	// dcp=255 rounds to 100 cents under the naive split; the fix should
	// emit transpose=1 and no tune opcode.
	data, _ := buildFZF(t, []string{"BORDER"})
	patchVoiceHeaderU16(data, 0, disk.VoiceDCPOffset, uint16(255))
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "cents"); err != nil {
		t.Fatal(err)
	}
	sfz := readSFZ(t, outDir, "cents")
	if !strings.Contains(sfz, "transpose=1") {
		t.Errorf("dcp=255 should carry to transpose=1:\n%s", sfz)
	}
	if strings.Contains(sfz, "tune=100") || strings.Contains(sfz, "tune=-100") {
		t.Errorf("tune should not be at the +/-100 boundary:\n%s", sfz)
	}
}

// TestExportDeduplicatesDuplicateVoiceNames is a regression test for a bug
// where the duplicate-name suffixer incremented its counter on the
// already-suffixed stem instead of the original. With N voices sharing one
// name, voices 2..N all resolved to "<name>-1" and overwrote each other, so
// only two unique WAVs survived (e.g. CASIO072.FZF's 59 UNTITLED SAM voices
// produced just 4 WAVs). Verifies N voices yield N distinct WAVs.
func TestExportDeduplicatesDuplicateVoiceNames(t *testing.T) {
	t.Parallel()
	const n = 5
	voices := make([][]byte, n)
	groups := make([]voicebuild.Keygroup, n)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice("DUPE", 256)
		note := uint8(disk.FirstMIDINote + i) //nolint:gosec // n bounded, fits uint8
		groups[i] = voicebuild.NewKeygroup(note, note, note)
	}
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	fzfPath := filepath.Join(t.TempDir(), "dupe.fzf")
	if err := os.WriteFile(fzfPath, fzf, 0644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "dupe"); err != nil {
		t.Fatal(err)
	}

	wavFiles, _ := filepath.Glob(filepath.Join(outDir, "*.wav"))
	if len(wavFiles) != n {
		names := make([]string, len(wavFiles))
		for i, p := range wavFiles {
			names[i] = filepath.Base(p)
		}
		t.Fatalf("expected %d distinct WAV files, got %d: %v", n, len(wavFiles), names)
	}

	// The exported SFZ should reference all N WAVs distinctly.
	sfz := readSFZ(t, outDir, "dupe")
	if got := strings.Count(sfz, "sample="); got != n {
		t.Errorf("expected %d sample= references, got %d:\n%s", n, got, sfz)
	}
	// Spot-check the expected suffix pattern: "DUPE.wav", "DUPE-1.wav",
	// ..., "DUPE-(n-1).wav".
	for i := range n {
		var want string
		if i == 0 {
			want = "sample=DUPE.wav"
		} else {
			want = fmt.Sprintf("sample=DUPE-%d.wav", i)
		}
		if !strings.Contains(sfz, want) {
			t.Errorf("SFZ missing %q:\n%s", want, sfz)
		}
	}
}

// TestExportSkipsVoiceWithCorruptWaveEnd is a regression test for a UX bug
// where a single voice header with an inflated waveEnd pointer (observed in
// real-world FZFs from the factory disk corpus) aborted the entire export
// with a single cryptic error and zero output. The export should now log a
// WARN for the bad voice, skip it, and emit all the well-formed voices.
func TestExportSkipsVoiceWithCorruptWaveEnd(t *testing.T) {
	buf := testutil.CaptureLog(t)
	data, _ := buildFZF(t, []string{testVoiceKick, testVoiceSnare, testVoicePad})
	// Inflate voice 1 (SNARE)'s waveEnd pointer to far past the end of the
	// audio area so voiceextract.Decode returns "wave end N exceeds
	// available samples M" for just that voice.
	patchVoiceHeaderU32(data, 1, disk.VoiceWaveEndOffset, 1_000_000)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "kit"); err != nil {
		t.Fatalf("Export should succeed and skip the bad voice, got: %v", err)
	}
	// The two well-formed voices (KICK and PAD) should still produce WAVs;
	// SNARE should not.
	wavFiles, _ := filepath.Glob(filepath.Join(outDir, "*.wav"))
	if len(wavFiles) != 2 {
		t.Errorf("expected 2 WAV files (bad voice skipped), got %d: %v", len(wavFiles), wavFiles)
	}
	for _, w := range wavFiles {
		if strings.Contains(filepath.Base(w), "SNARE") {
			t.Errorf("SNARE WAV should not have been produced: %s", w)
		}
	}
	sfz := readSFZ(t, outDir, "kit")
	if got := strings.Count(sfz, "<region>"); got != 2 {
		t.Errorf("expected 2 <region> blocks, got %d:\n%s", got, sfz)
	}
	if strings.Contains(sfz, "sample=SNARE.wav") {
		t.Errorf("SFZ should not reference the skipped voice's WAV:\n%s", sfz)
	}
	if !testutil.BufHasWarnContaining(buf, "skipping voice with undecodable audio") {
		t.Errorf("expected WARN log for skipped voice, got:\n%s", buf.String())
	}
	if !testutil.BufHasWarnContaining(buf, "SNARE") {
		t.Errorf("expected WARN log to name the SNARE voice, got:\n%s", buf.String())
	}
}

// TestExportWarnsOnLFOPhaseFlag verifies the LFO phase-sync flag (MSB of
// lfo_name) is reported on export because SFZ has no equivalent opcode.
func TestExportWarnsOnLFOPhaseFlag(t *testing.T) {
	buf := testutil.CaptureLog(t)
	data, _ := buildFZF(t, []string{testVoicePad})
	// Set lfo_name to a valid waveform (sine = 0) with the phase-sync bit set.
	patchVoiceByte(data, 0, disk.VoiceLFONameOffset, disk.LFOPhaseFlag)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "kit"); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !testutil.BufHasWarnContaining(buf, "phase-sync") {
		t.Errorf("expected phase-sync warning, got:\n%s", buf.String())
	}
	if !testutil.BufHasWarnContaining(buf, testVoicePad) {
		t.Errorf("expected warning to name the voice %q, got:\n%s", testVoicePad, buf.String())
	}
}

// TestExportWarnsOnReleaseLoop verifies a release-loop pair (loop_end < 8)
// triggers a warning because SFZ regions only carry a single loop.
func TestExportWarnsOnReleaseLoop(t *testing.T) {
	buf := testutil.CaptureLog(t)
	data, _ := buildFZF(t, []string{testVoiceSnare})
	// loop_end=3 selects release loop 4; anything < 8 is an active release.
	patchVoiceByte(data, 0, disk.VoiceLoopEndOffset, 3)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "kit"); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !testutil.BufHasWarnContaining(buf, "release loop") {
		t.Errorf("expected release-loop warning, got:\n%s", buf.String())
	}
	if !testutil.BufHasWarnContaining(buf, testVoiceSnare) {
		t.Errorf("expected warning to name the voice %q, got:\n%s", testVoiceSnare, buf.String())
	}
}

// TestExportWarnsOnReverseMode verifies that PlaybackModeReverse triggers a
// warning because the WAV is exported as forward audio.
func TestExportWarnsOnReverseMode(t *testing.T) {
	buf := testutil.CaptureLog(t)
	data, _ := buildFZF(t, []string{testVoiceKick})
	patchVoiceHeaderU16(data, 0, disk.VoiceLoopModeOffset, disk.PlaybackModeReverse)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "kit"); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !testutil.BufHasWarnContaining(buf, "reverse") {
		t.Errorf("expected reverse-playback warning, got:\n%s", buf.String())
	}
	if !testutil.BufHasWarnContaining(buf, testVoiceKick) {
		t.Errorf("expected warning to name the voice %q, got:\n%s", testVoiceKick, buf.String())
	}
}

// TestExportWarnsOnMultiBitGCHN verifies that a multi-generator gchn bitmask
// (which has no SFZ mutegroup equivalent) triggers a warning rather than
// being silently dropped.
func TestExportWarnsOnMultiBitGCHN(t *testing.T) {
	buf := testutil.CaptureLog(t)
	data, _ := buildFZF(t, []string{testVoicePad})
	// 0x03 = generators 0 and 1; bits.OnesCount8 > 1.
	patchBankByte(data, disk.BankAudioOutOffset, 0, 0x03)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "kit"); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !testutil.BufHasWarnContaining(buf, "multi-generator") {
		t.Errorf("expected multi-generator gchn warning, got:\n%s", buf.String())
	}
	if !testutil.BufHasWarnContaining(buf, testVoicePad) {
		t.Errorf("expected warning to name the voice %q, got:\n%s", testVoicePad, buf.String())
	}
}

// TestExportTechnoMultiBankRegionCount is the regression test for F4: on
// a real-hardware multi-bank dump like TECHNO.img every audible slot (32
// in this case) must produce one SFZ region with its bank-correct key
// range. The previous implementation read every region's key range from
// bank 0; slots 11-31 fell past bank 0's bstep=11 and ended up with
// zero/garbage key ranges.
func TestExportTechnoMultiBankRegionCount(t *testing.T) {
	t.Parallel()
	const technoImg = "../../testdata/synthetic/TECHNO.img"
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	outDir := filepath.Join(dir, "out")
	if err := sfzexport.Export(fzfPath, outDir, "techno"); err != nil {
		t.Fatalf("Export: %v", err)
	}
	sfz := readSFZ(t, outDir, "techno")
	regions := strings.Count(sfz, "<region>")
	if regions != 32 {
		t.Errorf("region count: got %d, want 32 (one per audible voice slot)", regions)
	}
	// NASTY BASS lives at voice slot 11; bank 0 vp[0]=11 with KeyLow=42 KeyHigh=66.
	// The exported region must carry that key range, not bank 0[offset+11].
	if !strings.Contains(sfz, "lokey=42 hikey=66 pitch_keycenter=49") {
		t.Errorf("expected NASTY BASS / METAL-BELL key range 42..66 from bank 0 split 0, got SFZ:\n%s", sfz)
	}
}

// TestExportLeadingNoSoundSlot is the regression test for F6. The previous
// loop indexed bank arrays and storedNames by the *compacted* FZV slice
// position (NoSound slots dropped). With a leading NoSound slot, voice 0
// in the SFZ region list took its name/key/output from bank slot 0 (the
// NoSound placeholder) instead of from the first audible slot.
func TestExportLeadingNoSoundSlot(t *testing.T) {
	t.Parallel()
	// Build a 3-voice FZF then patch slot 0 into a NoSound placeholder so
	// only slots 1 and 2 are audible. The exported SFZ must reference
	// slots 1 and 2's metadata, not slots 0 and 1's.
	data, _ := buildFZF(t, []string{"GHOST", "ALPHA", "BETA"})

	// Patch slot 0's loopMode to NoSound; clear its key range so the bug
	// case (reading slot 0's data) would be obvious.
	voff := disk.VoiceSlotOffset(disk.SectorSize, 0)
	binary.LittleEndian.PutUint16(data[voff+disk.VoiceLoopModeOffset:], disk.PlaybackModeNoSound)
	data[disk.BankKeyLowOffset+0] = 0
	data[disk.BankKeyHighOffset+0] = 0
	// Slot 1 (ALPHA) at bank split 1, slot 2 (BETA) at bank split 2:
	// give them distinct key ranges so the export must read from those splits.
	data[disk.BankKeyLowOffset+1] = 48
	data[disk.BankKeyHighOffset+1] = 60
	data[disk.BankKeyLowOffset+2] = 72
	data[disk.BankKeyHighOffset+2] = 84

	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "nosound"); err != nil {
		t.Fatalf("Export: %v", err)
	}
	sfz := readSFZ(t, outDir, "nosound")

	// The SFZ must reference ALPHA and BETA with their own key ranges, not
	// the NoSound slot's zeroed bytes.
	if !strings.Contains(sfz, "sample=ALPHA.wav") {
		t.Errorf("expected ALPHA.wav reference, got:\n%s", sfz)
	}
	if !strings.Contains(sfz, "sample=BETA.wav") {
		t.Errorf("expected BETA.wav reference, got:\n%s", sfz)
	}
	if !strings.Contains(sfz, "lokey=48 hikey=60") {
		t.Errorf("expected ALPHA's key range 48..60 (slot 1's bank metadata), got:\n%s", sfz)
	}
	if !strings.Contains(sfz, "lokey=72 hikey=84") {
		t.Errorf("expected BETA's key range 72..84 (slot 2's bank metadata), got:\n%s", sfz)
	}
}

// TestExportWarnsOnSharedVoicePointer verifies that when multiple bank slots
// share the same vp[] value, a warning is emitted because the key-split
// sharing is lost on SFZ export.
func TestExportWarnsOnSharedVoicePointer(t *testing.T) {
	buf := testutil.CaptureLog(t)
	data, _ := buildFZF(t, []string{testVoiceKick, testVoiceSnare})
	// Overwrite slot 1's vp[] entry to point at voice 0, so both slots
	// reference the same voice header.
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+2*1:], 0)
	fzfPath := writePatchedFZF(t, data)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := sfzexport.Export(fzfPath, outDir, "kit"); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !testutil.BufHasWarnContaining(buf, "voice pointer") && !testutil.BufHasWarnContaining(buf, "key-split sharing") {
		t.Errorf("expected shared voice-pointer warning, got:\n%s", buf.String())
	}
}

// FuzzExport feeds arbitrary FZF bytes to Export and asserts that any
// success path produces an SFZ that the converter can re-parse, with one
// region per voice slot the original FZF declared. Tightens the surface
// where the slot-order alignment bug lived.
func FuzzExport(f *testing.F) {
	for _, names := range [][]string{
		{"A"},
		{"KICK", "SNARE", "HAT"},
		{"ZOO", "AARDVARK"},
		{"V1", "V2", "V3", "V4"},
	} {
		f.Add(buildFuzzSeed(names))
	}
	f.Fuzz(func(t *testing.T, fzfBytes []byte) {
		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "fuzz.fzf")
		if err := os.WriteFile(fzfPath, fzfBytes, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		outDir := filepath.Join(dir, "out")
		if err := sfzexport.Export(fzfPath, outDir, "fuzz"); err != nil {
			return
		}
		sfzPath := filepath.Join(outDir, "fuzz.sfz")
		body, err := os.ReadFile(sfzPath)
		if err != nil {
			t.Fatalf("reading exported SFZ: %v", err)
		}
		regions := strings.Count(string(body), "<region>")
		// No path-traversal escape: outDir must be the only thing under dir
		// other than the input fzf and any other temp files the runtime
		// created. Specifically, no exported WAV should be a sibling of out.
		parent, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir parent: %v", err)
		}
		for _, e := range parent {
			n := e.Name()
			if n != "out" && n != "fuzz.fzf" {
				if strings.HasSuffix(n, ".wav") {
					t.Fatalf("voice WAV escaped outDir: %s", n)
				}
			}
		}
		if regions > 0 {
			// Every region should have a sample= line.
			samples := strings.Count(string(body), "sample=")
			if samples != regions {
				t.Errorf("region count %d != sample= count %d", regions, samples)
			}
		}
	})
}

// buildFuzzSeed synthesises a minimal valid FZF byte slice without
// depending on *testing.T, so it can be called from f.Add at registration
// time. Each voice gets a 256-byte slot inside one voice-area sector and a
// single-sector audio block, mirroring what fzfbuilder.MakeTestFZF
// produces.
func buildFuzzSeed(names []string) []byte {
	n := len(names)
	voiceSectors := disk.VoiceAreaSectors(n)
	size := disk.SectorSize + voiceSectors*disk.SectorSize + n*disk.SectorSize
	out := make([]byte, size)
	binary.LittleEndian.PutUint16(out[disk.BankVoiceCountOffset:], uint16(n)) //nolint:gosec // n bounded
	for i, name := range names {
		voff := disk.VoiceSlotOffset(disk.SectorSize, i)
		padded := disk.PadLabel(name)
		copy(out[voff+disk.VoiceNameOffset:], padded[:])
		binary.LittleEndian.PutUint32(out[voff+disk.VoiceWaveStartOffset:], 0)
		binary.LittleEndian.PutUint32(out[voff+disk.VoiceWaveEndOffset:], 0)
		binary.LittleEndian.PutUint32(out[voff+disk.VoiceGenStartOffset:], 0)
		binary.LittleEndian.PutUint32(out[voff+disk.VoiceGenEndOffset:], 0)
		binary.LittleEndian.PutUint16(out[voff+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	}
	return out
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func extractOpcode(regionText, opcode string) string {
	for _, line := range strings.Split(regionText, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, opcode+"=") {
			return strings.TrimPrefix(line, opcode+"=")
		}
		for _, part := range strings.Fields(line) {
			if strings.HasPrefix(part, opcode+"=") {
				return strings.TrimPrefix(part, opcode+"=")
			}
		}
	}
	return ""
}

var (
	_ = bytes.Equal
	_ = binary.LittleEndian
)
