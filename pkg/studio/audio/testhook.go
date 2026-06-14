package audio

import (
	"io"
	"testing"
)

// InstallNoopForTest replaces the default audition engine with one
// that uses a non-blocking no-op player, then restores the prior
// engine on t.Cleanup. Intended for tests in callers (e.g. the
// studio app's snapshot tests) that exercise the audition UI but
// have no real audio backend available, such as Linux CI runners
// with no pulseaudio / alsa / ffmpeg installed.
//
// Without this, Audition fails immediately on those environments
// and the rendered status text reads "Audition failed: ..." rather
// than the success-path "Auditioning ..." captured in the snapshot.
func InstallNoopForTest(t testing.TB) {
	t.Helper()
	prev := defaultEngine
	defaultEngine = &engine{makeFn: func(io.Reader) playerHandle { return noopPlayer{} }}
	t.Cleanup(func() {
		defaultEngine.Stop()
		defaultEngine = prev
	})
}

type noopPlayer struct{}

func (noopPlayer) Play()           {}
func (noopPlayer) IsPlaying() bool { return false }
func (noopPlayer) Pause()          {}
