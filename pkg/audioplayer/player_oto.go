//go:build darwin || windows

package audioplayer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"

	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

const (
	otoChannelCount = 1
	// otoSampleRate matches the FZ-1 default rate (36 kHz) so default-quality
	// voices skip resampling on the way to the device. oto requires the rate
	// to be fixed at context creation; FZ voices at 18 kHz and 9 kHz are
	// upsampled by fzutil.Resample before playback.
	otoSampleRate   = 36000
	otoPollInterval = 5 * time.Millisecond
)

type otoPlayer struct {
	once sync.Once
	ctx  *oto.Context
	err  error
}

var defaultOtoPlayer = &otoPlayer{}

func newPlatformPlayer() Player {
	return defaultOtoPlayer
}

func (p *otoPlayer) init() {
	p.once.Do(func() {
		c, ready, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate:   otoSampleRate,
			ChannelCount: otoChannelCount,
			Format:       oto.FormatSignedInt16LE,
		})
		if err != nil {
			p.err = fmt.Errorf("audioplayer: initialising audio context: %w", err)
			return
		}
		<-ready
		p.ctx = c
	})
}

func (p *otoPlayer) Available() bool {
	return true
}

func (p *otoPlayer) PlayWAV(ctx context.Context, path string) error {
	p.init()
	if p.err != nil {
		return p.err
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("audioplayer: opening %q: %w", path, err)
	}
	wf, err := wav.Read(f)
	f.Close() //nolint:errcheck
	if err != nil {
		return fmt.Errorf("audioplayer: reading %q: %w", path, err)
	}

	samples, err := fzutil.Resample(wf, otoSampleRate)
	if err != nil {
		return fmt.Errorf("audioplayer: resampling: %w", err)
	}
	pcm := samplesToBytes(samples)
	// oto.Player resources are reclaimed by the runtime when player is
	// unreachable (oto v3.4 deprecated explicit Close); the variable goes
	// out of scope on function return, so no manual cleanup is needed.
	player := p.ctx.NewPlayer(bytes.NewReader(pcm))
	player.Play()

	for player.IsPlaying() {
		select {
		case <-ctx.Done():
			player.Pause()
			return ctx.Err()
		default:
			time.Sleep(otoPollInterval)
		}
	}
	if err := player.Err(); err != nil {
		return fmt.Errorf("audioplayer: playback error: %w", err)
	}
	return nil
}

func samplesToBytes(samples []int16) []byte {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		bitconv.WriteInt16LE(buf[i*2:], s)
	}
	return buf
}
