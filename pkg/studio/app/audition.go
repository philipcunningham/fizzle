package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/philipcunningham/fizzle/pkg/audioplayer"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
)

// toggleAudition is the Space-key entry point. A first press starts
// playback of the currently-focused voice; a second press cancels it.
// The audio backend runs in a goroutine and updates the status line via
// QueueUpdateDraw on completion (spec §9.4 threading rule).
//
// If the focused widget is an InputField the caller must short-circuit
// this, because Space is also a text-entry key. The app-level key handler
// performs that check before invoking toggleAudition.
func (a *App) toggleAudition() {
	if !a.canPlay {
		a.status.SetWarning("Audio playback not available on this platform")
		return
	}
	// Second Space press: stop and clear.
	if a.hasActivePlayback() {
		a.stopPlayback()
		a.status.SetInfo("Audition stopped")
		return
	}

	slot := a.currentVoiceSlot()
	fzvBytes, err := a.m.VoiceFZVBytes(slot)
	if err != nil {
		if errors.Is(err, model.ErrAuditionVoiceMissing) {
			a.status.SetWarning(fmt.Sprintf("Voice %d has no audio (placeholder or multi-disk)", slot+1))
			return
		}
		a.status.SetError(fmt.Sprintf("Audition: %v", err))
		return
	}

	// Write FZV + WAV into the tmpDir so voiceextract.ExtractPlayback can
	// do its work. The files are cleaned up at app exit; per-audition
	// cleanup is not necessary because tmpDir is small and bounded.
	unique := time.Now().UnixNano()
	fzvPath := a.tempFile(fmt.Sprintf("audition-%d.fzv", unique))
	if err := os.WriteFile(fzvPath, fzvBytes, 0o644); err != nil {
		a.status.SetError(fmt.Sprintf("Audition: writing fzv: %v", err))
		return
	}
	wavPath := a.tempFile(fmt.Sprintf("audition-%d.wav", unique))
	if err := voiceextract.ExtractPlayback(fzvPath, wavPath, audioplayer.LeadInMs); err != nil {
		a.status.SetError(fmt.Sprintf("Audition: extracting wav: %v", err))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.playCancelMu.Lock()
	a.playCancel = cancel
	a.playGen++
	gen := a.playGen
	a.playCancelMu.Unlock()

	a.status.SetInfo(fmt.Sprintf("Auditioning voice %d (Space to stop)", slot+1))

	go func() {
		_ = a.player.PlayWAV(ctx, wavPath)
		// Mark playback finished (only if we're still the active gen;
		// a later audition may have superseded us).
		a.tApp.QueueUpdateDraw(func() {
			a.playCancelMu.Lock()
			if a.playGen == gen {
				a.playCancel = nil
			}
			a.playCancelMu.Unlock()
		})
	}()
}

// hasActivePlayback reports whether a playback goroutine is currently
// running.
func (a *App) hasActivePlayback() bool {
	a.playCancelMu.Lock()
	defer a.playCancelMu.Unlock()
	return a.playCancel != nil
}

// stopPlayback cancels any in-flight audition. Idempotent.
func (a *App) stopPlayback() {
	a.playCancelMu.Lock()
	defer a.playCancelMu.Unlock()
	if a.playCancel != nil {
		a.playCancel()
		a.playCancel = nil
	}
}
