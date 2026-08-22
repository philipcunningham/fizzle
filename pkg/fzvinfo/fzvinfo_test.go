package fzvinfo

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// buildFZV creates a minimal FZV for testing.
func buildFZV(t *testing.T, name string, nSamples int, rateIdx uint8) string {
	t.Helper()
	samples := make([]int16, nSamples)
	for i := range samples {
		samples[i] = int16(i % 100)
	}
	data := voiceimport.Encode(samples, rateIdx, name, 0, voiceimport.NoLoop())
	p := filepath.Join(t.TempDir(), "test.fzv")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInfoShowsName(t *testing.T) {
	t.Parallel()
	p := buildFZV(t, "HOOVER", 1000, 0)
	var buf bytes.Buffer
	if err := Info(p, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Voice:       HOOVER") {
		t.Errorf("expected voice name HOOVER:\n%s", buf.String())
	}
}

func TestInfoShowsSampleRate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		idx  uint8
		want string
	}{
		{0, "36000"},
		{1, "18000"},
		{2, "9000"},
	} {
		p := buildFZV(t, "X", 100, tc.idx)
		var buf bytes.Buffer
		if err := Info(p, &buf); err != nil {
			t.Fatalf("rate %d: %v", tc.idx, err)
		}
		if !strings.Contains(buf.String(), tc.want) {
			t.Errorf("rate index %d: expected %s Hz in output:\n%s", tc.idx, tc.want, buf.String())
		}
	}
}

func TestInfoShowsDuration(t *testing.T) {
	t.Parallel()
	// 3600 samples at 36kHz = 0.1 seconds.
	p := buildFZV(t, "X", 3600, 0)
	var buf bytes.Buffer
	if err := Info(p, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "0.100") {
		t.Errorf("expected duration ~0.100s:\n%s", buf.String())
	}
}

func TestInfoDefaultEnvelopeHidden(t *testing.T) {
	t.Parallel()
	// A standard one-shot voice carries default envelopes.
	p := buildFZV(t, "KICK", 1000, 0)
	var buf bytes.Buffer
	if err := Info(p, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "Envelope (DCA)") {
		t.Errorf("default DCA envelope should be hidden for one-shot voice:\n%s", out)
	}
	if strings.Contains(out, "Envelope (DCF)") {
		t.Errorf("default DCF envelope should be hidden for one-shot voice:\n%s", out)
	}
}

func TestInfoDefaultFilterHidden(t *testing.T) {
	t.Parallel()
	// The default filter is cutoff=127, resonance=0.
	p := buildFZV(t, "KICK", 1000, 0)
	var buf bytes.Buffer
	if err := Info(p, &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Filter:") {
		t.Errorf("default filter should be hidden:\n%s", buf.String())
	}
}

func TestInfoNonDefaultEnvelopeShown(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 1000)
	data := voiceimport.Encode(samples, 0, "BELL", 0, voiceimport.NoLoop())
	// dca_sus = 1 makes the envelope non-default.
	data[0x78] = 1
	p := filepath.Join(t.TempDir(), "bell.fzv")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Info(p, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Envelope (DCA)") {
		t.Errorf("non-default DCA envelope should be shown:\n%s", buf.String())
	}
}

func TestInfoEnvelopeRateDisplayCorrect(t *testing.T) {
	t.Parallel()
	// Rate byte 253 = 0xFD = MSB set (falling), magnitude = 0x7D = 125.
	// The hardware display scale ignores the sign bit and shows magnitude
	// only: (125 * 100) >> 7 = 97. This must not display as 3 (which
	// would indicate a two's complement sign-extension bug).
	samples := make([]int16, 1000)
	data := voiceimport.Encode(samples, 0, "TEST", 0, voiceimport.NoLoop())
	data[0x78] = 1   // dca_sus
	data[0x79] = 2   // dca_end
	data[0x7b] = 253 // dca_rate[1] = 0xFD
	p := filepath.Join(t.TempDir(), "test.fzv")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Info(p, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "97") {
		t.Errorf("rate 0xFD should display as 97 (magnitude only):\n%s", out)
	}
	if strings.Contains(out, " 3 ") {
		t.Errorf("rate 0xFD should not display as 3 (sign-extension bug):\n%s", out)
	}
}

func TestInfoLoopShown(t *testing.T) {
	t.Parallel()
	// A voice with loop_sus=0 and valid loop points should show the loop section.
	samples := make([]int16, 1000)
	data := voiceimport.Encode(samples, 0, "PAD", 0, voiceimport.LoopParams{LoopStart: 100, LoopEnd: 800})
	p := filepath.Join(t.TempDir(), "pad.fzv")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Info(p, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Loop:") {
		t.Errorf("expected Loop section for voice with active loop:\n%s", out)
	}
	if !strings.Contains(out, "Sustain on:") {
		t.Errorf("expected 'Sustain on:' in loop section:\n%s", out)
	}
}

func TestInfoLoopHiddenForOneShot(t *testing.T) {
	t.Parallel()
	p := buildFZV(t, "DRUM", 1000, 0)
	var buf bytes.Buffer
	if err := Info(p, &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Loop:") {
		t.Errorf("Loop section should be hidden for one-shot voice:\n%s", buf.String())
	}
}

func TestInfoWrongFileType(t *testing.T) {
	t.Parallel()
	err := Info("../../testdata/synthetic/HOOVER.img", &bytes.Buffer{})
	if err == nil {
		t.Error("expected error for wrong file type")
	}
	if !strings.Contains(err.Error(), "fzvinfo") {
		t.Errorf("expected 'fzvinfo' in error: %v", err)
	}
}

func TestInfoInvalidRateIndex(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 1000)
	data := voiceimport.Encode(samples, 0, "BADRATE", 0, voiceimport.NoLoop())
	data[disk.VoiceSampOffset] = 3

	p := filepath.Join(t.TempDir(), "badrate.fzv")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}

	err := Info(p, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for invalid rate index")
	}
	if !strings.Contains(err.Error(), "does not look like a voice file") {
		t.Errorf("expected 'does not look like a voice file' in error: %v", err)
	}
}

func TestParseRejectsTextLikeFile(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize)
	copy(data[disk.VoiceNameOffset:], "from the lat")
	data[disk.VoiceSampOffset] = 'e'

	p := filepath.Join(t.TempDir(), "text.fzv")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Parse(p)
	if err == nil {
		t.Fatal("expected error for text-like file")
	}
	if !strings.Contains(err.Error(), "does not look like a voice file") {
		t.Errorf("expected 'does not look like a voice file' in error: %v", err)
	}
}

// Both rejections name what they read: the path when the caller passed
// one, the data when it passed bytes. A short file and the neighbouring
// "does not look like a voice file" branch are two halves of one refusal.
func TestParseRejectionsNameWhatTheyRead(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "stub.fzv")
	if err := os.WriteFile(p, make([]byte, 16), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Parse(p)
	if err == nil {
		t.Fatal("expected an error for a file below one sector")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%q is too small", p)) {
		t.Errorf("Parse should name the file it read; got: %v", err)
	}

	_, err = ParseBytes(make([]byte, 16))
	if err == nil {
		t.Fatal("expected an error for bytes below one sector")
	}
	if !strings.Contains(err.Error(), "data is too small") {
		t.Errorf("ParseBytes holds no path, so it speaks of the data; got: %v", err)
	}
}

func TestParseAcceptsAllValidRateIndices(t *testing.T) {
	t.Parallel()
	for rate := byte(0); rate < 3; rate++ {
		samples := make([]int16, 1000)
		data := voiceimport.Encode(samples, rate, "TEST", 0, voiceimport.NoLoop())
		p := filepath.Join(t.TempDir(), fmt.Sprintf("rate%d.fzv", rate))
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
		params, err := Parse(p)
		if err != nil {
			t.Fatalf("rate index %d: unexpected error: %v", rate, err)
		}
		if params.Name == "" {
			t.Errorf("rate index %d: expected non-empty name", rate)
		}
	}
}

func TestParseShowsName(t *testing.T) {
	t.Parallel()
	p := buildFZV(t, "HOOVER", 1000, 0)
	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if params.Name != "HOOVER" {
		t.Errorf("Name = %q, want HOOVER", params.Name)
	}
}

func TestParseSampleRate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		idx  uint8
		want uint32
	}{
		{0, 36000},
		{1, 18000},
		{2, 9000},
	} {
		p := buildFZV(t, "X", 100, tc.idx)
		params, err := Parse(p)
		if err != nil {
			t.Fatalf("rate %d: %v", tc.idx, err)
		}
		if params.SampleRate != tc.want {
			t.Errorf("rate index %d: SampleRate = %d, want %d", tc.idx, params.SampleRate, tc.want)
		}
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()
	p := buildFZV(t, "X", 3600, 0)
	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if params.Duration < 0.099 || params.Duration > 0.101 {
		t.Errorf("Duration = %f, want ~0.1", params.Duration)
	}
}

// TestParseHeaderDurationUsesWaveSpan covers duration for a voice whose
// wave pointers are cumulative FZF audio-area addresses (waveStart > 0),
// the layout ParseVoiceInFZF feeds in. Duration is waveEnd minus
// waveStart; waveEnd alone inflates it by waveStart/rate, so the second
// voice in a 36000 Hz FZF gains 1 s for every 36000 samples ahead of it.
func TestParseHeaderDurationUsesWaveSpan(t *testing.T) {
	t.Parallel()
	// A voice sitting past position 0 in an FZF: waveStart 72000 (2 s at
	// 36 kHz), waveEnd 90000 (0.5 s of audio).
	hdr := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceWaveStartOffset:], 72000)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceWaveEndOffset:], 90000)
	hdr[disk.VoiceSampOffset] = 0 // 36 kHz
	hdr[disk.VoiceLoopModeOffset] = byte(disk.PlaybackModeNormal & 0xff)
	hdr[disk.VoiceLoopModeOffset+1] = byte(disk.PlaybackModeNormal >> 8)
	copy(hdr[disk.VoiceNameOffset:], "WAVESTART   ")

	params, err := parseHeader(hdr, "test")
	if err != nil {
		t.Fatal(err)
	}
	want := 0.5
	if params.Duration < want-0.001 || params.Duration > want+0.001 {
		t.Errorf("Duration = %f, want ~%f (audio is waveEnd-waveStart samples, not waveEnd)", params.Duration, want)
	}
}

// TestParseHeaderSamplesUsesWaveSpan covers the sample count for the same
// cumulative-pointer layout as TestParseHeaderDurationUsesWaveSpan.
// Samples is waveEnd minus waveStart; waveEnd alone inflates it by
// waveStart, the cumulative position of every preceding voice.
func TestParseHeaderSamplesUsesWaveSpan(t *testing.T) {
	t.Parallel()
	// waveStart 72000, waveEnd 90000: 18000 samples of audio.
	hdr := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceWaveStartOffset:], 72000)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceWaveEndOffset:], 90000)
	hdr[disk.VoiceSampOffset] = 0 // 36 kHz
	hdr[disk.VoiceLoopModeOffset] = byte(disk.PlaybackModeNormal & 0xff)
	hdr[disk.VoiceLoopModeOffset+1] = byte(disk.PlaybackModeNormal >> 8)
	copy(hdr[disk.VoiceNameOffset:], "WAVESTART   ")

	params, err := parseHeader(hdr, "test")
	if err != nil {
		t.Fatal(err)
	}
	const want uint32 = 18000
	if params.Samples != want {
		t.Errorf("Samples = %d, want %d (audio is waveEnd-waveStart samples, not waveEnd)", params.Samples, want)
	}
}

// TestParseHeaderLocalisesGenAndWavePointers covers F7's sibling: for a
// voice borrowed from an FZF audio area, WaveEnd, GenStart, and GenEnd
// localise to 0..N by subtracting waveStart, or the rendered "Gen range"
// line shows inflated cumulative addresses. Spec §2-1.
func TestParseHeaderLocalisesGenAndWavePointers(t *testing.T) {
	t.Parallel()
	// Same setup as TestParseHeaderDurationUsesWaveSpan, plus a gen range
	// of 72100..89900, which is 100..17900 voice-local.
	hdr := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceWaveStartOffset:], 72000)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceWaveEndOffset:], 90000)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceGenStartOffset:], 72100)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceGenEndOffset:], 89900)
	hdr[disk.VoiceSampOffset] = 0 // 36 kHz
	hdr[disk.VoiceLoopModeOffset] = byte(disk.PlaybackModeNormal & 0xff)
	hdr[disk.VoiceLoopModeOffset+1] = byte(disk.PlaybackModeNormal >> 8)
	copy(hdr[disk.VoiceNameOffset:], "WAVESTART   ")

	params, err := parseHeader(hdr, "test")
	if err != nil {
		t.Fatal(err)
	}
	if params.WaveEnd != 18000 {
		t.Errorf("WaveEnd = %d, want 18000 (voice-local, waveEnd-waveStart)", params.WaveEnd)
	}
	if params.GenStart != 100 {
		t.Errorf("GenStart = %d, want 100 (voice-local, genStart-waveStart)", params.GenStart)
	}
	if params.GenEnd != 17900 {
		t.Errorf("GenEnd = %d, want 17900 (voice-local, genEnd-waveStart)", params.GenEnd)
	}
}

func TestParseDefaultEnvelope(t *testing.T) {
	t.Parallel()
	p := buildFZV(t, "KICK", 1000, 0)
	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if !params.DCADefault {
		t.Error("expected DCADefault=true for standard one-shot voice")
	}
}

func TestParseLoopDetection(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 1000)
	data := voiceimport.Encode(samples, 0, "PAD", 0, voiceimport.LoopParams{LoopStart: 100, LoopEnd: 800})
	p := filepath.Join(t.TempDir(), "pad.fzv")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if !params.HasActiveLoop {
		t.Error("expected HasActiveLoop=true for voice with loop points")
	}
	if params.LoopStart != 100 {
		t.Errorf("LoopStart = %d, want 100", params.LoopStart)
	}
}

// TestParseLoopHonoursLoopSusIndex pins the loop reader to the pair
// loop_sus selects (0 to 7, spec §2-1), not loopst[0]/looped[0].
func TestParseLoopHonoursLoopSusIndex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		loopSus   uint8
		wantLoop  bool
		wantStart uint32
		wantEnd   uint32
	}{
		{"index 0", 0, true, 100, 200},
		{"index 1", 1, true, 1100, 1200},
		{"index 7", 7, true, 7100, 7200},
		{"no sustain", disk.NoSustainLoop, false, 0, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			samples := make([]int16, 8000)
			data := voiceimport.Encode(samples, 0, "PAD", 0, voiceimport.NoLoop())
			data[disk.VoiceLoopSusOffset] = tc.loopSus
			data[disk.VoiceLoopEndOffset] = disk.HoldIndefinitely
			for i := 0; i < disk.EnvelopeStages; i++ {
				st := uint32(i*1000 + 100)
				ed := uint32(i*1000 + 200)
				binary.LittleEndian.PutUint32(data[disk.VoiceLoopSt0Offset+i*4:], st)
				binary.LittleEndian.PutUint32(data[disk.VoiceLoopEd0Offset+i*4:], ed)
			}
			p := filepath.Join(t.TempDir(), "loop.fzv")
			if err := os.WriteFile(p, data, 0644); err != nil {
				t.Fatal(err)
			}
			params, err := Parse(p)
			if err != nil {
				t.Fatal(err)
			}
			if params.HasActiveLoop != tc.wantLoop {
				t.Errorf("loop_sus=%d HasActiveLoop: got %v, want %v",
					tc.loopSus, params.HasActiveLoop, tc.wantLoop)
			}
			if tc.wantLoop {
				if params.LoopStart != tc.wantStart {
					t.Errorf("loop_sus=%d LoopStart: got %d, want %d",
						tc.loopSus, params.LoopStart, tc.wantStart)
				}
				if params.LoopEnd != tc.wantEnd {
					t.Errorf("loop_sus=%d LoopEnd: got %d, want %d",
						tc.loopSus, params.LoopEnd, tc.wantEnd)
				}
			}
		})
	}
}

// TestParseLoopExposesReleaseAndTimings pins loop_release (the 0x13
// byte) and the loopxf[]/looptm[] entries at index loop_sus.
func TestParseLoopExposesReleaseAndTimings(t *testing.T) {
	t.Parallel()
	const (
		loopSus = 2
		loopEnd = 4
		wantXF  = 256
		wantTm  = 500
	)
	samples := make([]int16, 4000)
	data := voiceimport.Encode(samples, 0, "PAD", 0, voiceimport.NoLoop())
	data[disk.VoiceLoopSusOffset] = loopSus
	data[disk.VoiceLoopEndOffset] = loopEnd
	binary.LittleEndian.PutUint32(data[disk.VoiceLoopSt0Offset+loopSus*4:], 200)
	binary.LittleEndian.PutUint32(data[disk.VoiceLoopEd0Offset+loopSus*4:], 800)
	binary.LittleEndian.PutUint16(data[disk.VoiceLoopXFOffset+loopSus*disk.LoopXFEntrySize:], wantXF)
	binary.LittleEndian.PutUint16(data[disk.VoiceLoopTmOffset+loopSus*disk.LoopTmEntrySize:], wantTm)
	// Populate other indices with distinct values so an off-by-pair read
	// would produce a different observable value.
	binary.LittleEndian.PutUint16(data[disk.VoiceLoopXFOffset:], 999)
	binary.LittleEndian.PutUint16(data[disk.VoiceLoopTmOffset:], 999)

	p := filepath.Join(t.TempDir(), "loop.fzv")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if params.LoopSustain != loopSus {
		t.Errorf("LoopSustain: got %d, want %d", params.LoopSustain, loopSus)
	}
	if params.LoopRelease != loopEnd {
		t.Errorf("LoopRelease: got %d, want %d", params.LoopRelease, loopEnd)
	}
	if params.LoopXF != wantXF {
		t.Errorf("LoopXF: got %d, want %d (must come from index loop_sus)",
			params.LoopXF, wantXF)
	}
	if params.LoopTm != wantTm {
		t.Errorf("LoopTm: got %d, want %d (must come from index loop_sus)",
			params.LoopTm, wantTm)
	}

	// And the JSON form must surface the new keys for downstream consumers.
	var buf bytes.Buffer
	if err := RenderJSON(&buf, params); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"loop_release"`, `"loop_xfade"`, `"loop_time"`} {
		if !strings.Contains(buf.String(), key) {
			t.Errorf("JSON output missing %s:\n%s", key, buf.String())
		}
	}
}

func TestInfoRealHardwareVoice(t *testing.T) {
	t.Parallel()
	const technoImg = "../../testdata/synthetic/TECHNO.img"
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}

	fzf, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}

	// METAL-BELL by hand: 8 bank sectors, then the voice at offset 0.
	bankSectors := 8
	voiceOff := bankSectors * disk.SectorSize
	vhdr := fzf[voiceOff : voiceOff+disk.VoiceHeaderUsed]
	waveEnd := binary.LittleEndian.Uint32(vhdr[4:8])
	voiceSectors := disk.VoiceAreaSectors(11)
	audioStart := (bankSectors + voiceSectors) * disk.SectorSize
	audio := fzf[audioStart : audioStart+int(waveEnd)*2]

	fzv := make([]byte, disk.SectorSize+len(audio))
	copy(fzv[disk.VoiceNameOffset:], "METAL-BELL  ")
	binary.LittleEndian.PutUint32(fzv[0x00:], 0)
	copy(fzv[4:], vhdr[4:disk.VoiceHeaderUsed])
	copy(fzv[disk.SectorSize:], audio)

	fzvPath := filepath.Join(dir, "metal-bell.fzv")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Info(fzvPath, &buf); err != nil {
		t.Fatalf("Info on real hardware voice: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "36000") {
		t.Errorf("expected 36000 Hz sample rate:\n%s", out)
	}
	// Real hardware voice has non-default envelope, so it should be shown.
	if !strings.Contains(out, "Envelope (DCA)") {
		t.Errorf("TECHNO voices have non-default envelope, should be shown:\n%s", out)
	}
}

func TestParseModulationFields(t *testing.T) {
	t.Parallel()
	p := buildFZV(t, "MODTEST", 1000, 0)

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	data[disk.VoiceDCAKFOffset] = 10
	data[disk.VoiceDCARSOffset] = 20
	data[disk.VoiceDCFKFOffset] = 30
	data[disk.VoiceDCFRSOffset] = 40
	data[disk.VoiceVelDCAKFOffset] = 50
	data[disk.VoiceVelDCFKFOffset] = 60
	if err := os.WriteFile(p, data, 0644); err != nil { //nolint:gosec
		t.Fatal(err)
	}

	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if params.DCALevelKF != 10 {
		t.Errorf("DCALevelKF = %d, want 10", params.DCALevelKF)
	}
	if params.DCARateKF != 20 {
		t.Errorf("DCARateKF = %d, want 20", params.DCARateKF)
	}
	if params.DCFLevelKF != 30 {
		t.Errorf("DCFLevelKF = %d, want 30", params.DCFLevelKF)
	}
	if params.DCFRateKF != 40 {
		t.Errorf("DCFRateKF = %d, want 40", params.DCFRateKF)
	}
	if params.VelDCAKF != 50 {
		t.Errorf("VelDCAKF = %d, want 50", params.VelDCAKF)
	}
	if params.VelDCFKF != 60 {
		t.Errorf("VelDCFKF = %d, want 60", params.VelDCFKF)
	}
}

func TestParseLFODepthQ(t *testing.T) {
	t.Parallel()
	p := buildFZV(t, "LFOQTEST", 1000, 0)

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	data[disk.VoiceLFODCQOffset] = 77
	if err := os.WriteFile(p, data, 0644); err != nil { //nolint:gosec
		t.Fatal(err)
	}

	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if params.LFODepthQ != 77 {
		t.Errorf("LFODepthQ = %d, want 77", params.LFODepthQ)
	}
}

func TestRenderJSONBasicFields(t *testing.T) {
	t.Parallel()
	fzvPath := buildFZV(t, "JSONTEST", 1000, 0)
	params, err := Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, params); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, `"name": "JSONTEST"`) {
		t.Errorf("expected name in JSON:\n%s", out)
	}
	if !strings.Contains(out, `"sample_rate": 36000`) {
		t.Errorf("expected sample_rate in JSON:\n%s", out)
	}
	if !strings.Contains(out, `"samples": 1000`) {
		t.Errorf("expected samples in JSON:\n%s", out)
	}
	if !strings.Contains(out, `"dca_rates"`) {
		t.Errorf("expected dca_rates key in JSON:\n%s", out)
	}
}

func TestRenderJSONRoundTrip(t *testing.T) {
	t.Parallel()
	fzvPath := buildFZV(t, "ROUND", 500, 1)
	params, err := Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, params); err != nil {
		t.Fatal(err)
	}

	var decoded VoiceParams
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output is not valid: %v\n%s", err, buf.String())
	}
	if decoded.Name != "ROUND" {
		t.Errorf("decoded name = %q, want ROUND", decoded.Name)
	}
	if decoded.SampleRate != 18000 {
		t.Errorf("decoded sample rate = %d, want 18000", decoded.SampleRate)
	}
	if decoded.Samples != 500 {
		t.Errorf("decoded samples = %d, want 500", decoded.Samples)
	}
}

func TestParseVoiceInFZFNotFoundListsAvailable(t *testing.T) {
	t.Parallel()
	fzfData, _ := fzfbuilder.MakeTestFZF(t, []string{"KICK", "SNARE", "VOX"})
	_, err := ParseVoiceInFZF(fzfData, "MISSING")
	if err == nil {
		t.Fatal("expected error for missing voice")
	}
	msg := err.Error()
	if !strings.Contains(msg, "MISSING") {
		t.Errorf("error should name the requested voice: %v", err)
	}
	if !strings.Contains(msg, "available voices") {
		t.Errorf("error should list available voices: %v", err)
	}
	for _, want := range []string{"KICK", "SNARE", "VOX"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should include %q in available list: %v", want, err)
		}
	}
}

// TestRenderJSONFieldNamesMatchEditFlags pins the JSON keys for fields
// with CLI flag equivalents. The flags --cutoff, --resonance, and --root
// write to identically named --json fields, so an edit-then-verify script
// needs no mapping table.
func TestRenderJSONFieldNamesMatchEditFlags(t *testing.T) {
	t.Parallel()
	fzvPath := buildFZV(t, "JSONKEYS", 100, 0)
	params, err := Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, params); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	for _, key := range []string{"cutoff", "resonance", "root_note"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("JSON output missing top-level key %q (keys: %v)", key, mapKeys(parsed))
		}
	}
	for _, key := range []string{"filter_cutoff", "filter_q", "key_centre"} {
		if _, ok := parsed[key]; ok {
			t.Errorf("JSON output still has legacy key %q (keys: %v)", key, mapKeys(parsed))
		}
	}
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRenderJSONExcludesInternalFields(t *testing.T) {
	t.Parallel()
	fzvPath := buildFZV(t, "INTERNAL", 100, 0)
	params, err := Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, params); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	for _, field := range []string{"loop_mode", "gen_start", "gen_end", "wave_end", "dca_default", "dca_silent", "dcf_default"} {
		if strings.Contains(out, field) {
			t.Errorf("JSON should not contain internal field %q:\n%s", field, out)
		}
	}
}

// TestParseNormalVariantPlaybackMode pins voices carrying the
// undocumented 0x0157 playback mode (seen in the factory Clarinet.fzf) to
// render as "normal_variant" through disk.PlaybackModeName, not as
// "unknown (0x0157)".
func TestParseNormalVariantPlaybackMode(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 100)
	data := voiceimport.Encode(samples, 0, "CLRNT1", 0, voiceimport.NoLoop())
	// Overwrite the loop_mode bytes with PlaybackModeNormalVariant (0x0157).
	binary.LittleEndian.PutUint16(data[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormalVariant)
	p := filepath.Join(t.TempDir(), "clrnt.fzv")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	params, err := Parse(p)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if params.PlaybackMode != disk.PlaybackModeNameNormalVariant {
		t.Errorf("PlaybackMode = %q, want %q", params.PlaybackMode, disk.PlaybackModeNameNormalVariant)
	}
	// Info names the variant rather than falling back to "unknown".
	var buf bytes.Buffer
	if err := Info(p, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), disk.PlaybackModeNameNormalVariant) {
		t.Errorf("Info output missing %q:\n%s", disk.PlaybackModeNameNormalVariant, buf.String())
	}
	if strings.Contains(buf.String(), "unknown") {
		t.Errorf("Info output should not say 'unknown' for the documented variant:\n%s", buf.String())
	}
}

// ParseBytes is the pure in-memory entry point the web core calls:
// the same parse as Parse with no filesystem.
func TestParseBytesMatchesParse(t *testing.T) {
	samples := make([]int16, 2500)
	for i := range samples {
		samples[i] = int16(i % 173)
	}
	data := voiceimport.Encode(samples, 1, "PARSE ME", 0, voiceimport.NoLoop())

	path := filepath.Join(t.TempDir(), "PARSE ME.fzv")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fromPath, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	fromBytes, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if !reflect.DeepEqual(fromPath, fromBytes) {
		t.Fatalf("parses differ:\n%+v\n%+v", fromPath, fromBytes)
	}
}

func TestParseBytesRejectsGarbage(t *testing.T) {
	if _, err := ParseBytes([]byte("not a voice")); err == nil {
		t.Fatal("garbage accepted")
	}
}

// AllLoops exposes all eight loop entries (masked addresses) for the
// Web UI's loop table. It stays out of the JSON so the corpus
// snapshots hold.
func TestParseBytesExposesAllLoops(t *testing.T) {
	samples := make([]int16, 4096)
	data := voiceimport.Encode(samples, 1, "LOOPS", 0, voiceimport.LoopParams{LoopStart: 64, LoopEnd: 1024})
	vp, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if vp.AllLoops[0].Start != 64 || vp.AllLoops[0].End != 1024 {
		t.Fatalf("loop 0 = %+v, want 64..1024", vp.AllLoops[0])
	}
	if vp.AllLoops[0].Start != vp.LoopStart || vp.AllLoops[0].End != vp.LoopEnd {
		t.Fatal("loop 0 disagrees with the active loop fields")
	}
	out, err := json.Marshal(vp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(out), "AllLoops") || strings.Contains(string(out), "all_loops") {
		t.Fatal("AllLoops leaked into the JSON surface")
	}
}

// A rejection has to name the file the user typed (E1). The bytes
// entry point has no path, so it speaks of the data instead.
func TestParseNamesTheFileItRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "NOT A VOICE.fzv")
	if err := os.WriteFile(path, []byte("plain text, not a voice file"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Parse(path)
	if err == nil {
		t.Fatal("garbage accepted by Parse")
	}
	if !namesPath(err, path) {
		t.Errorf("error should name the file; got: %v", err)
	}

	_, err = ParseBytes([]byte("plain text, not a voice file"))
	if err == nil {
		t.Fatal("garbage accepted by ParseBytes")
	}
	if !strings.Contains(err.Error(), "data") {
		t.Errorf("the bytes entry point should speak of the data; got: %v", err)
	}
}

// namesPath reports whether err names path the way this package writes
// it, quoted with %q. That escapes a backslash, so a raw comparison
// misses every Windows path and any name holding one.
func namesPath(err error, path string) bool {
	return err != nil && strings.Contains(err.Error(), fmt.Sprintf("%q", path))
}

// The readout is a display, so it shows the delay the way the panel's
// DELAY row shows it. The stored word counts in 16s; the panel counts
// in 1s.
func TestRenderShowsTheDelayOnThePanelScale(t *testing.T) {
	p := &VoiceParams{
		LFOWaveform:    "sine",
		LFORate:        20,
		LFODelay:       1600, // the panel's 100
		LFOAttack:      5,
		LFODepthPitch:  30,
		LFODepthAmp:    20,
		LFODepthFilter: 50,
	}
	var buf bytes.Buffer
	Render(&buf, p)
	out := buf.String()
	if !strings.Contains(out, "Delay: 100") {
		t.Errorf("render shows the raw word, not the panel's delay:\n%s", out)
	}
	if strings.Contains(out, "Delay: 1600") {
		t.Errorf("render still shows the stored word:\n%s", out)
	}
}

// The readout is a display, so tune shows the way the panel's TUNE row
// shows it: cents, not whole semitones. A voice tuned within a semitone
// used to show nothing at all, because the semitone count rounded to
// zero and the line was skipped.
func TestRenderShowsTuneOnThePanelScale(t *testing.T) {
	// The panel stores 127 for +050.
	p := &VoiceParams{Name: "T", DCP: 127}
	var buf bytes.Buffer
	Render(&buf, p)
	if got := buf.String(); !strings.Contains(got, "Tune:") || !strings.Contains(got, "+50") {
		t.Errorf("render does not show the panel's tune:\n%s", got)
	}
}

// The panel's velocity RESONANCE row carries no sign column, so the
// readout doesn't print one either.
func TestRenderShowsVelocityResonanceUnsigned(t *testing.T) {
	p := &VoiceParams{Name: "T", VelDCQKF: 50}
	var buf bytes.Buffer
	Render(&buf, p)
	out := buf.String()
	if strings.Contains(out, "dcq KF=+50") {
		t.Errorf("velocity resonance printed with a sign the panel has no column for:\n%s", out)
	}
	if !strings.Contains(out, "dcq KF=50") {
		t.Errorf("velocity resonance missing from the readout:\n%s", out)
	}
}

// The attack and the resonance depth are real bytes with no panel row.
// Printing them beside panel values invites a reader to look for them
// on the machine, so they go on their own line, named for what they are.
func TestRenderSeparatesTheBytesThePanelDoesNotShow(t *testing.T) {
	p := &VoiceParams{
		Name:          "T",
		LFOWaveform:   "sine",
		LFODelay:      1600,
		LFOAttack:     5,
		LFODepthPitch: 30,
		LFODepthQ:     7,
	}
	var buf bytes.Buffer
	Render(&buf, p)
	out := buf.String()

	if strings.Contains(out, "Rate: 0   Attack:") {
		t.Errorf("the attack still sits on the panel's own line:\n%s", out)
	}
	if !strings.Contains(out, "No panel row:") {
		t.Errorf("no line names the bytes the panel doesn't show:\n%s", out)
	}
	if !strings.Contains(out, "attack=5") || !strings.Contains(out, "resonance depth=7") {
		t.Errorf("the unexposed bytes are missing from the readout:\n%s", out)
	}
	// The panel's own depths keep their line, without the q.
	if strings.Contains(out, "q=7") {
		t.Errorf("the resonance depth still sits with the panel's depths:\n%s", out)
	}
}

// The panel wraps a tune word past its own span, so a voice detuned by
// whole semitones reads +000 there. The file still carries the detune,
// and it's audible, so the readout names the part the panel can't show
// rather than mimicking the panel into silence.
func TestRenderNamesTheSemitonesThePanelCannotShow(t *testing.T) {
	// Two whole semitones: the panel reads +000 for this word.
	p := &VoiceParams{Name: "T", DCP: 512}
	var buf bytes.Buffer
	Render(&buf, p)
	out := buf.String()
	if !strings.Contains(out, "+000 cents") {
		t.Errorf("the panel's own reading is missing:\n%s", out)
	}
	if !strings.Contains(out, "2 semitones") {
		t.Errorf("the detune the panel can't show is missing:\n%s", out)
	}
}

// Inside the panel's span there are no semitones to name, so the line
// stays as the panel reads it.
func TestRenderLeavesTheCentsAloneInsideThePanelSpan(t *testing.T) {
	p := &VoiceParams{Name: "T", DCP: 127}
	var buf bytes.Buffer
	Render(&buf, p)
	out := buf.String()
	if !strings.Contains(out, "+50 cents") {
		t.Errorf("tune does not read in cents:\n%s", out)
	}
	if strings.Contains(out, "semitone") {
		t.Errorf("a tune inside the panel's span names semitones:\n%s", out)
	}
}
