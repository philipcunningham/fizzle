package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	original := &File{
		SampleRate: 36000,
		Samples:    []int16{0, 100, 200, -100, -200, 32767, -32768},
	}

	var buf bytes.Buffer
	if err := Write(&buf, original); err != nil {
		t.Fatal(err)
	}

	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}

	if got.SampleRate != original.SampleRate {
		t.Errorf("SampleRate: got %d, want %d", got.SampleRate, original.SampleRate)
	}
	if len(got.Samples) != len(original.Samples) {
		t.Fatalf("len(Samples): got %d, want %d", len(got.Samples), len(original.Samples))
	}
	for i := range original.Samples {
		if got.Samples[i] != original.Samples[i] {
			t.Errorf("Samples[%d]: got %d, want %d", i, got.Samples[i], original.Samples[i])
		}
	}
}

func TestReadNotRIFF(t *testing.T) {
	t.Parallel()
	_, err := Read(bytes.NewReader([]byte("NOT A WAV FILE")))
	if err == nil {
		t.Error("expected error for non-RIFF input")
	}
}

func TestWriteNoSamples(t *testing.T) {
	t.Parallel()
	err := Write(&bytes.Buffer{}, &File{SampleRate: 44100})
	if err == nil {
		t.Error("expected error for empty samples")
	}
}

func TestReadSampleRatePreserved(t *testing.T) {
	t.Parallel()
	for _, rate := range []uint32{9000, 18000, 36000, 44100} {
		f := &File{SampleRate: rate, Samples: []int16{1, 2, 3}}
		var buf bytes.Buffer
		if err := Write(&buf, f); err != nil {
			t.Fatalf("rate %d: write: %v", rate, err)
		}
		got, err := Read(&buf)
		if err != nil {
			t.Fatalf("rate %d: read: %v", rate, err)
		}
		if got.SampleRate != rate {
			t.Errorf("rate %d: got SampleRate %d", rate, got.SampleRate)
		}
	}
}

// TestRead24Bit writes a hand-crafted 24-bit WAV and verifies that the reader
// correctly decodes the samples to 16-bit values. This exercises the 24-bit
// path added for SFZ sample libraries.
func TestRead24Bit(t *testing.T) {
	t.Parallel()
	// Build a minimal 24-bit mono WAV by hand.
	// The known 24-bit samples and their expected 16-bit values after >> 8:
	// 0x7FFFFF yields 0x7FFF = 32767 (near full-scale positive)
	// 0x800000 yields -32768 after sign extension and >>8 (but let us check exactly)
	// 0x000000 yields 0
	// 0x010203 yields 0x0102 = 258 (upper two bytes)
	samples24 := [][3]byte{
		{0xFF, 0xFF, 0x7F}, // 0x7FFFFF yields 32767
		{0x00, 0x00, 0x00}, // 0        yields 0
		{0x03, 0x02, 0x01}, // 0x010203 sign-extended then >>8 = 0x0102 = 258
	}
	want := []int16{32767, 0, 258}

	nSamples := len(samples24)
	dataSize := nSamples * 3

	var buf bytes.Buffer
	// RIFF header
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize)) //nolint:errcheck,gosec // G115: test WAV header size fits uint32
	buf.WriteString("WAVE")
	// fmt chunk
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))      //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(44100))   //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(44100*3)) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(3))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(24))      //nolint:errcheck
	// data chunk
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(dataSize)) //nolint:errcheck,gosec // G115: test constant
	for _, s := range samples24 {
		buf.Write(s[:])
	}

	got, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read 24-bit WAV: %v", err)
	}
	if len(got.Samples) != nSamples {
		t.Fatalf("sample count: got %d, want %d", len(got.Samples), nSamples)
	}
	for i, w := range want {
		if got.Samples[i] != w {
			t.Errorf("sample %d: got %d, want %d", i, got.Samples[i], w)
		}
	}
}

func TestRead32Bit(t *testing.T) {
	t.Parallel()
	// A 32-bit sample of 0x7FFFFFFF should map to 32767 after >>16.
	nSamples := 2
	dataSize := nSamples * 4

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize)) //nolint:errcheck
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))      //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(44100))   //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(44100*4)) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(4))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(32))      //nolint:errcheck
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(dataSize))  //nolint:errcheck,gosec // G115: test WAV header size fits uint32
	binary.Write(&buf, binary.LittleEndian, int32(0x7FFFFFFF)) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, int32(0))          //nolint:errcheck

	got, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read 32-bit WAV: %v", err)
	}
	if got.Samples[0] != 32767 {
		t.Errorf("32-bit max: got %d, want 32767", got.Samples[0])
	}
	if got.Samples[1] != 0 {
		t.Errorf("32-bit zero: got %d, want 0", got.Samples[1])
	}
}

func TestReadStereoDecodesInterleaved(t *testing.T) {
	t.Parallel()
	// Build a 4-frame stereo file: frames (L,R) = (100, 200), (101, 201),
	// (102, 202), (103, 203). Data chunk is 4 frames × 2 channels × 2 bytes = 16 bytes.
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+16)) //nolint:errcheck
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))        //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))         //nolint:errcheck // PCM
	binary.Write(&buf, binary.LittleEndian, uint16(2))         //nolint:errcheck // stereo
	binary.Write(&buf, binary.LittleEndian, uint32(44100))     //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(44100*2*2)) //nolint:errcheck // byte rate
	binary.Write(&buf, binary.LittleEndian, uint16(2*2))       //nolint:errcheck // block align (stereo, 16-bit)
	binary.Write(&buf, binary.LittleEndian, uint16(16))        //nolint:errcheck // bits per sample
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(16)) //nolint:errcheck
	for _, v := range []int16{100, 200, 101, 201, 102, 202, 103, 203} {
		binary.Write(&buf, binary.LittleEndian, v) //nolint:errcheck
	}

	got, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read stereo: %v", err)
	}
	if got.Channels != 2 {
		t.Errorf("Channels = %d, want 2", got.Channels)
	}
	wantInterleaved := []int16{100, 200, 101, 201, 102, 202, 103, 203}
	if len(got.Samples) != len(wantInterleaved) {
		t.Fatalf("Samples len = %d, want %d", len(got.Samples), len(wantInterleaved))
	}
	for i, v := range wantInterleaved {
		if got.Samples[i] != v {
			t.Errorf("Samples[%d] = %d, want %d", i, got.Samples[i], v)
		}
	}
	// ExtractChannel(0) to left.
	left := got.ExtractChannel(0)
	wantL := []int16{100, 101, 102, 103}
	for i, v := range wantL {
		if left[i] != v {
			t.Errorf("Left[%d] = %d, want %d", i, left[i], v)
		}
	}
	// ExtractChannel(1) to right.
	right := got.ExtractChannel(1)
	wantR := []int16{200, 201, 202, 203}
	for i, v := range wantR {
		if right[i] != v {
			t.Errorf("Right[%d] = %d, want %d", i, right[i], v)
		}
	}
	// MixChannels to per-frame average.
	mix := got.MixChannels()
	wantMix := []int16{150, 151, 152, 153}
	for i, v := range wantMix {
		if mix[i] != v {
			t.Errorf("Mix[%d] = %d, want %d", i, mix[i], v)
		}
	}
}

// TestWrite_RejectsStereo pins the Read/Write asymmetry: Read
// accepts stereo and exposes ExtractChannel / MixChannels; Write
// emits a mono RIFF header and so MUST refuse a stereo File to
// avoid producing a malformed file (mono header + 2× sample data).
// Callers reduce to mono via ExtractChannel/MixChannels first.
func TestWrite_RejectsStereo(t *testing.T) {
	t.Parallel()
	stereo := &File{
		SampleRate: 44100,
		Channels:   2,
		Samples:    []int16{100, 200, 101, 201},
	}
	var buf bytes.Buffer
	err := Write(&buf, stereo)
	if err == nil {
		t.Fatal("Write(stereo) returned nil; expected ErrChannelCount")
	}
	if !errors.Is(err, ErrChannelCount) {
		t.Errorf("Write(stereo) error = %v; expected wraps ErrChannelCount", err)
	}
	if buf.Len() != 0 {
		t.Errorf("Write(stereo) wrote %d bytes; expected 0", buf.Len())
	}

	// A File with Channels=0 (legacy default-zero from existing
	// callers) or Channels=1 still writes successfully.
	mono := &File{
		SampleRate: 44100,
		Samples:    []int16{100, 200, 101, 201},
	}
	var monoBuf bytes.Buffer
	if err := Write(&monoBuf, mono); err != nil {
		t.Fatalf("Write(channels=0): %v", err)
	}
	monoExplicit := &File{
		SampleRate: 44100,
		Channels:   1,
		Samples:    []int16{100, 200, 101, 201},
	}
	monoBuf.Reset()
	if err := Write(&monoBuf, monoExplicit); err != nil {
		t.Errorf("Write(channels=1): %v", err)
	}
}

func TestReadInvalidChannelCount(t *testing.T) {
	t.Parallel()
	// 5.1 surround (6 channels); still rejected.
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+4)) //nolint:errcheck
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))        //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(6))        //nolint:errcheck // 6 channels
	binary.Write(&buf, binary.LittleEndian, uint32(44100))    //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(44100*12)) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(12))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(16))       //nolint:errcheck
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(4)) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, int32(0))  //nolint:errcheck

	if _, err := Read(&buf); err == nil {
		t.Error("expected error for 6-channel WAV")
	}
}

// buildWAVWithSMPL constructs a minimal 16-bit mono PCM WAV with a SMPL chunk
// containing one forward loop from loopStart to loopEnd.
func buildWAVWithSMPL(t *testing.T, samples []int16, rate uint32, loopStart, loopEnd uint32) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer

	dataSize := uint32(len(samples) * 2) //nolint:gosec // G115: test WAV header size fits uint32
	// smpl chunk: 36-byte header + 24-byte loop record = 60 bytes
	smplSize := uint32(60)
	// RIFF file size: 4 (WAVE) + 8+16 (fmt) + 8+dataSize (data) + 8+smplSize (smpl)
	riffSize := 4 + 24 + 8 + dataSize + 8 + smplSize

	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, riffSize) //nolint:errcheck
	buf.WriteString("WAVE")

	// fmt chunk
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16)) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))  //nolint:errcheck // PCM
	binary.Write(&buf, binary.LittleEndian, uint16(1))  //nolint:errcheck // mono
	binary.Write(&buf, binary.LittleEndian, rate)       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, rate*2)     //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(2))  //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(16)) //nolint:errcheck

	// data chunk
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, dataSize) //nolint:errcheck
	for _, s := range samples {
		binary.Write(&buf, binary.LittleEndian, s) //nolint:errcheck
	}

	// smpl chunk
	buf.WriteString("smpl")
	binary.Write(&buf, binary.LittleEndian, smplSize) //nolint:errcheck
	// 36-byte smpl header: manufacturer(4), product(4), samplePeriod(4),
	// midiUnityNote(4), midiPitchFraction(4), smpteFormat(4), smpteOffset(4),
	// numLoops(4), samplerData(4)
	for range 7 {
		binary.Write(&buf, binary.LittleEndian, uint32(0)) //nolint:errcheck
	}
	binary.Write(&buf, binary.LittleEndian, uint32(1)) //nolint:errcheck // numLoops = 1
	binary.Write(&buf, binary.LittleEndian, uint32(0)) //nolint:errcheck // samplerData
	// loop record: cuePointID(4), type(4)=0 forward, start(4), end(4), fraction(4), playCount(4)
	binary.Write(&buf, binary.LittleEndian, uint32(0)) //nolint:errcheck // cuePointID
	binary.Write(&buf, binary.LittleEndian, uint32(0)) //nolint:errcheck // type = forward
	binary.Write(&buf, binary.LittleEndian, loopStart) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, loopEnd)   //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(0)) //nolint:errcheck // fraction
	binary.Write(&buf, binary.LittleEndian, uint32(0)) //nolint:errcheck // playCount

	return &buf
}

func TestReadSMPLLoopChunk(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 30000)
	buf := buildWAVWithSMPL(t, samples, 44100, 9708, 27357)

	f, err := Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if f.LoopStart != 9708 {
		t.Errorf("LoopStart: got %d, want 9708", f.LoopStart)
	}
	if f.LoopEnd != 27357 {
		t.Errorf("LoopEnd: got %d, want 27357", f.LoopEnd)
	}
}

func TestReadNoSMPLChunk(t *testing.T) {
	t.Parallel()
	original := &File{SampleRate: 44100, Samples: []int16{1, 2, 3}}
	var buf bytes.Buffer
	if err := Write(&buf, original); err != nil {
		t.Fatal(err)
	}
	f, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if f.LoopStart != -1 {
		t.Errorf("LoopStart should be -1 when no SMPL chunk, got %d", f.LoopStart)
	}
	if f.LoopEnd != -1 {
		t.Errorf("LoopEnd should be -1 when no SMPL chunk, got %d", f.LoopEnd)
	}
}

func TestReadSMPLFromRealReese(t *testing.T) {
	t.Parallel()
	f, err := func() (*File, error) {
		fh, err := os.Open("../../testdata/synthetic/JUNGLISM Samples/reese.wav")
		if err != nil {
			return nil, err
		}
		defer fh.Close() //nolint:errcheck
		return Read(fh)
	}()
	if err != nil {
		t.Fatalf("reading reese.wav: %v", err)
	}
	if f.LoopStart != -1 {
		t.Errorf("reese.wav LoopStart: got %d, want -1 (one-shot, no SMPL loop)", f.LoopStart)
	}
	if f.LoopEnd != -1 {
		t.Errorf("reese.wav LoopEnd: got %d, want -1 (one-shot, no SMPL loop)", f.LoopEnd)
	}
}

func TestReadTruncatedDataChunk(t *testing.T) {
	t.Parallel()
	// Build a WAV where the data chunk header claims 1000 bytes but only 10 are present.
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+1000)) //nolint:errcheck
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))    //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))     //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))     //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(44100)) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(88200)) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(2))     //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(16))    //nolint:errcheck
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(1000)) //nolint:errcheck // claims 1000 bytes
	buf.Write(make([]byte, 10))                           // only 10 present

	_, err := Read(&buf)
	if err == nil {
		t.Error("expected error for truncated data chunk")
	}
}

func TestWriteRoundTrip(t *testing.T) {
	t.Parallel()
	original := &File{
		SampleRate: 36000,
		Samples:    []int16{0, 1000, -1000, 32767, -32768},
	}
	var buf bytes.Buffer
	if err := Write(&buf, original); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.SampleRate != original.SampleRate {
		t.Errorf("SampleRate: got %d, want %d", got.SampleRate, original.SampleRate)
	}
	if len(got.Samples) != len(original.Samples) {
		t.Fatalf("len: got %d, want %d", len(got.Samples), len(original.Samples))
	}
	for i, s := range original.Samples {
		if got.Samples[i] != s {
			t.Errorf("sample %d: got %d, want %d", i, got.Samples[i], s)
		}
	}
}

func TestDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rate    uint32
		samples int
		want    float64
	}{
		{0, 100, 0},
		{44100, 44100, 1.0},
		{36000, 0, 0},
	}
	for _, tt := range tests {
		f := &File{SampleRate: tt.rate, Samples: make([]int16, tt.samples)}
		got := f.Duration()
		if got != tt.want {
			t.Errorf("Duration(rate=%d, samples=%d) = %f, want %f",
				tt.rate, tt.samples, got, tt.want)
		}
	}
}

func TestWriteZeroRate(t *testing.T) {
	t.Parallel()
	f := &File{SampleRate: 0, Samples: []int16{1, 2, 3}}
	var buf bytes.Buffer
	err := Write(&buf, f)
	if err == nil {
		t.Fatal("expected error for zero sample rate")
	}
	if !errors.Is(err, ErrSampleRate) {
		t.Errorf("error should wrap ErrSampleRate, got: %v", err)
	}
}

func TestRead8BitRejected(t *testing.T) {
	t.Parallel()
	nSamples := 3
	dataSize := nSamples * 1

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize)) //nolint:errcheck
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))      //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(44100))   //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint32(44100*1)) //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(1))       //nolint:errcheck
	binary.Write(&buf, binary.LittleEndian, uint16(8))       //nolint:errcheck
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(dataSize)) //nolint:errcheck
	buf.Write([]byte{128, 255, 0})

	_, err := Read(&buf)
	if err == nil {
		t.Fatal("expected error for 8-bit WAV")
	}
	if !errors.Is(err, ErrBitDepth) {
		t.Errorf("error should wrap ErrBitDepth, got: %v", err)
	}
}

func TestLoopStartDefaultsToMinusOne(t *testing.T) {
	t.Parallel()
	f := &File{SampleRate: 44100, Samples: []int16{1, 2, 3}}
	var buf bytes.Buffer
	if err := Write(&buf, f); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.LoopStart != -1 || got.LoopEnd != -1 {
		t.Errorf("no SMPL chunk: LoopStart=%d LoopEnd=%d, want both -1", got.LoopStart, got.LoopEnd)
	}
}

func buildRawWAV(fmtChunk, dataChunk []byte, extraChunks ...[]byte) []byte {
	body := make([]byte, 0, 4+len(fmtChunk)+len(dataChunk))
	body = append(body, []byte("WAVE")...)
	if fmtChunk != nil {
		body = append(body, fmtChunk...)
	}
	for _, c := range extraChunks {
		body = append(body, c...)
	}
	if dataChunk != nil {
		body = append(body, dataChunk...)
	}
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(body))) //nolint:gosec // G115: test helper, size always fits uint32
	buf := make([]byte, 0, 4+len(size)+len(body))
	buf = append(buf, []byte("RIFF")...)
	buf = append(buf, size...)
	buf = append(buf, body...)
	return buf
}

func buildFmtChunk(audioFmt, channels uint16, sampleRate uint32, bitsPerSample uint16) []byte { //nolint:unparam // channels varies in intent
	data := make([]byte, 16)
	binary.LittleEndian.PutUint16(data[0:], audioFmt)
	binary.LittleEndian.PutUint16(data[2:], channels)
	binary.LittleEndian.PutUint32(data[4:], sampleRate)
	blockAlign := channels * (bitsPerSample / 8)
	binary.LittleEndian.PutUint32(data[8:], sampleRate*uint32(blockAlign))
	binary.LittleEndian.PutUint16(data[12:], blockAlign)
	binary.LittleEndian.PutUint16(data[14:], bitsPerSample)
	chunk := make([]byte, 0, 4+4+len(data))
	chunk = append(chunk, []byte("fmt ")...)
	sz := make([]byte, 4)
	binary.LittleEndian.PutUint32(sz, uint32(len(data))) //nolint:gosec // G115: test helper, size always fits uint32
	chunk = append(chunk, sz...)
	chunk = append(chunk, data...)
	return chunk
}

func buildDataChunk(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		bitconv.WriteInt16LE(data[i*2:], s)
	}
	chunk := make([]byte, 0, 4+4+len(data))
	chunk = append(chunk, []byte("data")...)
	sz := make([]byte, 4)
	binary.LittleEndian.PutUint32(sz, uint32(len(data))) //nolint:gosec // G115: test helper, size always fits uint32
	chunk = append(chunk, sz...)
	chunk = append(chunk, data...)
	return chunk
}

func buildChunk(id string, data []byte) []byte {
	chunk := make([]byte, 0, len(id)+4+len(data))
	chunk = append(chunk, []byte(id)...)
	sz := make([]byte, 4)
	binary.LittleEndian.PutUint32(sz, uint32(len(data))) //nolint:gosec // G115: test helper, size always fits uint32
	chunk = append(chunk, sz...)
	chunk = append(chunk, data...)
	return chunk
}

func TestReadNotWAVE(t *testing.T) {
	t.Parallel()
	raw := buildRawWAV(nil, nil)
	copy(raw[8:12], []byte("AVI "))
	_, err := Read(bytes.NewReader(raw))
	if !errors.Is(err, ErrNotWAVE) {
		t.Fatalf("expected ErrNotWAVE, got %v", err)
	}
}

func TestReadFmtTooSmall(t *testing.T) {
	t.Parallel()
	smallFmt := buildChunk("fmt ", make([]byte, 8))
	raw := buildRawWAV(smallFmt, nil)
	_, err := Read(bytes.NewReader(raw))
	if !errors.Is(err, ErrFmtChunkSize) {
		t.Fatalf("expected ErrFmtChunkSize, got %v", err)
	}
}

func TestReadNonPCM(t *testing.T) {
	t.Parallel()
	fmt := buildFmtChunk(3, 1, 44100, 16)
	data := buildDataChunk([]int16{100, 200})
	raw := buildRawWAV(fmt, data)
	_, err := Read(bytes.NewReader(raw))
	if !errors.Is(err, ErrUnsupportedPCM) {
		t.Fatalf("expected ErrUnsupportedPCM, got %v", err)
	}
}

func TestReadDataBeforeFmt(t *testing.T) {
	t.Parallel()
	data := buildDataChunk([]int16{100})
	fmt := buildFmtChunk(1, 1, 44100, 16)
	body := make([]byte, 0, 4+len(data)+len(fmt))
	body = append(body, []byte("WAVE")...)
	body = append(body, data...)
	body = append(body, fmt...)
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(body))) //nolint:gosec // G115: test helper, size always fits uint32
	raw := make([]byte, 0, 4+len(size)+len(body))
	raw = append(raw, []byte("RIFF")...)
	raw = append(raw, size...)
	raw = append(raw, body...)
	_, err := Read(bytes.NewReader(raw))
	if !errors.Is(err, ErrDataBeforeFmt) {
		t.Fatalf("expected ErrDataBeforeFmt, got %v", err)
	}
}

func TestReadNoFmtChunk(t *testing.T) {
	t.Parallel()
	unknown := buildChunk("LIST", []byte("test data here"))
	raw := buildRawWAV(nil, nil, unknown)
	_, err := Read(bytes.NewReader(raw))
	if !errors.Is(err, ErrMissingFmt) {
		t.Fatalf("expected ErrMissingFmt, got %v", err)
	}
}

func TestReadNoDataChunk(t *testing.T) {
	t.Parallel()
	fmt := buildFmtChunk(1, 1, 44100, 16)
	raw := buildRawWAV(fmt, nil)
	_, err := Read(bytes.NewReader(raw))
	if !errors.Is(err, ErrMissingData) {
		t.Fatalf("expected ErrMissingData, got %v", err)
	}
}

func TestReadSMPLZeroLoops(t *testing.T) {
	t.Parallel()
	fmt := buildFmtChunk(1, 1, 44100, 16)
	data := buildDataChunk([]int16{100, 200, 300})
	smplData := make([]byte, 36)
	smpl := buildChunk("smpl", smplData)
	raw := buildRawWAV(fmt, data, smpl)
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.LoopStart != -1 || f.LoopEnd != -1 {
		t.Errorf("expected loop defaults (-1,-1), got (%d,%d)", f.LoopStart, f.LoopEnd)
	}
}

func TestReadSMPLTooSmall(t *testing.T) {
	t.Parallel()
	fmt := buildFmtChunk(1, 1, 44100, 16)
	data := buildDataChunk([]int16{100, 200})
	smplData := make([]byte, 20)
	smpl := buildChunk("smpl", smplData)
	raw := buildRawWAV(fmt, data, smpl)
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.LoopStart != -1 || f.LoopEnd != -1 {
		t.Errorf("expected loop defaults (-1,-1) when smpl too small, got (%d,%d)", f.LoopStart, f.LoopEnd)
	}
}

func TestReadUnknownChunkSkipped(t *testing.T) {
	t.Parallel()
	fmt := buildFmtChunk(1, 1, 44100, 16)
	data := buildDataChunk([]int16{100, 200})
	unknown := buildChunk("LIST", []byte("some extra data!"))
	raw := buildRawWAV(fmt, data, unknown)
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Samples) != 2 {
		t.Errorf("expected 2 samples, got %d", len(f.Samples))
	}
}

type errWriter struct {
	n   int
	max int
}

func (w *errWriter) Write(p []byte) (int, error) {
	if w.n+len(p) > w.max {
		remaining := w.max - w.n
		if remaining <= 0 {
			return 0, errors.New("write limit reached")
		}
		w.n += remaining
		return remaining, errors.New("write limit reached")
	}
	w.n += len(p)
	return len(p), nil
}

func TestWriteHeaderError(t *testing.T) {
	t.Parallel()
	f := &File{SampleRate: 44100, Samples: []int16{100, 200}}
	w := &errWriter{max: 10}
	err := Write(w, f)
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestWriteSamplesError(t *testing.T) {
	t.Parallel()
	f := &File{SampleRate: 44100, Samples: []int16{100, 200}}
	w := &errWriter{max: 44}
	err := Write(w, f)
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestWriteWithLoopPointsRoundTrip(t *testing.T) {
	t.Parallel()
	f := &File{
		SampleRate: 44100,
		Samples:    make([]int16, 5000),
		LoopStart:  1000,
		LoopEnd:    4000,
	}
	var buf bytes.Buffer
	if err := Write(&buf, f); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.LoopStart != 1000 {
		t.Errorf("LoopStart: got %d, want 1000", got.LoopStart)
	}
	if got.LoopEnd != 4000 {
		t.Errorf("LoopEnd: got %d, want 4000", got.LoopEnd)
	}
	if len(got.Samples) != 5000 {
		t.Errorf("Samples: got %d, want 5000", len(got.Samples))
	}
}

func TestWriteWithoutLoopNoSmplChunk(t *testing.T) {
	t.Parallel()
	f := &File{SampleRate: 44100, Samples: []int16{100, 200}, LoopStart: -1, LoopEnd: -1}
	var buf bytes.Buffer
	if err := Write(&buf, f); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if bytes.Contains(buf.Bytes(), []byte("smpl")) {
		t.Error("expected no smpl chunk when loop points are not set")
	}
}

func TestWriteInvalidLoopPoints(t *testing.T) {
	t.Parallel()
	f := &File{
		SampleRate: 44100,
		Samples:    make([]int16, 500),
		LoopStart:  100,
		LoopEnd:    50,
	}
	var buf bytes.Buffer
	if err := Write(&buf, f); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got.Samples) != 500 {
		t.Errorf("Samples: got %d, want 500", len(got.Samples))
	}
	if got.LoopStart != -1 || got.LoopEnd != -1 {
		t.Errorf("inverted loop points should produce no SMPL chunk: LoopStart=%d LoopEnd=%d, want both -1", got.LoopStart, got.LoopEnd)
	}
	if bytes.Contains(buf.Bytes(), []byte("smpl")) {
		t.Error("expected no smpl chunk when LoopStart > LoopEnd")
	}
}

func TestReadDuplicateDataChunkUsesLast(t *testing.T) {
	t.Parallel()
	fmtChunk := buildFmtChunk(1, 1, 44100, 16)
	data1 := buildDataChunk([]int16{100, 200})
	data2 := buildDataChunk([]int16{300, 400, 500})
	raw := buildRawWAV(fmtChunk, nil, data1, data2)
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Samples) != 3 {
		t.Errorf("expected 3 samples from second data chunk, got %d", len(f.Samples))
	}
	if f.Samples[0] != 300 {
		t.Errorf("first sample: got %d, want 300", f.Samples[0])
	}
}

func TestReadDuplicateFmtChunkUsesLast(t *testing.T) {
	t.Parallel()
	fmt1 := buildFmtChunk(1, 1, 44100, 16)
	fmt2 := buildFmtChunk(1, 1, 22050, 16)
	data := buildDataChunk([]int16{100})
	body := make([]byte, 0, 4+len(fmt1)+len(fmt2)+len(data))
	body = append(body, []byte("WAVE")...)
	body = append(body, fmt1...)
	body = append(body, fmt2...)
	body = append(body, data...)
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(body))) //nolint:gosec // G115: test helper
	raw := make([]byte, 0, 4+len(size)+len(body))
	raw = append(raw, []byte("RIFF")...)
	raw = append(raw, size...)
	raw = append(raw, body...)
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.SampleRate != 22050 {
		t.Errorf("expected rate 22050 from second fmt chunk, got %d", f.SampleRate)
	}
}

// TestWriteMIDIUnityNoteRoundTrip pins the WAV writer's behaviour for the
// SMPL chunk's MIDIUnityNote field, which the voice -> WAV pipeline uses
// to preserve the FZV root note across extracts. A zero value falls back
// to middle C (60) for back-compat with callers that haven't been
// updated; any non-zero value is round-tripped verbatim.
func TestWriteMIDIUnityNoteRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		write      uint8
		wantParsed uint8
	}{
		{"unset falls back to 60", 0, 60},
		{"non-default root preserved", 36, 36},
		{"high root preserved", 96, 96},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &File{
				SampleRate:    44100,
				Samples:       make([]int16, 100),
				LoopStart:     10,
				LoopEnd:       90,
				MIDIUnityNote: tc.write,
			}
			var buf bytes.Buffer
			if err := Write(&buf, f); err != nil {
				t.Fatalf("Write: %v", err)
			}
			got, err := Read(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got.MIDIUnityNote != tc.wantParsed {
				t.Errorf("MIDIUnityNote: got %d, want %d", got.MIDIUnityNote, tc.wantParsed)
			}
		})
	}
}

// TestWriteMIDIUnityNoteWithoutLoop pins the WAV writer's behaviour for the
// one-shot SMPL chunk path: when MIDIUnityNote is non-zero but no loop is
// set, Write must still emit a SMPL chunk (with NumSampleLoops = 0) so the
// root note round-trips. This is the regression coverage for F10.
func TestWriteMIDIUnityNoteWithoutLoop(t *testing.T) {
	t.Parallel()
	f := &File{
		SampleRate:    44100,
		Samples:       make([]int16, 100),
		LoopStart:     -1,
		LoopEnd:       -1,
		MIDIUnityNote: 36,
	}
	var buf bytes.Buffer
	if err := Write(&buf, f); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("smpl")) {
		t.Fatal("expected smpl chunk when MIDIUnityNote is non-zero")
	}
	got, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.LoopStart != -1 {
		t.Errorf("LoopStart: got %d, want -1", got.LoopStart)
	}
	if got.LoopEnd != -1 {
		t.Errorf("LoopEnd: got %d, want -1", got.LoopEnd)
	}
	if got.MIDIUnityNote != 36 {
		t.Errorf("MIDIUnityNote: got %d, want 36", got.MIDIUnityNote)
	}
}

func TestReadFmtZeroSampleRate(t *testing.T) {
	t.Parallel()
	// A fmt chunk with sample rate 0 should be rejected at the parser level
	// so downstream consumers can rely on SampleRate being non-zero.
	fmtChunk := buildFmtChunk(1, 1, 0, 16)
	data := buildDataChunk([]int16{100, 200})
	raw := buildRawWAV(fmtChunk, data)
	_, err := Read(bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected error for zero sample rate")
	}
	if !errors.Is(err, ErrSampleRate) {
		t.Errorf("error should wrap ErrSampleRate, got: %v", err)
	}
}

func TestReadChunkSizeOverflow(t *testing.T) {
	t.Parallel()
	// A chunk whose declared size is 0xFFFFFFFF used to wrap to 0 when the
	// parser incremented the padded length, causing misaligned parsing of
	// later chunks. The parser must instead return a clean error.
	fmtChunk := buildFmtChunk(1, 1, 44100, 16)
	// Build a forged chunk header with id="JUNK" and size=0xFFFFFFFF, no body.
	bogus := make([]byte, 8)
	copy(bogus, "JUNK")
	binary.LittleEndian.PutUint32(bogus[4:8], math.MaxUint32)
	data := buildDataChunk([]int16{100, 200})
	raw := buildRawWAV(fmtChunk, data, bogus)
	_, err := Read(bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected error for chunk size 0xFFFFFFFF")
	}
	if !errors.Is(err, ErrChunkSize) {
		t.Errorf("error should wrap ErrChunkSize, got: %v", err)
	}
}

func TestReadSMPLLoopEndBeyondSampleCount(t *testing.T) {
	t.Parallel()
	// SMPL loopEnd is past the end of the sample array. The reader should
	// clear the loop fields rather than propagate out-of-range indices.
	samples := make([]int16, 100)
	buf := buildWAVWithSMPL(t, samples, 44100, 10, 500)
	f, err := Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if f.LoopStart != -1 || f.LoopEnd != -1 {
		t.Errorf("out-of-range loop should be cleared: got LoopStart=%d LoopEnd=%d, want -1, -1",
			f.LoopStart, f.LoopEnd)
	}
}

func TestReadSMPLLoopStartBeyondSampleCount(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 100)
	buf := buildWAVWithSMPL(t, samples, 44100, 200, 300)
	f, err := Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if f.LoopStart != -1 || f.LoopEnd != -1 {
		t.Errorf("out-of-range loop should be cleared: got LoopStart=%d LoopEnd=%d, want -1, -1",
			f.LoopStart, f.LoopEnd)
	}
}

func TestWriteSmplChunkError(t *testing.T) {
	t.Parallel()
	f := &File{SampleRate: 44100, Samples: []int16{100, 200}, LoopStart: 0, LoopEnd: 1}
	var buf bytes.Buffer
	if err := Write(&buf, f); err != nil {
		t.Fatalf("Write: %v", err)
	}
	headerAndData := buf.Len() - int(8+smplHeaderSize+smplLoopRecordSize)
	w := &errWriter{max: headerAndData + 4}
	err := Write(w, f)
	if err == nil {
		t.Fatal("expected write error for smpl chunk")
	}
}
