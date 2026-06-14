package pool

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
)

// fakeVoiceBytes returns a 256-byte slice shaped like a minimal
// voice header: name field set, mode word set to NoSound so
// IsActiveOrEmptyVoiceSlot accepts it. Used to feed Pool tests
// without depending on a real fixture.
func fakeVoiceBytes(name string) []byte {
	b := make([]byte, disk.VoicePackSize)
	for i := 0; i < disk.VoiceNameFieldSize && i < len(name); i++ {
		b[disk.VoiceNameOffset+i] = name[i] //nolint:gosec // G602: VoiceNameOffset+VoiceNameFieldSize <= VoicePackSize by disk-package construction
	}
	for i := len(name); i < disk.VoiceNameFieldSize; i++ {
		b[disk.VoiceNameOffset+i] = ' '
	}
	// PlaybackModeNoSound = 0x0000 (already zero from make).
	return b
}

// TestPool_AddAndRemoveKeepsCursorInRange pins the cursor
// invariant: after any sequence of Add/Remove, m.cursor must be
// either 0 (empty pool) or a valid entry index (0..len-1).
func TestPool_AddAndRemoveKeepsCursorInRange(t *testing.T) {
	m := New()
	if got := m.Cursor(); got != 0 {
		t.Errorf("fresh pool cursor = %d, want 0", got)
	}
	if m.Selected() != nil {
		t.Error("fresh pool Selected() should be nil")
	}

	m.AddFromAreaVoice("VOICE 1", "bank 1", fakeVoiceBytes("VOICE 1"))
	m.AddFromAreaVoice("VOICE 2", "bank 1", fakeVoiceBytes("VOICE 2"))
	m.AddFromAreaVoice("VOICE 3", "bank 1", fakeVoiceBytes("VOICE 3"))
	if got := len(m.Entries()); got != 3 {
		t.Fatalf("Entries len = %d, want 3", got)
	}

	// Walk cursor to the end via NavDown.
	for i := 0; i < 5; i++ {
		m.Apply(nav.NavDown)
	}
	if m.Cursor() != 2 {
		t.Errorf("cursor after 5x NavDown = %d, want 2 (clamp at last)", m.Cursor())
	}

	// Remove the focused entry; cursor must drop to the new last.
	m.Remove(m.Cursor())
	if m.Cursor() != 1 {
		t.Errorf("cursor after removing last = %d, want 1", m.Cursor())
	}
	if got := len(m.Entries()); got != 2 {
		t.Fatalf("Entries len after Remove = %d, want 2", got)
	}

	// Remove down to empty.
	m.Remove(0)
	m.Remove(0)
	if got := len(m.Entries()); got != 0 {
		t.Fatalf("Entries len after full Remove = %d, want 0", got)
	}
	if m.Selected() != nil {
		t.Error("empty pool Selected() should be nil")
	}

	// Out-of-bounds Remove on empty pool is a no-op (no panic).
	m.Remove(0)
	m.Remove(-1)
	m.Remove(100)
}

// TestPool_MirrorContainerVoicesReplacesBankEntries pins the
// invariant the "pool accumulates across disks" feature relies on:
// when MirrorContainerVoices runs, "bank "-sourced entries are
// replaced from the new container while entries from other sources
// (Workspace imports, explicit extractions) survive.
func TestPool_MirrorContainerVoicesReplacesBankEntries(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("KEEP-ME", "workspace", fakeVoiceBytes("KEEP-ME"))
	m.AddFromAreaVoice("OLD A", "bank 1", fakeVoiceBytes("OLD A"))
	m.AddFromAreaVoice("OLD B", "bank 1", fakeVoiceBytes("OLD B"))

	if got := len(m.Entries()); got != 3 {
		t.Fatalf("setup: Entries len = %d, want 3", got)
	}

	// Mirror with two new "bank" voices.
	newBytes := [][]byte{
		fakeVoiceBytes("NEW A"),
		fakeVoiceBytes("NEW B"),
	}
	m.MirrorContainerVoices(newBytes)

	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("post-mirror len = %d, want 3 (1 kept + 2 fresh)", len(entries))
	}
	if entries[0].Name != "KEEP-ME" {
		t.Errorf("workspace entry dropped; got entries[0].Name = %q", entries[0].Name)
	}
	if entries[1].Name != "NEW A" || entries[2].Name != "NEW B" {
		t.Errorf("fresh entries malformed: %q, %q", entries[1].Name, entries[2].Name)
	}
}

// TestPool_RemoveBeyondBoundsIsNoop pins that pathological Remove
// indices (negative, past-end) don't panic and don't mutate the
// pool. Defensive against caller bugs.
func TestPool_RemoveBeyondBoundsIsNoop(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("A", "bank 1", fakeVoiceBytes("A"))
	m.AddFromAreaVoice("B", "bank 1", fakeVoiceBytes("B"))
	before := len(m.Entries())
	m.Remove(-1)
	m.Remove(99)
	if got := len(m.Entries()); got != before {
		t.Errorf("Remove with bad index mutated pool: len %d, want %d", got, before)
	}
}
