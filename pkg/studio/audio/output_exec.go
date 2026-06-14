//go:build !darwin && !windows

package audio

import (
	"errors"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
)

// errNoSystemPlayer is returned when no supported system audio player
// is on PATH. Surfaced through engine.init so the TUI can show a status
// line rather than failing silently.
var errNoSystemPlayer = errors.New(
	"studio/audio: no system audio player found (install pulseaudio, alsa-utils, or ffmpeg)")

// newPlatformPlayerFactory selects a system audio player and returns a
// factory that streams the engine's raw PCM to it over stdin. The
// non-macOS/Windows build deliberately avoids oto's cgo ALSA dependency
// (mirroring pkg/audioplayer), so audio output here uses the same
// paplay / aplay / ffplay set audioplayer uses. The engine's PCM is
// signed 16-bit little-endian, stereo, at otoSampleRate Hz.
func newPlatformPlayerFactory() (playerFactory, error) {
	name, args, ok := findSystemPlayer()
	if !ok {
		return nil, errNoSystemPlayer
	}
	return func(r io.Reader) playerHandle {
		return newExecPlayer(name, args, r)
	}, nil
}

// findSystemPlayer returns the command and stdin raw-PCM arguments for
// the first available player, preferring paplay, then aplay, then
// ffplay (matching pkg/audioplayer's ordering).
func findSystemPlayer() (string, []string, bool) {
	rate := strconv.Itoa(otoSampleRate)
	chans := strconv.Itoa(otoChannelCount)
	candidates := []struct {
		name string
		args []string
	}{
		{"paplay", []string{"--raw", "--format=s16le", "--rate=" + rate, "--channels=" + chans}},
		{"aplay", []string{"-q", "-t", "raw", "-f", "S16_LE", "-r", rate, "-c", chans}},
		{"ffplay", []string{"-loglevel", "quiet", "-nodisp", "-autoexit", "-f", "s16le", "-ar", rate, "-ac", chans, "-i", "-"}},
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c.name); err == nil {
			return c.name, c.args, true
		}
	}
	return "", nil, false
}

// execPlayer adapts an external player process to playerHandle. The
// process reads PCM from stdin and exits when the stream is exhausted
// (natural completion) or when Pause kills it. It implements the same
// Play / IsPlaying / Pause contract oto.Player provides.
type execPlayer struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	started bool
	done    atomic.Bool
}

func newExecPlayer(name string, args []string, r io.Reader) *execPlayer {
	cmd := exec.Command(name, args...) //nolint:gosec // name is from the fixed allowlist in findSystemPlayer
	cmd.Stdin = r
	return &execPlayer{cmd: cmd}
}

func (p *execPlayer) Play() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return
	}
	p.started = true
	if err := p.cmd.Start(); err != nil {
		p.done.Store(true)
		return
	}
	go func() {
		_ = p.cmd.Wait()
		p.done.Store(true)
	}()
}

func (p *execPlayer) IsPlaying() bool {
	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	return started && !p.done.Load()
}

func (p *execPlayer) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started && !p.done.Load() && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}
