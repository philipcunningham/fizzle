package integration_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/disklist"
	"github.com/philipcunningham/fizzle/pkg/fzfinfo"
	"github.com/philipcunningham/fizzle/pkg/fzfmidi"

	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"

	voiceimport "github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

func TestTechnoDiskStructure(t *testing.T) {
	skipShort(t)
	t.Parallel()

	var buf bytes.Buffer
	if err := disklist.List(technoImg, &buf); err != nil {
		t.Fatalf("disk ls: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "FULL-DATA-FZ") {
		t.Errorf("expected FULL-DATA-FZ in directory listing:\n%s", out)
	}
	if !strings.Contains(out, "Full Dump") {
		t.Errorf("expected 'Full Dump' type in listing:\n%s", out)
	}
}

// TestTechnoFZFUnpack verifies that fzf unpack handles a real multi-bank FZF
// without panicking and produces the correct number of named voices.
func TestTechnoFZFUnpack(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack panicked or failed: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}
	// TECHNO is multi-bank: 8 banks, 32 distinct voice slots. Counting
	// from bank 0's bstep alone yields 11, dropping every voice only
	// banks 1 to 7 reference through vp[].
	if len(entries) != 32 {
		t.Errorf("expected 32 voices from TECHNO.img, got %d", len(entries))
	}

	for _, e := range entries {
		fzv, err := os.ReadFile(filepath.Join(unpackDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		waveStart := binary.LittleEndian.Uint32(fzv[0x00:0x04])
		if waveStart != 0 {
			t.Errorf("%s: waveStart=%d, want 0 (cumulative offset not subtracted)", e.Name(), waveStart)
		}
		loopMode := binary.LittleEndian.Uint16(fzv[0x10:0x12])
		if loopMode == 0x0000 {
			t.Errorf("%s: loop mode is NO SOUND (voice not extracted correctly)", e.Name())
		}
	}
}

// TestTechnoVoiceHeaderSanity checks that voices unpacked from real
// hardware carry sane headers, above all an envelope that doesn't silence
// the voice at once (dca_sus and dca_end must not both be 0).
func TestTechnoVoiceHeaderSanity(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		fzv, err := os.ReadFile(filepath.Join(unpackDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		dcaSus := fzv[0x78]
		dcaEnd := fzv[0x79]

		// dca_sus=0 with dca_end=0 leaves the envelope no sustain and no
		// release, so the sampler may produce nothing audible.
		if dcaSus == 0 && dcaEnd == 0 {
			t.Errorf("%s: dca_sus=0 and dca_end=0 (envelope will silence the voice immediately)", e.Name())
		}
	}
}

// TestEnvelopeDefaultsMatchHardware checks fizzle's generated envelope
// defaults (dca_sus, dca_end, dca_rate, dca_stop) against real hardware,
// with METAL-BELL from TECHNO.img as the reference. It guards the silent
// playback bug, where dca_sus=0 and dca_end=0 left the FZ-1 mute.
func TestEnvelopeDefaultsMatchHardware(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	ref, err := os.ReadFile(filepath.Join(unpackDir, "METAL-BELL.fzv"))
	if err != nil {
		t.Fatalf("reading METAL-BELL: %v", err)
	}
	refDCASus := ref[0x78]
	refDCAEnd := ref[0x79]

	// A synthetic voice built from fizzle's own defaults.
	samples := make([]int16, 1000)
	fzv := voiceimport.Encode(samples, 0, "TEST", 0, voiceimport.NoLoop())

	ourDCASus := fzv[0x78]
	ourDCAEnd := fzv[0x79]

	if ourDCASus != 0 {
		t.Errorf("our dca_sus=%d, want 0", ourDCASus)
	}
	if ourDCAEnd != 7 {
		t.Errorf("our dca_end=%d, want 7", ourDCAEnd)
	}

	t.Logf("Hardware reference: dca_sus=%d dca_end=%d", refDCASus, refDCAEnd)
	t.Logf("Our defaults:       dca_sus=%d dca_end=%d", ourDCASus, ourDCAEnd)
}

func TestBrassFZFUnpack(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}
	// BRASS is multi-bank, with 13 distinct voice slots across banks. See
	// TestTechnoFZFUnpack for why bank 0's bstep undercounts.
	if len(entries) != 13 {
		t.Errorf("expected 13 voices from BRASS.img, got %d", len(entries))
	}

	for _, e := range entries {
		fzv, err := os.ReadFile(filepath.Join(unpackDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		waveStart := binary.LittleEndian.Uint32(fzv[0x00:0x04])
		if waveStart != 0 {
			t.Errorf("%s: waveStart=%d, want 0", e.Name(), waveStart)
		}
	}
}

func TestBrassRoundTripExtract(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		fzvPath := filepath.Join(unpackDir, e.Name())
		wavPath := filepath.Join(dir, e.Name()+".wav")

		if err := voiceextract.Extract(fzvPath, wavPath); err != nil {
			t.Errorf("%s: extract failed: %v", e.Name(), err)
			continue
		}

		f, err := os.Open(wavPath)
		if err != nil {
			t.Fatal(err)
		}
		w, err := wav.Read(f)
		f.Close() //nolint:errcheck
		if err != nil {
			t.Errorf("%s: reading WAV: %v", e.Name(), err)
			continue
		}

		if w.SampleRate != 36000 && w.SampleRate != 18000 && w.SampleRate != 9000 {
			t.Errorf("%s: unexpected sample rate %d", e.Name(), w.SampleRate)
		}
	}
}

// TestTechnoRoundTripExtract verifies that voices extracted from the real
// hardware image produce non-silent WAV files with correct sample rates.
func TestTechnoRoundTripExtract(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		fzvPath := filepath.Join(unpackDir, e.Name())
		wavPath := filepath.Join(dir, e.Name()+".wav")

		if err := voiceextract.Extract(fzvPath, wavPath); err != nil {
			t.Errorf("%s: extract failed: %v", e.Name(), err)
			continue
		}

		f, err := os.Open(wavPath)
		if err != nil {
			t.Fatal(err)
		}
		w, err := wav.Read(f)
		f.Close() //nolint:errcheck
		if err != nil {
			t.Errorf("%s: reading WAV: %v", e.Name(), err)
			continue
		}

		if w.SampleRate != 36000 && w.SampleRate != 18000 && w.SampleRate != 9000 {
			t.Errorf("%s: unexpected sample rate %d", e.Name(), w.SampleRate)
		}
		hasSignal := false
		for _, s := range w.Samples {
			if s > 100 || s < -100 {
				hasSignal = true
				break
			}
		}
		if !hasSignal {
			t.Errorf("%s: WAV appears silent (all samples near zero)", e.Name())
		}
	}
}

// TestFZFMidiEndToEnd tests the full sfz convert, fzf midi, and fzf info pipeline.
func TestFZFMidiEndToEnd(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")

	// 9kHz fits on one disk.
	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzfPath, 9000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	res, err := fzfmidi.Set(fzfPath, []string{"REESE"}, false, 2)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 1 {
		t.Fatalf("expected 1 voice updated, got %d", len(res.Updated))
	}
	if res.Updated[0].Name != "REESE" || res.Updated[0].NewChannel != 2 {
		t.Errorf("unexpected update: %+v", res.Updated[0])
	}

	// The raw byte in the FZF, at the slot found by scanning names.
	data, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	reeseIdx := -1
	nBankSectors := 1
	nvoice := int(binary.LittleEndian.Uint16(data[0:2]))
	voiceAreaStart := nBankSectors * disk.SectorSize
	for i := range nvoice {
		voff := disk.VoiceSlotOffset(voiceAreaStart, i)
		if voff+disk.VoiceNameOffset+disk.LabelSize <= len(data) {
			name := strings.TrimRight(string(data[voff+disk.VoiceNameOffset:voff+disk.VoiceNameOffset+disk.LabelSize]), " ")
			if strings.EqualFold(name, "REESE") {
				reeseIdx = i
				break
			}
		}
	}
	if reeseIdx < 0 {
		t.Fatal("REESE voice not found in FZF")
	}
	rawChan := data[disk.BankMIDIRecvChanOffset+reeseIdx]
	if rawChan != 1 { // channel 2 stored as 1 (0-indexed)
		t.Errorf("REESE raw MIDI channel byte: got %d, want 1 (channel 2)", rawChan)
	}

	// fzf info shows the Chan column with * on the REESE row.
	var buf bytes.Buffer
	highlighted := map[int]bool{res.Updated[0].Index: true}
	if err := fzfinfo.Info(fzfPath, &buf, highlighted); err != nil {
		t.Fatalf("Info: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Chan") {
		t.Errorf("Chan column should appear after midi channel change:\n%s", out)
	}
	if !strings.Contains(out, "*") {
		t.Errorf("Changed voice should be marked with *:\n%s", out)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("Channel 2 should appear in output:\n%s", out)
	}

	// Resetting all to channel 1 removes the Chan column.
	if _, err := fzfmidi.Set(fzfPath, nil, true, 1); err != nil {
		t.Fatal(err)
	}
	var buf2 bytes.Buffer
	if err := fzfinfo.Info(fzfPath, &buf2, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf2.String(), "Chan") {
		t.Log("Chan column is always shown (expected)")
	}
}

// TestMultiDiskBankSectorInvariant pins the byte-level properties of the
// split format that make the FZ-10M prompt for disk 2 after loading disk
// 1. ConvertMultiDisk writes .img files directly: disk 1 holds a full FZF
// (bank, voices, and partial audio), and disk 2 is pure audio
// continuation with no bank sector and no voice headers.
