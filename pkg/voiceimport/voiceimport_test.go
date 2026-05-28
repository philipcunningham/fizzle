package voiceimport

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

func TestEncodeUnsupportedRate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "test.wav")
	f := &wav.File{SampleRate: 44100, Samples: []int16{1, 2, 3}}
	fh, err := os.Create(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := wav.Write(fh, f); err != nil {
		fh.Close() //nolint:errcheck
		t.Fatal(err)
	}
	fh.Close() //nolint:errcheck

	fzvPath := filepath.Join(dir, "test.fzv")
	err = Import(wavPath, fzvPath, 44100)
	if err == nil {
		t.Fatal("expected error for unsupported rate")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should contain 'unsupported': %v", err)
	}
}

func TestRateIndex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rate uint32
		want uint8
	}{
		{36000, 0},
		{18000, 1},
		{9000, 2},
	}
	for _, tt := range tests {
		got, ok := disk.RateIndexFor(tt.rate)
		if !ok {
			t.Errorf("rate %d not found in RateIndexFor", tt.rate)
			continue
		}
		if got != tt.want {
			t.Errorf("RateIndexFor(%d) = %d, want %d", tt.rate, got, tt.want)
		}
	}
}

func TestResampleIdentity(t *testing.T) {
	t.Parallel()
	f := &wav.File{
		SampleRate: 36000,
		Samples:    []int16{100, 200, 300, -100, -200},
	}
	out, err := fzutil.Resample(f, 36000)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(f.Samples) {
		t.Fatalf("len mismatch: got %d, want %d", len(out), len(f.Samples))
	}
	for i := range f.Samples {
		if out[i] != f.Samples[i] {
			t.Errorf("samples[%d]: got %d, want %d", i, out[i], f.Samples[i])
		}
	}
}

func TestResampleDownsample(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 3600)
	for i := range samples {
		samples[i] = int16(i % 100)
	}
	f := &wav.File{SampleRate: 36000, Samples: samples}
	out, err := fzutil.Resample(f, 18000)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1800 {
		t.Errorf("expected 1800 samples after 2x downsample, got %d", len(out))
	}
}

func decodeHeader(t *testing.T, data []byte) voiceHeader {
	t.Helper()
	var hdr voiceHeader
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &hdr); err != nil {
		t.Fatalf("decoding voice header: %v", err)
	}
	return hdr
}

// TestVoiceHeaderBinarySizeMatchesSpec mechanically enforces the invariant
// that Encode's binary.Write panic guards rely on: voiceHeader's binary
// footprint must equal the spec's documented voicedata size (0xC0 = 192
// bytes, spec §2-1) and must be statically determinable.
//
// binary.Size returns -1 for any type that contains a variable-length field
// (slice, string, interface) and returns the byte count of the fixed-size
// representation otherwise. If a future struct change either grows the
// layout or introduces a non-marshallable field, binary.Write would panic
// at runtime on every Encode call. This test catches the regression at
// build time instead.
func TestVoiceHeaderBinarySizeMatchesSpec(t *testing.T) {
	t.Parallel()
	got := binary.Size(voiceHeader{})
	if got != disk.VoiceHeaderUsed {
		t.Errorf("binary.Size(voiceHeader{}) = %d, want %d (spec §2-1: voicedata total byte = 0x%X)",
			got, disk.VoiceHeaderUsed, disk.VoiceHeaderUsed)
	}
}

func TestEncodeWaveEndField(t *testing.T) {
	t.Parallel()
	hdr := decodeHeader(t, Encode([]int16{1, 2, 3, 4}, 0, "TEST", 0, NoLoop()))
	if hdr.WaveEnd != 4 {
		t.Errorf("WaveEnd: got %d, want 4", hdr.WaveEnd)
	}
}

func TestEncodeWaveStartZero(t *testing.T) {
	t.Parallel()
	hdr := decodeHeader(t, Encode([]int16{1, 2, 3}, 0, "X", 0, NoLoop()))
	if hdr.WaveStart != 0 {
		t.Errorf("WaveStart: got %d, want 0", hdr.WaveStart)
	}
}

func TestEncodeRateIndex(t *testing.T) {
	t.Parallel()
	hdr := decodeHeader(t, Encode([]int16{1, 2, 3}, 2, "X", 0, NoLoop()))
	if hdr.Samp != 2 {
		t.Errorf("Samp: got %d, want 2", hdr.Samp)
	}
}

func TestEncodeName(t *testing.T) {
	t.Parallel()
	hdr := decodeHeader(t, Encode([]int16{1, 2, 3}, 0, "MYKICK", 0, NoLoop()))
	got := string(hdr.Name[:6])
	if got != "MYKICK" {
		t.Errorf("Name: got %q, want %q", got, "MYKICK")
	}
}

func TestVelocitySensitivity(t *testing.T) {
	t.Parallel()
	hdr := decodeHeader(t, Encode([]int16{1, 2, 3}, 0, "X", 0, NoLoop()))
	if hdr.VelDCAKF == 0 {
		t.Error("vel_dca_kf=0: velocity has no effect on amplitude. All notes play at full volume regardless of how hard they are struck")
	}
	if int8(hdr.VelDCAKF) < 0 { //nolint:gosec // G115: intentional sign-bit check
		t.Errorf("vel_dca_kf=%d is negative: higher velocity would make notes quieter", int8(hdr.VelDCAKF)) //nolint:gosec // G115: intentional sign-bit check
	}
}

// TestEnvelopeDefaults verifies the default DCA and DCF envelope configuration.
// DCA: stage 0 opens to full level at max rate. Stages 1-7 decay to silence
// at the hardware idle rate, producing a clean amplitude release on note-off.
// DCF: dcf=127 (max offset) keeps the filter fully open. Stage 0 ramps to
// full level. Stages 1-7 are inert (rate=0, stop=255) so the filter stays
// open through note release with no sweep.
func TestEnvelopeDefaults(t *testing.T) {
	t.Parallel()
	hdr := decodeHeader(t, Encode([]int16{1, 2, 3}, 0, "X", 0, NoLoop()))

	if hdr.DCASus != 0 {
		t.Errorf("DCA sus: got %d, want 0", hdr.DCASus)
	}
	if hdr.DCAEnd != 7 {
		t.Errorf("DCA end: got %d, want 7", hdr.DCAEnd)
	}
	if hdr.DCARate[0] != disk.EnvelopeMaxRate {
		t.Errorf("DCA rate[0]: got %d, want %d (max rate attack)", hdr.DCARate[0], disk.EnvelopeMaxRate)
	}
	if hdr.DCAStop[0] != disk.EnvelopeFullLevel {
		t.Errorf("DCA stop[0]: got %d, want %d (full level)", hdr.DCAStop[0], disk.EnvelopeFullLevel)
	}
	for i := 1; i < 8; i++ {
		if hdr.DCARate[i] != disk.EnvelopeIdleRate {
			t.Errorf("DCA rate[%d]: got 0x%02x, want 0x%02x (idle: falling)", i, hdr.DCARate[i], disk.EnvelopeIdleRate)
		}
		if hdr.DCAStop[i] != 0 {
			t.Errorf("DCA stop[%d]: got %d, want 0 (decay to silence)", i, hdr.DCAStop[i])
		}
	}

	if hdr.DCF != disk.DCFMaxOffset {
		t.Errorf("DCF offset: got %d, want %d (max offset, filter fully open)", hdr.DCF, disk.DCFMaxOffset)
	}
	if hdr.DCFSus != 0 {
		t.Errorf("DCF sus: got %d, want 0", hdr.DCFSus)
	}
	if hdr.DCFEnd != 7 {
		t.Errorf("DCF end: got %d, want 7", hdr.DCFEnd)
	}
	if hdr.DCFRate[0] != disk.EnvelopeMaxRate {
		t.Errorf("DCF rate[0]: got %d, want %d", hdr.DCFRate[0], disk.EnvelopeMaxRate)
	}
	for i := 1; i < 8; i++ {
		if hdr.DCFRate[i] != 0 {
			t.Errorf("DCF rate[%d]: got %d, want 0 (inert)", i, hdr.DCFRate[i])
		}
		if hdr.DCFStop[i] != disk.EnvelopeFullLevel {
			t.Errorf("DCF stop[%d]: got %d, want %d (fully open)", i, hdr.DCFStop[i], disk.EnvelopeFullLevel)
		}
	}
}

func TestEncodeTranspose(t *testing.T) {
	t.Parallel()
	hdr := decodeHeader(t, Encode([]int16{1, 2, 3}, 0, "X", 2, NoLoop()))
	if hdr.DCP != 2*256 {
		t.Errorf("DCP with transpose=2: got %d, want %d", hdr.DCP, 2*256)
	}
}

// TestEncodeTransposeClamped guards against silent int16 wrap when a caller
// passes a transpose value outside the FZ-1 dcp field's representable range.
// Without the defensive clamp in Encode, transpose=200 would write
// uint16(200*256) = 51200, which reinterpreted as int16 yields -14336: i.e.
// a wildly wrong negative pitch instead of the intended upward transpose.
func TestEncodeTransposeClamped(t *testing.T) {
	t.Parallel()
	data := Encode([]int16{1, 2, 3}, 0, "X", 200, NoLoop())
	hdr := decodeHeader(t, data)
	// DCP stored at offset 0x74 (2 bytes, little-endian, signed int per spec).
	rawDCP := binary.LittleEndian.Uint16(data[0x74:])
	if rawDCP != hdr.DCP {
		t.Fatalf("DCP bytes at 0x74 disagree with decoded header: raw=%d hdr=%d", rawDCP, hdr.DCP)
	}
	signed := int16(rawDCP) //nolint:gosec // intentional bit-pattern reinterpret matching FZ-1 spec
	if signed != MaxTranspose*disk.SemitoneDCPScale {
		t.Errorf("DCP with transpose=200 (out of range): got signed %d, want %d (clamped to +127 semitones)",
			signed, MaxTranspose*disk.SemitoneDCPScale)
	}

	// Negative side: transpose=-200 should clamp to -127, not wrap.
	dataNeg := Encode([]int16{1, 2, 3}, 0, "X", -200, NoLoop())
	rawNeg := binary.LittleEndian.Uint16(dataNeg[0x74:])
	signedNeg := int16(rawNeg) //nolint:gosec // intentional bit-pattern reinterpret matching FZ-1 spec
	if signedNeg != MinTranspose*disk.SemitoneDCPScale {
		t.Errorf("DCP with transpose=-200 (out of range): got signed %d, want %d (clamped to -127 semitones)",
			signedNeg, MinTranspose*disk.SemitoneDCPScale)
	}
}

func TestBaseName(t *testing.T) {
	t.Parallel()
	tests := []struct{ path, want string }{
		{"/path/to/HOOVER.wav", "HOOVER"},
		{"STAB.wav", "STAB"},
		{"kick drum.wav", "KICK DRUM"},
		{"noext", "NOEXT"},
	}
	for _, tt := range tests {
		if got := fzutil.VoiceName(tt.path); got != tt.want {
			t.Errorf("fzutil.VoiceName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// TestImportWarnsSizeExceedsDisk verifies that importing a very long sample
// (one that produces a voice file larger than a floppy disk) logs a WARN.
// Not parallel: CaptureLog redirects the global logger.
func TestImportWarnsSizeExceedsDisk(t *testing.T) {
	buf := testutil.CaptureLog(t)

	// Create a WAV with enough samples to exceed disk.UsableDataSize after encoding.
	// UsableDataSize ≈ 1,308,672 bytes; at 36kHz that's ~654,336 samples.
	// Use 700,000 samples to be safely over.
	nSamples := 700000
	samples := make([]int16, nSamples)
	f := &wav.File{SampleRate: 36000, Samples: samples}

	dir := t.TempDir()
	wavPath := filepath.Join(dir, "long.wav")
	fh, err := os.Create(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := wav.Write(fh, f); err != nil {
		fh.Close() //nolint:errcheck
		t.Fatal(err)
	}
	fh.Close() //nolint:errcheck

	fzvPath := filepath.Join(dir, "long.fzv")
	if err := Import(wavPath, fzvPath, 36000); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= int64(disk.UsableDataSize) {
		t.Skipf("test voice (%d bytes) did not exceed UsableDataSize (%d); adjust sample count", info.Size(), disk.UsableDataSize)
	}
	if !testutil.BufHasWarnContaining(buf, "exceeds floppy disk capacity") {
		t.Error("expected disk capacity warning for oversized voice file")
	}
}

// TestImportPreservesSMPLLoop is a regression guard for the documented
// behaviour in docs/fizzle-manual.md: "Loops carried in WAV SMPL chunks are
// imported automatically by fzv import."
func TestImportPreservesSMPLLoop(t *testing.T) {
	t.Parallel()
	const (
		rate      = 36000
		nSamples  = 36000
		loopStart = 4000
		loopEnd   = 32000
	)
	samples := make([]int16, nSamples)
	f := &wav.File{
		SampleRate: rate,
		Samples:    samples,
		LoopStart:  loopStart,
		LoopEnd:    loopEnd,
	}

	dir := t.TempDir()
	wavPath := filepath.Join(dir, "looped.wav")
	fh, err := os.Create(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := wav.Write(fh, f); err != nil {
		fh.Close() //nolint:errcheck
		t.Fatal(err)
	}
	fh.Close() //nolint:errcheck

	fzvPath := filepath.Join(dir, "looped.fzv")
	if err := Import(wavPath, fzvPath, rate); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	hdr := decodeHeader(t, data)

	if hdr.LoopSus != 0 {
		t.Errorf("LoopSus: got %d, want 0 (sustain on first loop)", hdr.LoopSus)
	}
	if hdr.LoopEnd != disk.HoldIndefinitely {
		t.Errorf("LoopEnd: got %d, want %d (hold indefinitely)", hdr.LoopEnd, disk.HoldIndefinitely)
	}
	if hdr.LoopSt[0] != loopStart {
		t.Errorf("LoopSt[0]: got %d, want %d", hdr.LoopSt[0], loopStart)
	}
	if hdr.LoopEd[0] != loopEnd {
		t.Errorf("LoopEd[0]: got %d, want %d", hdr.LoopEd[0], loopEnd)
	}
}

// TestImportScalesSMPLLoopOnResample verifies the loop points carried in a
// WAV SMPL chunk are scaled to the target sample rate when fzv import has
// to resample.
func TestImportScalesSMPLLoopOnResample(t *testing.T) {
	t.Parallel()
	const (
		srcRate    = 36000
		dstRate    = 18000
		nSamples   = 36000
		loopStart  = 4000
		loopEnd    = 32000
		scaledStrt = loopStart * dstRate / srcRate
		scaledEnd  = loopEnd * dstRate / srcRate
	)
	samples := make([]int16, nSamples)
	f := &wav.File{
		SampleRate: srcRate,
		Samples:    samples,
		LoopStart:  loopStart,
		LoopEnd:    loopEnd,
	}

	dir := t.TempDir()
	wavPath := filepath.Join(dir, "looped.wav")
	fh, err := os.Create(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := wav.Write(fh, f); err != nil {
		fh.Close() //nolint:errcheck
		t.Fatal(err)
	}
	fh.Close() //nolint:errcheck

	fzvPath := filepath.Join(dir, "looped.fzv")
	if err := Import(wavPath, fzvPath, dstRate); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	hdr := decodeHeader(t, data)

	if hdr.LoopSus != 0 {
		t.Errorf("LoopSus: got %d, want 0 (sustain on first loop)", hdr.LoopSus)
	}
	if hdr.LoopSt[0] != scaledStrt {
		t.Errorf("LoopSt[0]: got %d, want %d (scaled %d->%d Hz)", hdr.LoopSt[0], scaledStrt, srcRate, dstRate)
	}
	if hdr.LoopEd[0] != scaledEnd {
		t.Errorf("LoopEd[0]: got %d, want %d (scaled %d->%d Hz)", hdr.LoopEd[0], scaledEnd, srcRate, dstRate)
	}
}

func TestEncodeLoopPoints(t *testing.T) {
	t.Parallel()
	loop := LoopParams{LoopStart: 100, LoopEnd: 500}
	hdr := decodeHeader(t, Encode([]int16{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 0, "LOOP", 0, loop))
	if hdr.LoopSus != 0 {
		t.Errorf("LoopSus: got %d, want 0 (sustain on first loop)", hdr.LoopSus)
	}
	if hdr.LoopEnd != 7 {
		t.Errorf("LoopEnd: got %d, want 7 (hold indefinitely)", hdr.LoopEnd)
	}
	if hdr.LoopSt[0] != 100 {
		t.Errorf("LoopSt[0]: got %d, want 100", hdr.LoopSt[0])
	}
	if hdr.LoopEd[0] != 500 {
		t.Errorf("LoopEd[0]: got %d, want 500", hdr.LoopEd[0])
	}
	genEnd := hdr.GenEnd
	for i := 1; i < 8; i++ {
		if hdr.LoopSt[i] != genEnd {
			t.Errorf("LoopSt[%d]: got %d, want %d (genEnd)", i, hdr.LoopSt[i], genEnd)
		}
		if hdr.LoopEd[i] != genEnd {
			t.Errorf("LoopEd[%d]: got %d, want %d (genEnd)", i, hdr.LoopEd[i], genEnd)
		}
	}
}
