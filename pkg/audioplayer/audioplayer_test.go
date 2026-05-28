package audioplayer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestErrNoPlayerSentinel(t *testing.T) {
	t.Parallel()
	if !errors.Is(ErrNoPlayer, ErrNoPlayer) {
		t.Error("ErrNoPlayer should be a sentinel error")
	}
}

func TestNewPlayerReturnsNonNil(t *testing.T) {
	t.Parallel()
	p := NewPlayer()
	if p == nil {
		t.Fatal("NewPlayer returned nil")
	}
}

func TestNewTestPlayerAvailable(t *testing.T) {
	t.Parallel()
	p := NewTestPlayer(true)
	if !p.Available() {
		t.Error("expected Available() == true")
	}
}

func TestNewTestPlayerUnavailable(t *testing.T) {
	t.Parallel()
	p := NewTestPlayer(false)
	if p.Available() {
		t.Error("expected Available() == false")
	}
}

func TestTestPlayerRecordsCalls(t *testing.T) {
	t.Parallel()
	p := NewTestPlayer(true)
	_ = p.PlayWAV(context.Background(), "/a.wav")
	_ = p.PlayWAV(context.Background(), "/b.wav")
	calls := p.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0] != "/a.wav" {
		t.Errorf("call 0: got %q, want /a.wav", calls[0])
	}
	if calls[1] != "/b.wav" {
		t.Errorf("call 1: got %q, want /b.wav", calls[1])
	}
}

func TestTestPlayerReturnsError(t *testing.T) {
	t.Parallel()
	want := errors.New("audio broken")
	p := NewTestPlayer(true)
	p.SetError(want)
	err := p.PlayWAV(context.Background(), "/x.wav")
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
	if len(p.Calls()) != 1 {
		t.Error("call should still be recorded even on error")
	}
}

func TestTestPlayerReset(t *testing.T) {
	t.Parallel()
	p := NewTestPlayer(true)
	_ = p.PlayWAV(context.Background(), "/a.wav")
	p.Reset()
	if len(p.Calls()) != 0 {
		t.Error("expected no calls after Reset")
	}
}

func TestTestPlayerCallsReturnsCopy(t *testing.T) {
	t.Parallel()
	p := NewTestPlayer(true)
	_ = p.PlayWAV(context.Background(), "/a.wav")
	calls := p.Calls()
	calls[0] = "mutated"
	if p.Calls()[0] == "mutated" {
		t.Error("Calls() should return a copy")
	}
}

// TestPlayWAVConcurrent verifies the TestPlayer is safe for concurrent use.
// Run with -race to surface any unsynchronised access to the call log.
func TestPlayWAVConcurrent(t *testing.T) {
	t.Parallel()
	const goroutines = 10
	p := NewTestPlayer(true)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = p.PlayWAV(context.Background(), fmt.Sprintf("/audio%d.wav", id))
		}(i)
	}
	wg.Wait()
	if got := len(p.Calls()); got != goroutines {
		t.Errorf("expected %d calls, got %d", goroutines, got)
	}
}
