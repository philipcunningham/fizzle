package audio

import (
	"encoding/binary"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
)

// buildVoice constructs the smallest pair of byte slices that
// decodeSource will accept: a 192-byte voice header with a valid
// sample-rate index, root note, and a gen range covering the audio,
// plus an audio-area slice the wave/gen pointers index into. The
// audio area carries a ramp (audio[i] = i) so decode errors surface
// as wrong sample values rather than silent zero output.
func buildVoice(t *testing.T, nSamples uint32, sampleByte byte, rootNote uint8) (voice, audioArea []byte) {
	t.Helper()
	if nSamples == 0 {
		t.Fatalf("buildVoice: nSamples must be >= 1")
	}

	voice = make([]byte, disk.VoiceHeaderUsed)
	// wave start = 0, wave end = nSamples, gen start = 0, gen end = nSamples.
	binary.LittleEndian.PutUint32(voice[disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(voice[disk.VoiceWaveEndOffset:], nSamples)
	binary.LittleEndian.PutUint32(voice[disk.VoiceGenStartOffset:], 0)
	binary.LittleEndian.PutUint32(voice[disk.VoiceGenEndOffset:], nSamples)
	voice[disk.VoiceSampOffset] = sampleByte
	voice[disk.VoiceKeyCentOffset] = rootNote

	audioArea = make([]byte, int(nSamples)*disk.BytesPerSample)
	for i := uint32(0); i < nSamples; i++ {
		bitconv.WriteInt16LE(audioArea[i*disk.BytesPerSample:], int16(i)) //nolint:gosec // tiny sample count fits int16
	}
	return voice, audioArea
}

// fakePlayer is a synchronous oto.Player stand-in. It reports
// IsPlaying() = true once Play() is called and returns to false once
// Pause() is called or its goroutine drains the underlying reader.
// "Drain" is deliberately fast (one poll interval) so tests do not
// have to wait for real audio time.
type fakePlayer struct {
	mu       sync.Mutex
	playing  bool
	paused   bool
	stopOnce sync.Once
	stopCh   chan struct{}
}

func newFakePlayer() *fakePlayer {
	return &fakePlayer{stopCh: make(chan struct{})}
}

func (f *fakePlayer) Play() {
	f.mu.Lock()
	if f.playing {
		f.mu.Unlock()
		return
	}
	f.playing = true
	f.mu.Unlock()
	// Schedule a natural completion shortly so tests that don't call
	// Stop still see IsPlaying() drop to false.
	go func() {
		select {
		case <-time.After(50 * time.Millisecond):
		case <-f.stopCh:
		}
		f.mu.Lock()
		f.playing = false
		f.mu.Unlock()
	}()
}

func (f *fakePlayer) Pause() {
	f.mu.Lock()
	f.paused = true
	f.playing = false
	f.mu.Unlock()
	f.stopOnce.Do(func() { close(f.stopCh) })
}

func (f *fakePlayer) IsPlaying() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.playing
}

// fakeFactory tracks how many players have been minted so tests can
// assert that Audition reached the playback goroutine.
type fakeFactory struct {
	created int32
}

func (f *fakeFactory) make(_ io.Reader) playerHandle {
	atomic.AddInt32(&f.created, 1)
	return newFakePlayer()
}

// newTestEngine returns an engine pre-wired to a fake player factory.
// The engine's init() will not call the real oto path.
func newTestEngine() (*engine, *fakeFactory) {
	ff := &fakeFactory{}
	e := &engine{makeFn: ff.make}
	return e, ff
}

// waitFor polls until cond returns true or the deadline elapses.
// Cleaner than scattering time.Sleep across tests; the cadence is
// short so failed conditions surface quickly.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("waitFor timeout: %s", msg)
}

func TestDecodeSource_ValidVoice(t *testing.T) {
	voice, fzf := buildVoice(t, 64, 0 /* 36 kHz */, 60)
	src, err := decodeSource(voice, fzf)
	if err != nil {
		t.Fatalf("decodeSource: %v", err)
	}
	if got, want := src.sampleRate, uint32(36000); got != want {
		t.Errorf("sampleRate = %d, want %d", got, want)
	}
	if got, want := src.rootNote, uint8(60); got != want {
		t.Errorf("rootNote = %d, want %d", got, want)
	}
	if got, want := len(src.samples), 64; got != want {
		t.Errorf("len(samples) = %d, want %d", got, want)
	}
	// Ramp fidelity: ensure we read the audio bytes correctly.
	if src.samples[0] != 0 || src.samples[63] != 63 {
		t.Errorf("ramp sample values: [0]=%d [63]=%d, want 0 and 63", src.samples[0], src.samples[63])
	}
}

func TestDecodeSource_RejectsTruncated(t *testing.T) {
	if _, err := decodeSource(make([]byte, 16), make([]byte, 16)); err == nil {
		t.Fatalf("expected error for short voiceBytes")
	}
}

func TestDecodeSource_RejectsBadRateIndex(t *testing.T) {
	voice, fzf := buildVoice(t, 8, 7 /* invalid */, 60)
	if _, err := decodeSource(voice, fzf); err == nil {
		t.Fatalf("expected error for invalid sample rate index")
	}
}

// TestDecodeSource_RootNoteZeroIsLiteralCminus1 pins a real audition
// bug: the old code treated KeyCent == 0 as an "unset" sentinel and
// fell back to MIDI 60 (middle C). But 0 is the literal C-1 root and
// AMEN-style slices on the JUNGLISM disk use exactly that. The
// fallback applied a phantom -60 semitone shift, producing the
// "totally broken and distorted" playback the user heard.
func TestDecodeSource_RootNoteZeroIsLiteralCminus1(t *testing.T) {
	voice, audio := buildVoice(t, 64, 0 /* 36 kHz */, 0 /* root C-1 */)
	src, err := decodeSource(voice, audio)
	if err != nil {
		t.Fatalf("decodeSource: %v", err)
	}
	if src.rootNote != 0 {
		t.Errorf("rootNote = %d, want 0 (no silent fallback to 60)", src.rootNote)
	}
}

func TestRenderPCM_PitchMaths(t *testing.T) {
	// At root pitch (pitch == rootNote) and otoSampleRate == sampleRate,
	// the step is exactly 1, so output frame count after the lead-in
	// equals the input sample count and each output frame echoes the
	// source sample.
	src := &source{
		samples:    []int16{0, 100, 200, 300},
		sampleRate: otoSampleRate,
		rootNote:   60,
	}
	pcm := renderPCM(src, 60)
	leadInFrames := leadInMs * otoSampleRate / 1000
	if got, want := len(pcm), (leadInFrames+4)*otoBytesPerFrame; got != want {
		t.Fatalf("len(pcm) = %d, want %d", got, want)
	}
	for i, want := range src.samples {
		off := (leadInFrames + i) * otoBytesPerFrame
		got := bitconv.ReadInt16LE(pcm[off:])
		if got != want {
			t.Errorf("frame %d sample = %d, want %d", i, got, want)
		}
		// Stereo: second channel matches first.
		got2 := bitconv.ReadInt16LE(pcm[off+2:])
		if got2 != want {
			t.Errorf("frame %d right channel = %d, want %d", i, got2, want)
		}
	}
}

func TestRenderPCM_OctaveUp(t *testing.T) {
	// One octave up halves the number of audio frames (step = 2). The
	// lead-in is independent of pitch.
	src := &source{
		samples:    make([]int16, 100),
		sampleRate: otoSampleRate,
		rootNote:   60,
	}
	pcm := renderPCM(src, 72)
	leadInFrames := leadInMs * otoSampleRate / 1000
	frames := len(pcm) / otoBytesPerFrame
	if got, want := frames, leadInFrames+50; got != want {
		t.Errorf("octave up frame count = %d, want %d (lead-in + audio)", got, want)
	}
}

// TestDecodeSource_FZFLikeAbsolutePointers pins the audition artifact
// fix: in a multi-voice FZF, a voice's wave_start / gen_* pointers are
// absolute sample indices into the bank's shared audio area, not
// 0-relative offsets within the voice's own slice. A previous version
// of decodeSource subtracted wave_start from gen_*, which made every
// audition read from the very start of the audio area (i.e. voice 1)
// regardless of which voice the user selected. The symptom is a click
// or pop or non-PCM data at note-on for any voice past the first.
//
// The fixture mimics voice 2 of a hypothetical two-voice bank:
//   - voice 1 occupies samples 0..64 in the shared audio area
//   - voice 2 occupies samples 64..128 in the same area
//   - voice 2's header carries wave_start=64, wave_end=128,
//     gen_start=64, gen_end=128
//
// The audio area is a ramp where audio[i] == i. We audition voice 2
// and assert the decoded source matches samples 64..127 of the ramp,
// not samples 0..63.
func TestDecodeSource_FZFLikeAbsolutePointers(t *testing.T) {
	const voice1Samples = 64
	const voice2Samples = 64
	const totalSamples = voice1Samples + voice2Samples

	// Build voice 2's header with absolute pointers into the shared
	// audio area.
	voice2 := make([]byte, disk.VoiceHeaderUsed)
	binary.LittleEndian.PutUint32(voice2[disk.VoiceWaveStartOffset:], voice1Samples)
	binary.LittleEndian.PutUint32(voice2[disk.VoiceWaveEndOffset:], totalSamples)
	binary.LittleEndian.PutUint32(voice2[disk.VoiceGenStartOffset:], voice1Samples)
	binary.LittleEndian.PutUint32(voice2[disk.VoiceGenEndOffset:], totalSamples)
	voice2[disk.VoiceSampOffset] = 0 // 36 kHz
	voice2[disk.VoiceKeyCentOffset] = 60

	// The shared audio area is a ramp i -> i so any wrong offset
	// surfaces as the wrong sample value.
	audioArea := make([]byte, totalSamples*disk.BytesPerSample)
	for i := 0; i < totalSamples; i++ {
		bitconv.WriteInt16LE(audioArea[i*disk.BytesPerSample:], int16(i))
	}

	src, err := decodeSource(voice2, audioArea)
	if err != nil {
		t.Fatalf("decodeSource: %v", err)
	}
	if got, want := len(src.samples), voice2Samples; got != want {
		t.Fatalf("len(samples) = %d, want %d", got, want)
	}
	// Each decoded sample should equal voice1Samples + i, NOT i.
	for i, s := range src.samples {
		want := int16(voice1Samples + i)
		if s != want {
			t.Fatalf("sample %d = %d, want %d (regression: reading voice 1 instead of voice 2)",
				i, s, want)
		}
	}
}

// TestRenderPCM_PrependsLeadInSilence pins the USB-DAC artifact fix:
// the first leadInMs of output must be exactly zero stereo frames so
// the device powers up before the actual sample starts. A regression
// here returns the click-and-pop the user reported during audition.
func TestRenderPCM_PrependsLeadInSilence(t *testing.T) {
	src := &source{
		samples:    []int16{32767, -32768, 32767, -32768}, // max-amplitude transients
		sampleRate: otoSampleRate,
		rootNote:   60,
	}
	pcm := renderPCM(src, 60)
	leadInFrames := leadInMs * otoSampleRate / 1000
	leadInBytes := leadInFrames * otoBytesPerFrame
	if len(pcm) < leadInBytes {
		t.Fatalf("len(pcm) = %d, want at least %d for lead-in", len(pcm), leadInBytes)
	}
	for i := 0; i < leadInBytes; i++ {
		if pcm[i] != 0 {
			t.Fatalf("lead-in byte %d = %#x, want 0 (silent)", i, pcm[i])
		}
	}
	// First audio frame after the lead-in is the loudest sample;
	// confirm it survived the prepend untouched.
	off := leadInFrames * otoBytesPerFrame
	if got := bitconv.ReadInt16LE(pcm[off:]); got != 32767 {
		t.Errorf("first post-lead-in sample = %d, want 32767", got)
	}
}

func TestAudition_RejectsEmptyVoiceID(t *testing.T) {
	e, _ := newTestEngine()
	if err := e.Audition("", []byte{}, []byte{}, 60); err == nil {
		t.Fatalf("expected error for empty VoiceID")
	}
}

func TestEngine_StartsPlayback(t *testing.T) {
	e, ff := newTestEngine()
	voice, fzf := buildVoice(t, 32, 0, 60)
	if err := e.Audition("a", voice, fzf, 60); err != nil {
		t.Fatalf("Audition: %v", err)
	}
	if got := e.CurrentVoiceID(); got != "a" {
		t.Errorf("CurrentVoiceID = %q, want %q", got, "a")
	}
	if !e.IsPlaying() {
		t.Errorf("IsPlaying = false, want true after Audition")
	}
	// Audition spins up the player on a goroutine; wait briefly for it
	// to actually mint one before asserting the call count.
	waitFor(t, func() bool { return atomic.LoadInt32(&ff.created) == 1 }, "player not minted")
	e.Stop()
}

func TestEngine_ToggleOff(t *testing.T) {
	e, _ := newTestEngine()
	voice, fzf := buildVoice(t, 32, 0, 60)

	if err := e.Audition("a", voice, fzf, 60); err != nil {
		t.Fatalf("first Audition: %v", err)
	}
	if got := e.CurrentVoiceID(); got != "a" {
		t.Fatalf("after first call CurrentVoiceID = %q, want %q", got, "a")
	}

	// Second call with the same VoiceID toggles off; no new playback.
	if err := e.Audition("a", voice, fzf, 60); err != nil {
		t.Fatalf("toggle Audition: %v", err)
	}
	if got := e.CurrentVoiceID(); got != "" {
		t.Errorf("after toggle CurrentVoiceID = %q, want empty", got)
	}
	if e.IsPlaying() {
		t.Errorf("IsPlaying = true after toggle-off, want false")
	}
}

func TestEngine_SwitchVoice(t *testing.T) {
	e, ff := newTestEngine()
	voice, fzf := buildVoice(t, 32, 0, 60)

	if err := e.Audition("a", voice, fzf, 60); err != nil {
		t.Fatalf("Audition a: %v", err)
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&ff.created) == 1 }, "first player not minted")
	if err := e.Audition("b", voice, fzf, 60); err != nil {
		t.Fatalf("Audition b: %v", err)
	}
	if got := e.CurrentVoiceID(); got != "b" {
		t.Errorf("after switch CurrentVoiceID = %q, want %q", got, "b")
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&ff.created) == 2 }, "second player not minted")
	e.Stop()
}

func TestEngine_Stop(t *testing.T) {
	e, _ := newTestEngine()
	voice, fzf := buildVoice(t, 32, 0, 60)
	if err := e.Audition("a", voice, fzf, 60); err != nil {
		t.Fatalf("Audition: %v", err)
	}
	e.Stop()
	if e.IsPlaying() {
		t.Errorf("IsPlaying = true after Stop, want false")
	}
	if got := e.CurrentVoiceID(); got != "" {
		t.Errorf("CurrentVoiceID after Stop = %q, want empty", got)
	}
	// Stopping a second time is a no-op.
	e.Stop()
}

func TestEngine_NaturalCompletionClearsState(t *testing.T) {
	e, ff := newTestEngine()
	voice, fzf := buildVoice(t, 32, 0, 60)
	if err := e.Audition("a", voice, fzf, 60); err != nil {
		t.Fatalf("Audition: %v", err)
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&ff.created) == 1 }, "player not minted")
	// The fake player drains itself after ~50ms; the engine goroutine
	// should clear in-flight state once draining is observed.
	waitFor(t, func() bool { return !e.IsPlaying() }, "player did not drain")
	if got := e.CurrentVoiceID(); got != "" {
		t.Errorf("CurrentVoiceID after natural drain = %q, want empty", got)
	}
}

// TestAudition_RealOto exercises the package-level Audition function
// against the real oto backend. Skipped under -short or when the host
// has no audio device.
func TestAudition_RealOto(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-oto audition under -short")
	}
	// Reset the package-level engine so this test doesn't observe state
	// from earlier tests in the same binary run. Skip under -short
	// remains the primary guard for CI without audio output.
	defer Stop()
	voice, fzf := buildVoice(t, 256, 0, 60)
	if err := Audition("real", voice, fzf, 60); err != nil {
		t.Skipf("oto unavailable on this host: %v", err)
	}
	if got := CurrentVoiceID(); got != "real" {
		t.Errorf("CurrentVoiceID = %q, want %q", got, "real")
	}
	if !IsPlaying() {
		t.Errorf("IsPlaying = false after real Audition")
	}
	Stop()
	if IsPlaying() {
		t.Errorf("IsPlaying = true after Stop")
	}
}
