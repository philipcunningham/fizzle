package voiceextract

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

func makeTestFZV(rateIdx uint8, sampleCount uint32) []byte {
	data := make([]byte, disk.SectorSize+int(sampleCount)*2)
	binary.LittleEndian.PutUint32(data[disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[disk.VoiceWaveEndOffset:], sampleCount)
	data[disk.VoiceSampOffset] = rateIdx
	for i := uint32(0); i < sampleCount; i++ {
		binary.LittleEndian.PutUint16(data[disk.SectorSize+int(i)*2:], uint16(i%32768))
	}
	return data
}

func TestDecodeRateMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		idx  uint8
		want uint32
	}{
		{0, 36000},
		{1, 18000},
		{2, 9000},
	}
	for _, tt := range tests {
		data := makeTestFZV(tt.idx, 100)
		rate, _, err := Decode(data)
		if err != nil {
			t.Errorf("rate index %d: %v", tt.idx, err)
			continue
		}
		if rate != tt.want {
			t.Errorf("rate index %d: got %d, want %d", tt.idx, rate, tt.want)
		}
	}
}

func TestDecodeInvalidRateIndex(t *testing.T) {
	t.Parallel()
	data := makeTestFZV(3, 100)
	_, _, err := Decode(data)
	if err == nil {
		t.Error("expected error for rate index 3")
	}
}

func TestDecodeWaveEndBeyondData(t *testing.T) {
	t.Parallel()
	data := makeTestFZV(0, 10)
	binary.LittleEndian.PutUint32(data[disk.VoiceWaveEndOffset:], 99999)
	_, _, err := Decode(data)
	if err == nil {
		t.Error("expected error for wave end beyond data")
	}
}

func TestDecodeSampleValues(t *testing.T) {
	t.Parallel()
	data := makeTestFZV(0, 5)
	_, samples, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 5 {
		t.Fatalf("expected 5 samples, got %d", len(samples))
	}
	for i, s := range samples {
		if s != int16(i) {
			t.Errorf("samples[%d] = %d, want %d", i, s, i)
		}
	}
}

func TestDecodeNonZeroWaveStart(t *testing.T) {
	t.Parallel()
	data := makeTestFZV(0, 100)
	binary.LittleEndian.PutUint32(data[disk.VoiceWaveStartOffset:], 1)
	_, _, err := Decode(data)
	if err == nil {
		t.Error("expected error for non-zero wave start")
	}
}

func TestExtractRoundTrip(t *testing.T) {
	t.Parallel()
	samples := []int16{100, -200, 300, -400, 500}
	data := voiceimport.Encode(samples, 0, "ROUND", 0, voiceimport.NoLoop())

	dir := t.TempDir()
	fzvPath := filepath.Join(dir, "round.fzv")
	if err := os.WriteFile(fzvPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	wavPath := filepath.Join(dir, "round.wav")
	if err := Extract(fzvPath, wavPath); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	fh, err := os.Open(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close() //nolint:errcheck

	f, err := wav.Read(fh)
	if err != nil {
		t.Fatalf("wav.Read: %v", err)
	}

	if f.SampleRate != 36000 {
		t.Errorf("SampleRate: got %d, want 36000", f.SampleRate)
	}
	if len(f.Samples) != len(samples) {
		t.Fatalf("sample count: got %d, want %d", len(f.Samples), len(samples))
	}
	for i, want := range samples {
		if f.Samples[i] != want {
			t.Errorf("samples[%d]: got %d, want %d", i, f.Samples[i], want)
		}
	}
}

// TestExtractPreservesRootNote verifies the FZV -> WAV round trip preserves
// the voice's root note (byte 0xB0) even for one-shot voices without a
// sustain loop. Earlier WAV writer behaviour only emitted the SMPL chunk
// when a loop was present, silently dropping the root note for one-shots;
// this test exercises the no-loop path to pin the fix.
func TestExtractPreservesRootNote(t *testing.T) {
	t.Parallel()
	samples := []int16{100, -200, 300, -400, 500}
	data := voiceimport.Encode(samples, 0, "ROOT", 0, voiceimport.NoLoop())
	// Patch the root note (cent) to something distinctive that is neither
	// the writer's hardcoded fallback (60) nor the encoder default (72).
	const wantRoot uint8 = 36
	data[disk.VoiceKeyCentOffset] = wantRoot

	dir := t.TempDir()
	fzvPath := filepath.Join(dir, "root.fzv")
	if err := os.WriteFile(fzvPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	wavPath := filepath.Join(dir, "root.wav")
	if err := Extract(fzvPath, wavPath); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	fh, err := os.Open(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close() //nolint:errcheck

	f, err := wav.Read(fh)
	if err != nil {
		t.Fatalf("wav.Read: %v", err)
	}
	if f.MIDIUnityNote != wantRoot {
		t.Errorf("MIDIUnityNote: got %d, want %d (FZV cent byte should round-trip via SMPL chunk)", f.MIDIUnityNote, wantRoot)
	}
	// One-shot voices must still report no loop after the round-trip.
	if f.LoopStart != -1 || f.LoopEnd != -1 {
		t.Errorf("one-shot voice should not carry loop points, got LoopStart=%d LoopEnd=%d", f.LoopStart, f.LoopEnd)
	}
}

// TestExtractImportPreservesRootForOneShot pins the full FZV -> WAV -> FZV
// round-trip for one-shot voices. The WAV writer used to omit the SMPL
// chunk when no loop was present, so a non-default root key (cent byte at
// 0xB0) was silently lost when the extracted WAV was re-imported. The
// round-trip must now preserve the cent byte for one-shot voices too.
func TestExtractImportPreservesRootForOneShot(t *testing.T) {
	t.Parallel()
	samples := []int16{100, -200, 300, -400, 500, 600, -700, 800}
	src := voiceimport.Encode(samples, 0, "ONESHOT", 0, voiceimport.NoLoop())
	const wantRoot uint8 = 36
	src[disk.VoiceKeyCentOffset] = wantRoot

	dir := t.TempDir()
	fzvPath := filepath.Join(dir, "oneshot.fzv")
	if err := os.WriteFile(fzvPath, src, 0644); err != nil {
		t.Fatal(err)
	}

	wavPath := filepath.Join(dir, "oneshot.wav")
	if err := Extract(fzvPath, wavPath); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	importedFZV := filepath.Join(dir, "oneshot-roundtrip.fzv")
	if err := voiceimport.Import(wavPath, importedFZV, 36000); err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, err := os.ReadFile(importedFZV)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) <= disk.VoiceKeyCentOffset {
		t.Fatalf("imported FZV too small (%d bytes)", len(got))
	}
	if got[disk.VoiceKeyCentOffset] != wantRoot {
		t.Errorf("imported cent byte: got %d, want %d (root note must survive one-shot round-trip)", got[disk.VoiceKeyCentOffset], wantRoot)
	}
}

func TestExtractWithLoopPoints(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 5000)
	for i := range samples {
		samples[i] = int16(i % 1000)
	}
	loop := voiceimport.LoopParams{LoopStart: 1000, LoopEnd: 4000}
	fzv := voiceimport.Encode(samples, 0, "LOOPTEST", 0, loop)

	dir := t.TempDir()
	fzvPath := filepath.Join(dir, "loop.fzv")
	wavPath := filepath.Join(dir, "loop.wav")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatalf("writing FZV: %v", err)
	}
	if err := Extract(fzvPath, wavPath); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	f, err := os.Open(wavPath)
	if err != nil {
		t.Fatalf("opening WAV: %v", err)
	}
	defer f.Close() //nolint:errcheck
	w, err := wav.Read(f)
	if err != nil {
		t.Fatalf("reading WAV: %v", err)
	}
	if w.LoopStart < 0 || w.LoopEnd < 0 {
		t.Errorf("expected loop points in WAV, got LoopStart=%d LoopEnd=%d", w.LoopStart, w.LoopEnd)
	}
	if w.LoopStart != 1000 {
		t.Errorf("LoopStart: got %d, want 1000", w.LoopStart)
	}
	if w.LoopEnd != 4000 {
		t.Errorf("LoopEnd: got %d, want 4000", w.LoopEnd)
	}
}

// TestDecodeLoopPointsUsesLoopSusIndex pins the fix for the bug where
// decodeLoopPoints always read loopst[0]/looped[0] regardless of the
// loop_sus selector at 0x12. The FZ-1 voice header carries eight
// loopst/looped pairs; loop_sus picks which pair drives the WAV SMPL
// chunk's sustain loop. Hard-coding [0] silently exported wrong loop
// points for any voice whose active loop was not the first.
func TestDecodeLoopPointsUsesLoopSusIndex(t *testing.T) {
	t.Parallel()
	const (
		nSamples = 8000
		sampleSt = disk.SectorSize
	)
	cases := []struct {
		name      string
		loopSus   uint8
		wantStart int
		wantEnd   int
	}{
		{"index 0", 0, 100, 200},
		{"index 1", 1, 1100, 1200},
		{"index 7", 7, 7100, 7200},
		{"no sustain", disk.NoSustainLoop, -1, -1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := makeTestFZV(0, nSamples)
			binary.LittleEndian.PutUint16(data[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
			data[disk.VoiceLoopSusOffset] = tc.loopSus
			data[disk.VoiceLoopEndOffset] = disk.HoldIndefinitely
			// Populate all eight loopst/looped pairs with distinct ranges
			// so an off-by-pair read produces an observably wrong value.
			for i := 0; i < disk.EnvelopeStages; i++ {
				st := uint32(i*1000 + 100)
				ed := uint32(i*1000 + 200)
				binary.LittleEndian.PutUint32(data[disk.VoiceLoopSt0Offset+i*4:], st)
				binary.LittleEndian.PutUint32(data[disk.VoiceLoopEd0Offset+i*4:], ed)
			}
			_ = sampleSt
			gotSt, gotEd := decodeLoopPoints(data)
			if gotSt != tc.wantStart || gotEd != tc.wantEnd {
				t.Errorf("decodeLoopPoints(loop_sus=%d): got (%d, %d), want (%d, %d)",
					tc.loopSus, gotSt, gotEd, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

func TestExtractMissingInput(t *testing.T) {
	t.Parallel()
	err := Extract(filepath.Join(t.TempDir(), "nope.fzv"), filepath.Join(t.TempDir(), "out.wav"))
	if err == nil {
		t.Error("expected error for non-existent FZV file")
	}
}

func TestDecodeEmptyAudio(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint32(data[disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[disk.VoiceWaveEndOffset:], 0)
	data[disk.VoiceSampOffset] = 0
	_, _, err := Decode(data)
	if err == nil {
		t.Error("expected error for header-only voice with no audio")
	}
}

func makeTestFZVWithGenRange(sampleCount, genStart, genEnd uint32) []byte {
	data := makeTestFZV(0, sampleCount)
	binary.LittleEndian.PutUint32(data[disk.VoiceGenStartOffset:], genStart)
	binary.LittleEndian.PutUint32(data[disk.VoiceGenEndOffset:], genEnd)
	return data
}

func TestDecodePlaybackRangeFullWave(t *testing.T) {
	t.Parallel()
	data := makeTestFZVWithGenRange(100, 0, 100)
	rate, samples, err := DecodePlaybackRange(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 36000 {
		t.Errorf("rate: got %d, want 36000", rate)
	}
	if len(samples) != 100 {
		t.Errorf("sample count: got %d, want 100", len(samples))
	}
}

func TestDecodePlaybackRangeSubset(t *testing.T) {
	t.Parallel()
	data := makeTestFZVWithGenRange(200, 50, 150)
	_, samples, err := DecodePlaybackRange(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 100 {
		t.Errorf("sample count: got %d, want 100 (genEnd 150 minus genStart 50)", len(samples))
	}
	if samples[0] != int16(50%32768) {
		t.Errorf("first sample: got %d, want %d (sample at genStart=50)", samples[0], 50%32768)
	}
}

func TestDecodePlaybackRangeWithLeadIn(t *testing.T) {
	t.Parallel()
	data := makeTestFZVWithGenRange(100, 0, 100)
	leadIn := 500
	_, samples, err := DecodePlaybackRange(data, leadIn)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 600 {
		t.Errorf("sample count: got %d, want 600 (500 lead-in + 100 audio)", len(samples))
	}
	for i := range leadIn {
		if samples[i] != 0 {
			t.Errorf("lead-in sample[%d]: got %d, want 0 (silence)", i, samples[i])
			break
		}
	}
	if samples[leadIn] != 0 {
		t.Errorf("first audio sample: got %d, want 0", samples[leadIn])
	}
}

func TestDecodePlaybackRangeZeroGenEnd(t *testing.T) {
	t.Parallel()
	data := makeTestFZVWithGenRange(50, 0, 0)
	_, samples, err := DecodePlaybackRange(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 50 {
		t.Errorf("sample count: got %d, want 50 (genEnd=0 should default to max)", len(samples))
	}
}

func TestDecodePlaybackRangeGenStartBeyondData(t *testing.T) {
	t.Parallel()
	data := makeTestFZVWithGenRange(50, 999, 1000)
	_, samples, err := DecodePlaybackRange(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 0 {
		t.Errorf("expected empty output when genStart exceeds data, got %d samples", len(samples))
	}
}

func TestExtractPlaybackBrassVoice(t *testing.T) {
	t.Parallel()
	data := makeTestFZVWithGenRange(1000, 100, 900)
	dir := t.TempDir()
	fzvPath := filepath.Join(dir, "brass.fzv")
	if err := os.WriteFile(fzvPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	wavPath := filepath.Join(dir, "brass.wav")
	if err := ExtractPlayback(fzvPath, wavPath, 0); err != nil {
		t.Fatal(err)
	}

	fh, err := os.Open(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close() //nolint:errcheck
	f, err := wav.Read(fh)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Samples) != 800 {
		t.Errorf("WAV sample count: got %d, want 800 (genEnd 900 minus genStart 100)", len(f.Samples))
	}
}

func TestExtractPlaybackWithLeadIn(t *testing.T) {
	t.Parallel()
	data := makeTestFZVWithGenRange(100, 0, 92)
	dir := t.TempDir()
	fzvPath := filepath.Join(dir, "test.fzv")
	if err := os.WriteFile(fzvPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	wavPath := filepath.Join(dir, "test.wav")
	if err := ExtractPlayback(fzvPath, wavPath, 500); err != nil {
		t.Fatal(err)
	}

	fh, err := os.Open(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close() //nolint:errcheck
	f, err := wav.Read(fh)
	if err != nil {
		t.Fatal(err)
	}
	expectedLeadIn := 36000 * 500 / msPerSecond
	expectedTotal := expectedLeadIn + 92
	if len(f.Samples) != expectedTotal {
		t.Errorf("WAV sample count: got %d, want %d (%d lead-in + 92 audio)", len(f.Samples), expectedTotal, expectedLeadIn)
	}
	for i := range expectedLeadIn {
		if f.Samples[i] != 0 {
			t.Errorf("lead-in sample[%d] is not silent: %d", i, f.Samples[i])
			break
		}
	}
}
