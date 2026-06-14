// Package audio provides the studio TUI's audition path: play a voice's
// raw sample at a chosen MIDI pitch as a sanity check. There is no
// envelope, LFO, or filter shaping. The package allows a single
// in-flight playback at a time and exposes Audition / Stop /
// IsPlaying / CurrentVoiceID for the TUI to coordinate against.
//
// Initialisation of the platform audio backend is lazy and happens on
// the first Audition call. On macOS / Windows this opens an oto context
// (cached for the process lifetime, as oto requires); on Linux and
// other platforms it selects a system audio player. The backend split
// lives in output_oto.go and output_exec.go so the Linux build does not
// link oto's cgo ALSA dependency.
package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
)

// VoiceID uniquely identifies the voice being auditioned. The engine
// uses it to decide whether a new Audition call cancels the in-flight
// playback (different voice) or stops it (same voice, toggle-off).
type VoiceID string

const (
	// otoSampleRate matches audioplayer's choice so default-quality FZ
	// voices (36 kHz) skip resampling on the way to the device. oto fixes
	// the rate at context creation; voices at 18 kHz / 9 kHz are pitched
	// up by the playback step. Stereo so callers don't have to think
	// about channel duplication later.
	otoSampleRate    = 36000
	otoChannelCount  = 2
	otoBytesPerFrame = otoChannelCount * 2 // stereo, int16

	// otoPollInterval mirrors pkg/audioplayer; balances responsiveness
	// against wasted CPU when waiting for playback to drain.
	otoPollInterval = 5 * time.Millisecond

	// leadInMs prepends silence to compensate for USB DAC startup
	// latency. Matches pkg/audioplayer.LeadInMs so studio's audition
	// path produces the same pre-roll the studio1 path does. Without
	// this the opening transient of the voice gets clipped by the DAC
	// power-up ramp, producing the click-and-pop that the user reports.
	leadInMs = 500

	// semitonesPerOctave is the equal-temperament step count used to
	// convert MIDI semitone deltas into a frequency ratio.
	semitonesPerOctave = 12
)

// ErrVoiceMalformed is returned by Audition when the voice header bytes
// cannot be interpreted (truncated, bad sample-rate index, gen range
// outside available audio). Surfaced so the TUI can show a status line
// rather than fail silently.
var ErrVoiceMalformed = errors.New("studio/audio: malformed voice bytes")

// playerHandle is the small subset of *oto.Player the engine actually
// uses. Keeping it as an interface lets tests inject a fake without
// opening a real audio device.
type playerHandle interface {
	Play()
	IsPlaying() bool
	Pause()
}

// playerFactory constructs a playerHandle backed by a stream of PCM
// bytes. Production wraps oto.Context.NewPlayer; tests substitute a
// fake that returns immediately.
type playerFactory func(io.Reader) playerHandle

// engine owns the cached oto context (when in production) and the
// in-flight playback bookkeeping. There is exactly one default engine
// per process because oto allows only one context per process; tests
// instantiate ad-hoc engines with a fake factory so they can verify
// the in-flight rules without touching audio hardware.
type engine struct {
	initOnce sync.Once
	initErr  error
	makeFn   playerFactory // set on first init or by tests directly

	mu              sync.Mutex
	currentVoiceID  VoiceID
	currentCancel   context.CancelFunc
	currentFinished chan struct{}
}

var defaultEngine = &engine{}

// platformPlayerFactory is a hook so tests of the package-level API can
// substitute a fake. Production resolves it to the per-OS
// newPlatformPlayerFactory: an oto-backed context on macOS / Windows
// (output_oto.go) and a system-player exec backend elsewhere
// (output_exec.go).
var platformPlayerFactory = newPlatformPlayerFactory

func (e *engine) init() error {
	e.initOnce.Do(func() {
		if e.makeFn != nil {
			// Test-installed factory: do not call the real oto path.
			return
		}
		f, err := platformPlayerFactory()
		if err != nil {
			e.initErr = fmt.Errorf("studio/audio: initialising audio context: %w", err)
			return
		}
		e.makeFn = f
	})
	return e.initErr
}

// Audition plays the voice at the given MIDI note. See the package
// docstring for the in-flight rule. The audio stream runs
// asynchronously; the returned error reflects only synchronous setup
// failure (context init or voice parsing).
//
// audioArea is the byte slice the voice header's wave/gen pointers
// index into. For an FZV (single-voice) container that is
// fzv[disk.SectorSize:]. For an FZF (full dump) it is the bytes
// starting at VoiceAreaStart + voiceSectors*SectorSize: callers
// typically derive this from fzutil.FZFHeader.
func Audition(voiceID VoiceID, voiceBytes, audioArea []byte, pitch int) error {
	return defaultEngine.Audition(voiceID, voiceBytes, audioArea, pitch)
}

// Stop cancels any in-flight playback. Safe to call when nothing is
// playing.
func Stop() {
	defaultEngine.Stop()
}

// IsPlaying reports whether a playback is currently in flight.
func IsPlaying() bool {
	return defaultEngine.IsPlaying()
}

// CurrentVoiceID returns the VoiceID of the in-flight playback, or
// "" if none.
func CurrentVoiceID() VoiceID {
	return defaultEngine.CurrentVoiceID()
}

// Audition is the engine-scoped implementation. Tests instantiate a
// throwaway engine to avoid stomping on global state.
func (e *engine) Audition(voiceID VoiceID, voiceBytes, audioArea []byte, pitch int) error {
	if voiceID == "" {
		return errors.New("studio/audio: voiceID must be non-empty")
	}
	src, err := decodeSource(voiceBytes, audioArea)
	if err != nil {
		return err
	}
	if err := e.init(); err != nil {
		return err
	}

	// Take the lock once for the toggle/switch decision so two
	// concurrent Audition calls cannot both decide "no in-flight
	// playback" and start two streams.
	e.mu.Lock()
	if e.currentVoiceID != "" {
		// Cancel whatever is playing. If it's the same voice, toggle-off
		// semantics return after stop with no new playback.
		toggleOff := e.currentVoiceID == voiceID
		cancel := e.currentCancel
		finished := e.currentFinished
		e.currentVoiceID = ""
		e.currentCancel = nil
		e.currentFinished = nil
		e.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if finished != nil {
			<-finished
		}
		if toggleOff {
			return nil
		}
		e.mu.Lock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	e.currentVoiceID = voiceID
	e.currentCancel = cancel
	e.currentFinished = finished
	e.mu.Unlock()

	go e.run(ctx, voiceID, src, pitch, finished)
	return nil
}

func (e *engine) Stop() {
	e.mu.Lock()
	cancel := e.currentCancel
	finished := e.currentFinished
	e.currentVoiceID = ""
	e.currentCancel = nil
	e.currentFinished = nil
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if finished != nil {
		<-finished
	}
}

func (e *engine) IsPlaying() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentVoiceID != ""
}

func (e *engine) CurrentVoiceID() VoiceID {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentVoiceID
}

// run is the goroutine that drives a single playback. It exits when
// the source is exhausted, the oto player drains, or ctx is cancelled.
// Always closes finished and (if still owner) clears the engine's
// in-flight state so IsPlaying flips back to false on natural
// completion.
func (e *engine) run(ctx context.Context, voiceID VoiceID, src *source, pitch int, finished chan struct{}) {
	defer close(finished)
	defer func() {
		// Only clear engine state if we're still the owner; a concurrent
		// Audition or Stop may have taken ownership already and started
		// its own goroutine.
		e.mu.Lock()
		if e.currentVoiceID == voiceID && e.currentFinished == finished {
			e.currentVoiceID = ""
			e.currentCancel = nil
			e.currentFinished = nil
		}
		e.mu.Unlock()
	}()

	pcm := renderPCM(src, pitch)
	player := e.makeFn(bytes.NewReader(pcm))
	player.Play()
	for player.IsPlaying() {
		select {
		case <-ctx.Done():
			player.Pause()
			return
		default:
			time.Sleep(otoPollInterval)
		}
	}
}

// source is the decoded voice information needed to drive playback.
// All fields are normalised: samples is the raw int16 PCM in playback
// order (genStart to genEnd), sampleRate is the recorded rate in Hz,
// rootNote is the MIDI note the recording is in tune at.
type source struct {
	samples    []int16
	sampleRate uint32
	rootNote   uint8
}

// decodeSource extracts the gen-range samples and pitch metadata from
// the voice header. audioArea is the byte slice the header's wave/gen
// pointers index into; the caller is responsible for slicing the
// container down to that region (FZV: fzv[SectorSize:]; FZF:
// data[VoiceAreaStart + voiceSectors*SectorSize:]). gen* are absolute
// sample indices within audioArea, so byte offsets are gen* *
// disk.BytesPerSample with no further normalisation.
func decodeSource(voiceBytes, audioArea []byte) (*source, error) {
	if len(voiceBytes) < disk.VoiceHeaderUsed {
		return nil, fmt.Errorf("%w: voice header is %d bytes, need at least %d", ErrVoiceMalformed, len(voiceBytes), disk.VoiceHeaderUsed)
	}
	sampIdx := voiceBytes[disk.VoiceSampOffset]
	if int(sampIdx) >= disk.NumSampleRates() {
		return nil, fmt.Errorf("%w: invalid sample rate index %d", ErrVoiceMalformed, sampIdx)
	}
	sampleRate := disk.SampleRate(sampIdx)

	// KeyCent == 0 is the FZ's literal C-1 (MIDI 0), not an "unset"
	// sentinel; applying a fallback here used to transpose every
	// C-1-rooted voice (e.g. AMEN slices) down 5 octaves, producing
	// the obvious wrong-pitch distortion the user heard. Trust the
	// header byte as-is.
	rootNote := voiceBytes[disk.VoiceKeyCentOffset]

	genStart := binary.LittleEndian.Uint32(voiceBytes[disk.VoiceGenStartOffset : disk.VoiceGenStartOffset+4])
	genEnd := binary.LittleEndian.Uint32(voiceBytes[disk.VoiceGenEndOffset : disk.VoiceGenEndOffset+4])
	waveEnd := binary.LittleEndian.Uint32(voiceBytes[disk.VoiceWaveEndOffset : disk.VoiceWaveEndOffset+4])

	// Establish a sample-count upper bound from the available audio.
	maxSamples := bitconv.NarrowU32(len(audioArea) / disk.BytesPerSample)

	// Default genEnd to the wave end (or full audio) when the header
	// leaves it zero. This matches voiceextract.DecodePlaybackRange.
	if genEnd == 0 {
		if waveEnd > 0 {
			genEnd = waveEnd
		} else {
			genEnd = maxSamples
		}
	}
	if genStart > maxSamples {
		genStart = maxSamples
	}
	if genEnd > maxSamples {
		genEnd = maxSamples
	}
	if genEnd <= genStart {
		return nil, fmt.Errorf("%w: empty gen range (%d-%d) in %d available samples", ErrVoiceMalformed, genStart, genEnd, maxSamples)
	}

	n := genEnd - genStart
	samples := make([]int16, n)
	for i := uint32(0); i < n; i++ {
		off := (genStart + i) * disk.BytesPerSample
		samples[i] = bitconv.ReadInt16LE(audioArea[off : off+disk.BytesPerSample])
	}
	return &source{samples: samples, sampleRate: sampleRate, rootNote: rootNote}, nil
}

// renderPCM resamples src.samples to oto's fixed playback rate while
// applying the MIDI pitch offset. The algorithm is nearest-neighbour:
// each output frame picks the source sample at index round(outIdx *
// step), where step combines the rate ratio (src.sampleRate /
// otoSampleRate) and the pitch ratio (2^((pitch - rootNote) / 12)).
//
// Output is stereo (both channels carry the same int16 sample) in
// little-endian PCM, matching the oto context format.
func renderPCM(src *source, pitch int) []byte {
	if len(src.samples) == 0 {
		return nil
	}
	semitones := pitch - int(src.rootNote)
	pitchRatio := math.Pow(2, float64(semitones)/float64(semitonesPerOctave))
	step := (float64(src.sampleRate) / float64(otoSampleRate)) * pitchRatio
	if step <= 0 || math.IsNaN(step) || math.IsInf(step, 0) {
		// Defensive: a degenerate pitch should not produce an infinite
		// loop. Treat as silent.
		return nil
	}

	// Compute the number of output frames as ceil(len/step) so we play
	// the entire gen range without dropping the tail.
	outFrames := int(math.Ceil(float64(len(src.samples)) / step))
	if outFrames <= 0 {
		return nil
	}
	leadInFrames := leadInMs * otoSampleRate / 1000
	buf := make([]byte, (leadInFrames+outFrames)*otoBytesPerFrame)
	srcLen := len(src.samples)
	for i := 0; i < outFrames; i++ {
		idx := int(math.Round(float64(i) * step))
		if idx >= srcLen {
			break
		}
		s := src.samples[idx]
		off := (leadInFrames + i) * otoBytesPerFrame
		// Stereo: same sample on both channels.
		bitconv.WriteInt16LE(buf[off:], s)
		bitconv.WriteInt16LE(buf[off+2:], s)
	}
	return buf
}
