//go:build linux

package audioplayer

import (
	"context"
	"os/exec"
	"sync"
)

var players = []struct {
	name string
	args func(path string) []string
}{
	{"paplay", func(path string) []string { return []string{path} }},
	{"aplay", func(path string) []string { return []string{path} }},
	{"ffplay", func(path string) []string {
		return []string{"-nodisp", "-autoexit", "-loglevel", "quiet", path}
	}},
}

type execPlayer struct {
	once     sync.Once
	detected string
}

func newPlatformPlayer() Player {
	return &execPlayer{}
}

func (p *execPlayer) detect() {
	for _, player := range players {
		if _, err := exec.LookPath(player.name); err == nil {
			p.detected = player.name
			return
		}
	}
}

func (p *execPlayer) Available() bool {
	p.once.Do(p.detect)
	return p.detected != ""
}

func (p *execPlayer) PlayWAV(ctx context.Context, path string) error {
	p.once.Do(p.detect)
	if p.detected == "" {
		return ErrNoPlayer
	}
	var args []string
	for _, player := range players {
		if player.name == p.detected {
			args = player.args(path)
			break
		}
	}
	return exec.CommandContext(ctx, p.detected, args...).Run() //nolint:gosec // player name is from a fixed allowlist
}
