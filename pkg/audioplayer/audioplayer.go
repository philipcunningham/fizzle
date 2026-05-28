// Package audioplayer provides cross-platform audio playback for WAV files.
// On macOS and Windows, playback uses native audio APIs via oto/v3 (no
// external tools or CGo required). On Linux, playback shells out to a
// detected system audio player (aplay, paplay, or ffplay).
//
// Use NewPlayer to create a platform-appropriate player. Use TestPlayer
// in tests to verify playback behaviour without audio hardware.
package audioplayer

import (
	"context"
	"errors"
)

// LeadInMs is the number of milliseconds of silence prepended before audio
// playback to compensate for USB DAC startup latency.
const LeadInMs = 500

// ErrNoPlayer is returned by PlayWAV when no supported audio player is
// available on the current platform.
var ErrNoPlayer = errors.New("audioplayer: no supported audio player found")

// Player plays WAV audio files.
type Player interface {
	Available() bool
	PlayWAV(ctx context.Context, path string) error
}

// NewPlayer returns a Player using the platform-appropriate audio backend.
func NewPlayer() Player {
	return newPlatformPlayer()
}
