package fzutil

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// buildWAV writes a minimal 16-bit mono PCM WAV to a temp file and returns its path.
func buildWAV(t *testing.T, samples []int16, rate uint32) string {
	t.Helper()
	var buf bytes.Buffer
	f := &wav.File{SampleRate: rate, Samples: samples}
	if err := wav.Write(&buf, f); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "test.wav")
	if err := os.WriteFile(p, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadWAV(t *testing.T) {
	t.Parallel()
	want := []int16{100, 200, -100, -200}
	p := buildWAV(t, want, 44100)
	f, err := ReadWAV(p)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if f.SampleRate != 44100 {
		t.Errorf("SampleRate: got %d, want 44100", f.SampleRate)
	}
	if len(f.Samples) != len(want) {
		t.Fatalf("sample count: got %d, want %d", len(f.Samples), len(want))
	}
	for i, s := range f.Samples {
		if s != want[i] {
			t.Errorf("sample %d: got %d, want %d", i, s, want[i])
		}
	}
}

func TestReadWAVMissingFile(t *testing.T) {
	t.Parallel()
	_, err := ReadWAV("/nonexistent/file.wav")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestResampleSameRate(t *testing.T) {
	t.Parallel()
	f := &wav.File{SampleRate: 36000, Samples: []int16{100, 200, 300}}
	out, err := Resample(f, 36000)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range f.Samples {
		if out[i] != s {
			t.Errorf("sample %d: got %d, want %d", i, out[i], s)
		}
	}
}

func TestResampleHalvesLength(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 3600)
	f := &wav.File{SampleRate: 36000, Samples: samples}
	out, err := Resample(f, 18000)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1800 {
		t.Errorf("length: got %d, want 1800", len(out))
	}
}

func TestResampleEmptySamplesErrors(t *testing.T) {
	t.Parallel()
	f := &wav.File{SampleRate: 36000, Samples: nil}
	_, err := Resample(f, 9000)
	if err == nil {
		t.Error("expected error for empty samples")
	}
}

func TestResampleZeroRateErrors(t *testing.T) {
	t.Parallel()
	f := &wav.File{SampleRate: 0, Samples: []int16{1, 2}}
	_, err := Resample(f, 9000)
	if err == nil {
		t.Error("expected error for zero sample rate")
	}
}

func TestResampleRejectsPathologicallyLowSampleRate(t *testing.T) {
	t.Parallel()
	// A crafted WAV with SampleRate=1 would compute an enormous output
	// length when resampled to a normal target rate. The guard must reject
	// the input before the multi-terabyte allocation is attempted.
	f := &wav.File{SampleRate: 1, Samples: make([]int16, 16)}
	_, err := Resample(f, DefaultRate)
	if err == nil {
		t.Fatal("expected error for SampleRate below minimum")
	}
	if !strings.Contains(err.Error(), "below minimum") {
		t.Errorf("error should mention minimum sample rate, got: %v", err)
	}
}

func TestResampleRejectsOversizedOutput(t *testing.T) {
	t.Parallel()
	// Force the resample math to exceed MaxResampleOut without tripping the
	// MinSampleRate guard: upsample heavily from MinSampleRate to a much
	// higher rate with enough input samples that the projected output
	// crosses the FZ-1 RAM cap.
	src := make([]int16, MaxResampleOut)
	f := &wav.File{SampleRate: MinSampleRate, Samples: src}
	_, err := Resample(f, MinSampleRate*2)
	if err == nil {
		t.Fatal("expected error for resampled length over cap")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("error should mention maximum, got: %v", err)
	}
}

func TestResampleClampsSaturation(t *testing.T) {
	t.Parallel()
	samples := []int16{32767, -32768, 32767, -32768}
	f := &wav.File{SampleRate: 44100, Samples: samples}
	out, err := Resample(f, 36000)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range out {
		v := int32(s)
		if v < -32768 || v > 32767 {
			t.Errorf("sample %d out of int16 range: %d", i, v)
		}
	}
}

func TestResampleSingleSample(t *testing.T) {
	t.Parallel()
	// A single sample should always produce at least 1 output sample,
	// even when extreme downsampling would round outLen to 0.
	f := &wav.File{SampleRate: 44100, Samples: []int16{1000}}
	out, err := Resample(f, 9000)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 1 {
		t.Error("expected at least 1 output sample for single-sample input")
	}
}

func TestResamplePreservesSign(t *testing.T) {
	t.Parallel()
	// Negative samples should remain negative after resampling.
	samples := make([]int16, 3600)
	for i := range samples {
		samples[i] = -1000
	}
	f := &wav.File{SampleRate: 36000, Samples: samples}
	out, err := Resample(f, 18000)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range out {
		if s >= 0 {
			t.Errorf("sample %d: expected negative, got %d", i, s)
			break
		}
	}
}

func TestReadFZVMissingFile(t *testing.T) {
	t.Parallel()
	_, err := ReadFZV("/nonexistent/file.fzv")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadFZVTooSmall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "tiny.fzv")
	if err := os.WriteFile(p, []byte("too small"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadFZV(p)
	if err == nil {
		t.Error("expected error for file smaller than one sector")
	}
}

func TestReadFZVValid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "voice.fzv")
	data := make([]byte, 2048) // two sectors
	copy(data[disk.VoiceNameOffset:], "TESTVOICE   ")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFZV(p)
	if err != nil {
		t.Fatalf("ReadFZV: %v", err)
	}
	if len(got) != 2048 {
		t.Errorf("len: got %d, want 2048", len(got))
	}
}

func TestCountBankSectorsOne(t *testing.T) {
	t.Parallel()
	// Single bank sector with valid voice count and name.
	data := make([]byte, 2048)
	data[0] = 4 // nvoice=4
	copy(data[0x282:], "All Voices  ")
	// Second sector is garbage, so detection should stop at 1.
	if n := CountBankSectors(data); n != 1 {
		t.Errorf("expected 1 bank sector, got %d", n)
	}
}

func TestCountBankSectorsMultiple(t *testing.T) {
	t.Parallel()
	// Three consecutive valid bank sectors.
	data := make([]byte, 3*1024)
	for i := range 3 {
		off := i * 1024
		data[off] = 4 // nvoice=4
		copy(data[off+0x282:], "Bank Name   ")
	}
	if n := CountBankSectors(data); n != 3 {
		t.Errorf("expected 3 bank sectors, got %d", n)
	}
}

func TestCountBankSectorsStopsAtInvalidVoiceCount(t *testing.T) {
	t.Parallel()
	data := make([]byte, 2*1024)
	data[0] = 4
	copy(data[0x282:], "Valid Bank  ")
	// Second sector: voice count = 0 (invalid)
	data[1024] = 0
	if n := CountBankSectors(data); n != 1 {
		t.Errorf("expected 1, got %d", n)
	}
}

func TestCountBankSectorsMax8(t *testing.T) {
	t.Parallel()
	// 10 sectors all looking valid; should cap at 8.
	data := make([]byte, 10*1024)
	for i := range 10 {
		off := i * 1024
		data[off] = 4
		copy(data[off+0x282:], "Bank Name   ")
	}
	if n := CountBankSectors(data); n != 8 {
		t.Errorf("expected max 8 bank sectors, got %d", n)
	}
}

func TestVoiceName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{"/samples/kick.wav", "KICK"},
		{"/samples/HOOVER.wav", "HOOVER"},
		{"amen 01.wav", "AMEN 01"},
		{"SPEED GRG BASS 01.wav", "SPEED GRG BA"}, // truncated to 12
		{"strange--name.wav", "STRANGE NAME"},
		{"123.wav", "123"},
		{".wav", "VOICE"}, // empty stem falls back to VOICE
		{"noext", "NOEXT"},
	}
	for _, tt := range tests {
		if got := VoiceName(tt.path); got != tt.want {
			t.Errorf("VoiceName(%q): got %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestVoiceNameTruncatesTo12(t *testing.T) {
	t.Parallel()
	got := VoiceName("a very long filename that exceeds twelve chars.wav")
	if len(got) > 12 {
		t.Errorf("VoiceName should truncate to 12 chars, got %d: %q", len(got), got)
	}
}

func TestReadBoundedExceedsMaxSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(p, make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadBounded(p, 50)
	if err == nil {
		t.Error("expected error for file exceeding maxSize")
	}
}

func TestVoiceNameNonASCII(t *testing.T) {
	t.Parallel()
	got := VoiceName("café.wav")
	if got != "CAF" {
		t.Errorf("VoiceName(\"café.wav\"): got %q, want %q", got, "CAF")
	}
	for i := range len(got) {
		if got[i] > 127 {
			t.Errorf("non-ASCII byte at index %d: 0x%02x", i, got[i])
		}
	}
}

func TestParseFZFHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name: "valid header",
			data: func() []byte {
				d := make([]byte, disk.SectorSize*2)
				binary.LittleEndian.PutUint16(d[0:2], 1)
				copy(d[disk.BankNameOffset:], "Valid Bank   ")
				// Voice area starts at the second sector; place one plausible
				// voice header so InferVoiceCount returns 1.
				vOff := disk.VoiceSlotOffset(disk.SectorSize, 0)
				binary.LittleEndian.PutUint16(d[vOff+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
				return d
			}(),
			wantErr: false,
		},
		{
			name:    "data too small",
			data:    make([]byte, 500),
			wantErr: true,
		},
		{
			name: "zero voice count",
			data: func() []byte {
				d := make([]byte, disk.SectorSize)
				binary.LittleEndian.PutUint16(d[0:2], 0)
				return d
			}(),
			wantErr: true,
		},
		{
			name: "voice count too large",
			data: func() []byte {
				d := make([]byte, disk.SectorSize)
				binary.LittleEndian.PutUint16(d[0:2], 65)
				return d
			}(),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, err := ParseFZFHeader(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h.NVoice != 1 {
				t.Errorf("NVoice = %d, want 1", h.NVoice)
			}
		})
	}
}

// TestIsMultiDiskFirstHalf pins the shared helper studio uses to
// gate destructive operations against multi-disk dumps. A standalone
// single-disk FZF must return false; a synthesised disk-1-like
// payload (BankTotalWaveOffset claiming more audio than is present,
// plus a plausible voice with wavst past local audio) must return
// true.
func TestIsMultiDiskFirstHalf(t *testing.T) {
	t.Parallel()

	t.Run("single-disk returns false", func(t *testing.T) {
		t.Parallel()
		// One bank, three plausible voices, audio area large enough
		// for the claimed wavst, BankTotalWaveOffset zero.
		const bstep = 3
		voiceAreaSectors := disk.VoiceAreaSectors(bstep)
		data := make([]byte, disk.SectorSize+voiceAreaSectors*disk.SectorSize+4*disk.SectorSize)
		binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], uint16(bstep))
		copy(data[disk.BankNameOffset:], "OnceDisk    ")
		for i := 0; i < bstep; i++ {
			off := disk.VoiceSlotOffset(disk.SectorSize, i)
			binary.LittleEndian.PutUint16(data[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		}
		if IsMultiDiskFirstHalf(data) {
			t.Errorf("standalone FZF flagged as multi-disk first half")
		}
	})

	t.Run("disk-1-like returns true", func(t *testing.T) {
		t.Parallel()
		const bstep = 1
		voiceAreaSectors := disk.VoiceAreaSectors(bstep)
		// Local audio: 2 sectors.
		localAudioSectors := 2
		data := make([]byte, disk.SectorSize+voiceAreaSectors*disk.SectorSize+localAudioSectors*disk.SectorSize)
		binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], uint16(bstep))
		copy(data[disk.BankNameOffset:], "Disk1       ")
		// Claim 100 wave sectors total.
		binary.LittleEndian.PutUint32(data[disk.BankTotalWaveOffset:], 100)
		// Slot 0: plausible voice with name, valid playback mode, and
		// wavst pointing way past local audio. IsPlausibleVoiceSlot
		// also requires waved >= wavst and envelope-stage fields in
		// range.
		off := disk.VoiceSlotOffset(disk.SectorSize, 0)
		binary.LittleEndian.PutUint16(data[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		copy(data[off+disk.VoiceNameOffset:], "Voice1      ")
		// wavst in samples = 1024 sectors past local audio.
		const wavstSamples = 524288
		binary.LittleEndian.PutUint32(data[off+disk.VoiceWaveStartOffset:], wavstSamples)
		binary.LittleEndian.PutUint32(data[off+disk.VoiceWaveEndOffset:], wavstSamples+1024)
		data[off+disk.VoiceSampOffset] = 0
		// Envelope stages: any value < EnvelopeStages (=8).
		data[off+disk.VoiceDCASusOffset] = 0
		data[off+disk.VoiceDCAEndOffset] = 0
		data[off+disk.VoiceDCFSusOffset] = 0
		data[off+disk.VoiceDCFEndOffset] = 0
		if !IsMultiDiskFirstHalf(data) {
			t.Errorf("synthetic disk-1 payload not flagged as multi-disk first half")
		}
	})
}

// TestParseFZFHeaderBStepLargerThanVoices is a regression test for the
// bstep/vn conflation bug. Real-world Drums-style FZFs (e.g. CASIO005,
// CASIO019, several factory drum kits) declare a bank-level bstep that
// counts key splits, not voices, and is larger than the file-level voice
// count. Slots past index vn are audio bytes, not voice headers. The
// parser must stop at the first non-plausible slot rather than trusting
// bstep.
func TestParseFZFHeaderBStepLargerThanVoices(t *testing.T) {
	t.Parallel()
	// Three real voices, but bstep declares 8 (as if the bank had 8 key
	// splits). Slots 3..7 are audio garbage.
	const realVoices = 3
	const bstep = 8

	// Voice area is sized by the parser using min(bstep, ...). We need
	// enough bytes for 8 slot positions plus some audio bytes after.
	voiceAreaSectors := (bstep + 3) / 4
	data := make([]byte, disk.SectorSize+voiceAreaSectors*disk.SectorSize+1024)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], uint16(bstep))
	copy(data[disk.BankNameOffset:], "Drums Mock   ")

	// Write 3 plausible voice headers.
	voiceArea := disk.SectorSize
	for i := range realVoices {
		off := disk.VoiceSlotOffset(voiceArea, i)
		binary.LittleEndian.PutUint16(data[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	}
	// Fill the rest of the voice area with non-zero garbage so the loop
	// mode bytes for slots 3..7 are not coincidentally a valid mode.
	for j := disk.VoiceSlotOffset(voiceArea, realVoices); j < disk.SectorSize+voiceAreaSectors*disk.SectorSize; j++ {
		data[j] = 0xAB
	}

	h, err := ParseFZFHeader(data)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}
	if h.NVoice != realVoices {
		t.Errorf("NVoice = %d, want %d (bstep=%d, parser must stop at first non-plausible slot)",
			h.NVoice, realVoices, bstep)
	}
	if h.BStep0 != bstep {
		t.Errorf("BStep0 = %d, want %d (raw bstep should be preserved)", h.BStep0, bstep)
	}
}

// TestInferVoiceCountSpansNoSoundSlots is a regression test for CASIO139.FZF
// and similar files where the head of the voice area carries legitimate
// PlaybackModeNoSound placeholder slots before the real voices begin.
// Inference must continue past NoSound slots while still stopping at byte
// patterns that are neither active nor NoSound.
func TestInferVoiceCountSpansNoSoundSlots(t *testing.T) {
	t.Parallel()
	const bstep = 17
	const noSoundSlots = 4
	const activeSlots = 13
	const total = noSoundSlots + activeSlots

	voiceAreaSectors := (bstep + 3) / 4
	data := make([]byte, disk.SectorSize+voiceAreaSectors*disk.SectorSize+1024)
	voiceArea := disk.SectorSize

	// Slots 0..3: PlaybackModeNoSound (the loop mode bytes default to 0,
	// which is NoSound). Add garbage in the wave pointer fields to mimic
	// CASIO139's real-world bytes; IsActiveOrEmptyVoiceSlot should still
	// accept these because mode == NoSound short-circuits.
	for i := range noSoundSlots {
		off := disk.VoiceSlotOffset(voiceArea, i)
		binary.LittleEndian.PutUint32(data[off+disk.VoiceWaveStartOffset:], 0xDEADBEEF)
		binary.LittleEndian.PutUint32(data[off+disk.VoiceWaveEndOffset:], 0xCAFEBABE)
	}
	// Slots 4..16: plausible Normal voices.
	for i := noSoundSlots; i < total; i++ {
		off := disk.VoiceSlotOffset(voiceArea, i)
		binary.LittleEndian.PutUint16(data[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	}

	got := InferVoiceCount(data, voiceArea, bstep)
	if got != total {
		t.Errorf("InferVoiceCount = %d, want %d (must span NoSound slots before real voices)", got, total)
	}
}

func TestCountBankSectors(t *testing.T) {
	t.Parallel()
	t.Run("single bank", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, disk.SectorSize)
		data[0] = 4
		copy(data[disk.BankNameOffset:], "Bank Name   ")
		if n := CountBankSectors(data); n != 1 {
			t.Errorf("expected 1, got %d", n)
		}
	})
	t.Run("data shorter than 2 sectors", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, disk.SectorSize+100)
		data[0] = 4
		copy(data[disk.BankNameOffset:], "Bank Name   ")
		if n := CountBankSectors(data); n != 1 {
			t.Errorf("expected 1, got %d", n)
		}
	})
}

const (
	testNameKick  = "KICK"
	testNameSnare = "SNARE"
	testNameHihat = "HIHAT"
	testNameBass  = "BASS"
)

// makeFZFWithNames assembles a minimal in-memory FZF byte slice with one bank
// sector and nvoice voice headers carrying the supplied names. It is used by
// tests for ExtractStoredNames and ResolveVoiceTargets so the fzutil package
// does not depend on the higher-level fzfbuilder helper (which would form an
// import cycle through voicebuild).
func makeFZFWithNames(t *testing.T, names []string) ([]byte, *FZFHeader) {
	t.Helper()
	nvoice := len(names)
	data := make([]byte, disk.SectorSize+nvoice*disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], uint16(nvoice)) //nolint:gosec
	copy(data[disk.BankNameOffset:], "Bank Name   ")
	voiceArea := disk.SectorSize
	for i, name := range names {
		voff := disk.VoiceSlotOffset(voiceArea, i)
		padded := disk.PadLabel(name)
		copy(data[voff+disk.VoiceNameOffset:], padded[:])
	}
	hdr, err := ParseFZFHeader(data)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}
	return data, hdr
}

func TestExtractStoredNames(t *testing.T) {
	t.Parallel()
	want := []string{testNameKick, testNameSnare, testNameHihat}
	data, hdr := makeFZFWithNames(t, want)
	got := ExtractStoredNames(data, hdr)
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("name %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveVoiceTargetsAll(t *testing.T) {
	t.Parallel()
	data, hdr := makeFZFWithNames(t, []string{"A", "B", "C"})
	targets, stored, err := ResolveVoiceTargets(data, hdr, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stored) != 3 {
		t.Errorf("stored: got %d, want 3", len(stored))
	}
	want := []int{0, 1, 2}
	if len(targets) != len(want) {
		t.Fatalf("targets: got %d, want %d", len(targets), len(want))
	}
	for i, v := range want {
		if targets[i] != v {
			t.Errorf("target %d: got %d, want %d", i, targets[i], v)
		}
	}
}

func TestResolveVoiceTargetsByName(t *testing.T) {
	t.Parallel()
	data, hdr := makeFZFWithNames(t, []string{testNameKick, testNameSnare, testNameHihat})
	targets, _, err := ResolveVoiceTargets(data, hdr, []string{"snare", testNameHihat}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []int{1, 2}
	if len(targets) != len(want) {
		t.Fatalf("targets: got %v, want %v", targets, want)
	}
	for i, v := range want {
		if targets[i] != v {
			t.Errorf("target %d: got %d, want %d", i, targets[i], v)
		}
	}
}

func TestResolveVoiceTargetsByNameDuplicates(t *testing.T) {
	t.Parallel()
	// Two voices share the same name; both indices must be returned for
	// a single matching selector.
	data, hdr := makeFZFWithNames(t, []string{testNameBass, testNameBass, "PAD"})
	targets, _, err := ResolveVoiceTargets(data, hdr, []string{testNameBass}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []int{0, 1}
	if len(targets) != len(want) {
		t.Fatalf("targets: got %v, want %v", targets, want)
	}
	for i, v := range want {
		if targets[i] != v {
			t.Errorf("target %d: got %d, want %d", i, targets[i], v)
		}
	}
}

func TestResolveVoiceTargetsNotFound(t *testing.T) {
	t.Parallel()
	data, hdr := makeFZFWithNames(t, []string{testNameKick, testNameSnare})
	_, _, err := ResolveVoiceTargets(data, hdr, []string{"GHOST"}, false)
	if err == nil {
		t.Fatal("expected error for unknown voice name")
	}
	msg := err.Error()
	if !strings.Contains(msg, "fzutil:") {
		t.Errorf("error missing fzutil prefix: %q", msg)
	}
	if !strings.Contains(msg, "\"GHOST\"") {
		t.Errorf("error should mention requested name: %q", msg)
	}
	// Available names should be listed in sorted order.
	if !strings.Contains(msg, testNameKick+", "+testNameSnare) {
		t.Errorf("error should list available voices in sorted order: %q", msg)
	}
}

// makeMultiBankFZF assembles a synthetic multi-bank FZF for BankSite testing.
// Each entry in bankPlans is a list of voice-slot indices that bank's vp[]
// should map (in key-split order). The voice area is sized for the highest
// slot referenced, and every referenced slot gets a plausible voice header
// (PlaybackModeNormal + a printable name) so InferVoiceCount accepts it.
func makeMultiBankFZF(t *testing.T, bankPlans [][]int) []byte {
	t.Helper()
	nBanks := len(bankPlans)
	if nBanks == 0 || nBanks > disk.MaxBanks {
		t.Fatalf("makeMultiBankFZF: nBanks=%d out of range", nBanks)
	}
	// Find the largest slot index referenced anywhere; voice area must
	// cover slot 0..maxSlot inclusive.
	maxSlot := -1
	for _, plan := range bankPlans {
		for _, s := range plan {
			if s > maxSlot {
				maxSlot = s
			}
		}
	}
	if maxSlot < 0 {
		t.Fatal("makeMultiBankFZF: no slots referenced")
	}
	nvoice := maxSlot + 1
	voiceSectors := disk.VoiceAreaSectors(nvoice)
	size := nBanks*disk.SectorSize + voiceSectors*disk.SectorSize
	data := make([]byte, size)
	for b, plan := range bankPlans {
		off := b * disk.SectorSize
		bstep := len(plan)
		binary.LittleEndian.PutUint16(data[off+disk.BankVoiceCountOffset:], uint16(bstep)) //nolint:gosec // G115: test constant
		bankName := disk.PadLabel(fmt.Sprintf("BANK%d", b))
		copy(data[off+disk.BankNameOffset:], bankName[:])
		for s, slot := range plan {
			binary.LittleEndian.PutUint16(data[off+disk.BankVoiceNumOffset+2*s:], uint16(slot)) //nolint:gosec // G115: test constant
			// Stamp distinct key range bytes so callers can verify they
			// read from the right (bank, split) site.
			data[off+disk.BankKeyLowOffset+s] = uint8(36 + b*10 + s)  //nolint:gosec // G115: test constant
			data[off+disk.BankKeyHighOffset+s] = uint8(72 + b*10 + s) //nolint:gosec // G115: test constant
			data[off+disk.BankMIDIRecvChanOffset+s] = uint8(b)        //nolint:gosec // G115: test constant; mchn is stored 0-indexed, ParseBankVoiceEntry adds 1
			data[off+disk.BankAudioOutOffset+s] = uint8(1 << b)       //nolint:gosec // G115: test constant
		}
	}
	voiceAreaStart := nBanks * disk.SectorSize
	for slot := 0; slot < nvoice; slot++ {
		voff := disk.VoiceSlotOffset(voiceAreaStart, slot)
		vName := disk.PadLabel(fmt.Sprintf("V%02d", slot+1))
		copy(data[voff+disk.VoiceNameOffset:], vName[:])
		binary.LittleEndian.PutUint16(data[voff+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	}
	return data
}

func TestFindBankSitesForVoiceSingleBank(t *testing.T) {
	t.Parallel()
	// Single bank, identity vp[]=i: the voicebuild-produced case.
	data := makeMultiBankFZF(t, [][]int{{0, 1, 2}})
	hdr, err := ParseFZFHeader(data)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}
	for slot := 0; slot < 3; slot++ {
		sites := FindBankSitesForVoice(data, hdr, slot)
		if len(sites) != 1 || sites[0].BankIdx != 0 || sites[0].SplitIdx != slot {
			t.Errorf("slot %d sites: got %v, want [{0 %d}]", slot, sites, slot)
		}
	}
	// Voice slot beyond the voice area has no sites.
	if got := FindBankSitesForVoice(data, hdr, 5); len(got) != 0 {
		t.Errorf("orphan slot 5: got %v, want []", got)
	}
}

func TestFindBankSitesForVoiceMultiBank(t *testing.T) {
	t.Parallel()
	// Bank 0 owns slots 0,1; bank 1 references slot 0 (shared) and slot 2;
	// bank 2 owns slot 3; slot 4 is orphaned.
	data := makeMultiBankFZF(t, [][]int{
		{0, 1},
		{0, 2},
		{3},
		// Slot 4 is reachable in the voice area (maxSlot=3 -> wait, we need
		// slot 4 referenced somewhere or it won't be in the voice area).
	})
	// Force the voice area to span slot 4 by writing one extra slot manually.
	data = append(data, make([]byte, disk.SectorSize)...)
	// Re-stamp slot 4 as a plausible voice. NOTE: VoicesPerSector=4 means
	// slots 0-3 share one sector; slot 4 lives in the next sector. Append
	// added that sector, so the existing voice area now covers slots 0-7.
	voiceAreaStart := 3 * disk.SectorSize
	voff := disk.VoiceSlotOffset(voiceAreaStart, 4)
	v05Name := disk.PadLabel("V05")
	copy(data[voff+disk.VoiceNameOffset:], v05Name[:])
	binary.LittleEndian.PutUint16(data[voff+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	hdr, err := ParseFZFHeader(data)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}

	tests := []struct {
		slot int
		want []BankSite
	}{
		{0, []BankSite{{BankIdx: 0, SplitIdx: 0}, {BankIdx: 1, SplitIdx: 0}}},
		{1, []BankSite{{BankIdx: 0, SplitIdx: 1}}},
		{2, []BankSite{{BankIdx: 1, SplitIdx: 1}}},
		{3, []BankSite{{BankIdx: 2, SplitIdx: 0}}},
		{4, nil},
	}
	for _, tt := range tests {
		got := FindBankSitesForVoice(data, hdr, tt.slot)
		if len(got) != len(tt.want) {
			t.Errorf("slot %d: got %v, want %v", tt.slot, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("slot %d site %d: got %v, want %v", tt.slot, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParseBankVoiceEntryUsesSplitIdx(t *testing.T) {
	t.Parallel()
	// Two banks each map slot 5 to different splits with different metadata.
	data := makeMultiBankFZF(t, [][]int{
		{0, 5}, // bank 0 split 1 references slot 5
		{5},    // bank 1 split 0 references slot 5
	})
	voiceArea := data[2*disk.SectorSize:]

	// Bank 0 split 1: KeyLow = 36 + 0*10 + 1 = 37, MIDIChan = 0+1 = 1.
	bank0 := data[:disk.SectorSize]
	e, ok := ParseBankVoiceEntry(bank0, voiceArea, 1, 5)
	if !ok {
		t.Fatal("ParseBankVoiceEntry returned !ok for bank 0 split 1")
	}
	if e.KeyLow != 37 {
		t.Errorf("bank 0 split 1 KeyLow: got %d, want 37", e.KeyLow)
	}
	if e.MIDIChannel != 1 {
		t.Errorf("bank 0 split 1 MIDIChannel: got %d, want 1", e.MIDIChannel)
	}

	// Bank 1 split 0: KeyLow = 36 + 1*10 + 0 = 46, MIDIChan = 1+1 = 2.
	bank1 := data[disk.SectorSize : 2*disk.SectorSize]
	e, ok = ParseBankVoiceEntry(bank1, voiceArea, 0, 5)
	if !ok {
		t.Fatal("ParseBankVoiceEntry returned !ok for bank 1 split 0")
	}
	if e.KeyLow != 46 {
		t.Errorf("bank 1 split 0 KeyLow: got %d, want 46", e.KeyLow)
	}
	if e.MIDIChannel != 2 {
		t.Errorf("bank 1 split 0 MIDIChannel: got %d, want 2", e.MIDIChannel)
	}
}
