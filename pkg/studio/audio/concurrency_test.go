package audio

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEngine_ConcurrentMixedOps_RaceSafe stresses the engine with
// many goroutines calling Audition/Stop concurrently. The Go race
// detector is the primary assertion: a mutex slip on the engine's
// currentVoiceID / currentCancel / currentFinished fields would
// surface here.
//
// Secondary assertions:
//   - After draining (one final Stop), engine state is consistent:
//     IsPlaying()==false and CurrentVoiceID()=="".
//   - No goroutine leak: NumGoroutine after the test (+ a generous
//     settle window) is within a small delta of the baseline. The
//     delta accounts for runtime jitter; a real leak shows up as
//     N goroutines per Audition call.
//
// Run under `-race` for full value: this test passing without -race
// is a weak signal.
func TestEngine_ConcurrentMixedOps_RaceSafe(t *testing.T) {
	e, _ := newTestEngine()
	voice, fzf := buildVoice(t, 32, 0, 60)

	const goroutines = 16
	const opsPerGoroutine = 50

	// Snapshot goroutine count before launching workers.
	baseline := runtime.NumGoroutine()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				// Mix Audition and Stop calls; voice IDs rotate through
				// a small set so toggle / switch / start paths all fire.
				switch (gid + i) % 4 {
				case 0:
					_ = e.Audition("voice-a", voice, fzf, 60)
				case 1:
					_ = e.Audition("voice-b", voice, fzf, 62)
				case 2:
					_ = e.Audition("voice-c", voice, fzf, 64)
				case 3:
					e.Stop()
				}
			}
		}(g)
	}
	wg.Wait()

	// Drain anything still playing.
	e.Stop()

	// Engine state must be self-consistent after the storm settles.
	if e.IsPlaying() {
		t.Errorf("engine still playing after Stop: voiceID=%q", e.CurrentVoiceID())
	}
	if got := e.CurrentVoiceID(); got != "" {
		t.Errorf("CurrentVoiceID after Stop = %q, want empty", got)
	}

	// Goroutine leak check: give the runtime a moment to retire any
	// straggler player goroutines, then compare against baseline.
	// Allow some slack: Go internals, the test runtime, and the
	// race-detector can each leave a small residue.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if leak := runtime.NumGoroutine() - baseline; leak > 2 {
		// Dump goroutine count info; the actual stacks would be
		// huge, so we leave that to manual investigation.
		t.Errorf("possible goroutine leak: %d goroutines above baseline (baseline=%d, after=%d)",
			leak, baseline, runtime.NumGoroutine())
	}
}

// TestEngine_StopWhileAuditioning pins Stop's correctness during
// active playback. The defer in run() should not double-clear
// engine state nor leak the context.
func TestEngine_StopWhileAuditioning(t *testing.T) {
	e, _ := newTestEngine()
	voice, fzf := buildVoice(t, 32, 0, 60)

	for i := 0; i < 50; i++ {
		if err := e.Audition("v", voice, fzf, 60); err != nil {
			t.Fatalf("iter %d Audition: %v", i, err)
		}
		// Tight loop: Stop right after Audition. Race window is
		// between Audition returning and the run goroutine starting
		// its poll loop. The mutex must serialise both sides cleanly.
		e.Stop()
		if e.IsPlaying() {
			t.Fatalf("iter %d: still playing after Stop", i)
		}
	}
}

// TestEngine_RestartAfterNaturalCompletion pins the exhaustion-
// vs-restart sequence: voice A plays to natural completion, then
// voice B starts. The defer in run() for A must not clear B's
// state (the owner-identity guard).
//
// This isn't a deterministic race trigger; Go's scheduler decides
// whether A's defer runs before or after B's Audition installs
// state. The test repeats the pattern many times so a broken guard
// shows up as an intermittent failure under -race.
func TestEngine_RestartAfterNaturalCompletion(t *testing.T) {
	e, _ := newTestEngine()
	voice, fzf := buildVoice(t, 32, 0, 60)

	for i := 0; i < 30; i++ {
		// A plays. fakePlayer auto-drains in ~50ms.
		if err := e.Audition("voice-a", voice, fzf, 60); err != nil {
			t.Fatalf("iter %d: Audition A: %v", i, err)
		}
		// Wait for the natural drain.
		waitFor(t, func() bool { return !e.IsPlaying() }, fmt.Sprintf("iter %d: A did not drain", i))

		// Immediately Audition B. If the guard is broken, A's defer
		// (running concurrently with this call's run-goroutine
		// installation) could clear B's state. The guard's identity
		// check prevents that.
		if err := e.Audition("voice-b", voice, fzf, 62); err != nil {
			t.Fatalf("iter %d: Audition B: %v", i, err)
		}

		// B should now be current. The check below tolerates B
		// having already drained naturally (the test exercises the
		// guard, not strict liveness); empty current is also OK as
		// long as it's not the result of A's defer corrupting state.
		got := e.CurrentVoiceID()
		if got != "voice-b" && got != "" {
			t.Fatalf("iter %d: CurrentVoiceID = %q, want voice-b or empty (post-drain)", i, got)
		}

		// Tidy up before the next iteration.
		e.Stop()
	}
}

// TestEngine_RapidToggleSameVoice pins the toggle-off path's
// behaviour under repeated rapid presses of the same voice. Each
// Audition with the same voiceID must either start (when none is
// playing) or toggle off (when that voice is playing), never leak.
func TestEngine_RapidToggleSameVoice(t *testing.T) {
	e, ff := newTestEngine()
	voice, fzf := buildVoice(t, 32, 0, 60)

	for i := 0; i < 100; i++ {
		if err := e.Audition("same", voice, fzf, 60); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	// After 100 toggles, the engine should be in a definite state:
	// the last call either started or toggled-off. Drain whatever
	// is left.
	e.Stop()
	if e.IsPlaying() {
		t.Errorf("rapid-toggle left engine playing after Stop")
	}
	// At least some players were minted. The exact count depends on
	// scheduling; we just assert > 0 as a sanity check.
	if atomic.LoadInt32(&ff.created) == 0 {
		t.Errorf("no players minted across 100 toggles")
	}
}
