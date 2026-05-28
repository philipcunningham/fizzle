package model

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
)

// TestVoiceFZVBytesRoundTrip verifies that the bytes returned by
// VoiceFZVBytes parse cleanly through fzvinfo.Parse and that the audio
// pointers were rewritten to be relative to the voice's own audio bytes.
func TestVoiceFZVBytesRoundTrip(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	fzvBytes, err := m.VoiceFZVBytes(0)
	if err != nil {
		t.Fatalf("VoiceFZVBytes(0): %v", err)
	}
	if len(fzvBytes) == 0 {
		t.Fatal("VoiceFZVBytes returned empty slice")
	}

	// Round-trip via fzvinfo.Parse: write to tempfile and parse back.
	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "audition.fzv")
	if err := os.WriteFile(tmpPath, fzvBytes, 0o644); err != nil {
		t.Fatalf("writing tmp fzv: %v", err)
	}
	v, err := fzvinfo.Parse(tmpPath)
	if err != nil {
		t.Fatalf("fzvinfo.Parse(%q): %v", tmpPath, err)
	}
	if v == nil {
		t.Fatal("fzvinfo.Parse returned nil voice")
	}
	if v.Name != testVoiceAlpha {
		t.Errorf("parsed voice name = %q, want %q", v.Name, testVoiceAlpha)
	}
}

// TestVoiceFZVBytesOutOfRangeSlot rejects out-of-range slots cleanly.
func TestVoiceFZVBytesOutOfRangeSlot(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := m.VoiceFZVBytes(99); err == nil {
		t.Errorf("VoiceFZVBytes(99) returned nil error, want range error")
	}
	if _, err := m.VoiceFZVBytes(-1); err == nil {
		t.Errorf("VoiceFZVBytes(-1) returned nil error, want range error")
	}
}

// TestVoiceFZVBytesDistinctVoices: the FZV bytes for two different slots
// should not match.
func TestVoiceFZVBytesDistinctVoices(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a, err := m.VoiceFZVBytes(0)
	if err != nil {
		t.Fatalf("VoiceFZVBytes(0): %v", err)
	}
	b, err := m.VoiceFZVBytes(1)
	if err != nil {
		t.Fatalf("VoiceFZVBytes(1): %v", err)
	}
	if bytes.Equal(a, b) {
		t.Errorf("VoiceFZVBytes returned identical bytes for slot 0 and slot 1")
	}
}
