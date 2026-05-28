package audioplayer

import (
	"context"
	"sync"
)

// TestPlayer is a Player implementation for use in tests. It records
// PlayWAV calls and returns a configurable error. Use NewTestPlayer to
// create one.
type TestPlayer struct {
	mu    sync.Mutex
	avail bool
	calls []string
	err   error
}

// NewTestPlayer creates a TestPlayer with the given availability.
// PlayWAV calls succeed by default; use SetError to inject failures.
func NewTestPlayer(available bool) *TestPlayer {
	return &TestPlayer{avail: available}
}

// SetError configures the error returned by subsequent PlayWAV calls.
func (p *TestPlayer) SetError(err error) {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
}

// Available reports whether this player is available.
func (p *TestPlayer) Available() bool { return p.avail }

// PlayWAV records the path and returns the configured error.
func (p *TestPlayer) PlayWAV(_ context.Context, path string) error {
	p.mu.Lock()
	p.calls = append(p.calls, path)
	err := p.err
	p.mu.Unlock()
	return err
}

// Calls returns a copy of the recorded PlayWAV paths.
func (p *TestPlayer) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.calls))
	copy(out, p.calls)
	return out
}

// Reset clears the recorded calls.
func (p *TestPlayer) Reset() {
	p.mu.Lock()
	p.calls = nil
	p.mu.Unlock()
}
