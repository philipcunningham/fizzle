//go:build darwin || windows

package audio

import (
	"io"

	"github.com/ebitengine/oto/v3"
)

// newPlatformPlayerFactory opens a real oto context (CoreAudio on
// macOS, WASAPI on Windows) and returns a factory wrapping
// oto.Context.NewPlayer. oto allows a single context per process, so
// the engine calls this at most once.
func newPlatformPlayerFactory() (playerFactory, error) {
	c, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   otoSampleRate,
		ChannelCount: otoChannelCount,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		return nil, err
	}
	<-ready
	return func(r io.Reader) playerHandle {
		return c.NewPlayer(r)
	}, nil
}
