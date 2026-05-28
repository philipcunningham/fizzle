package fzfmidi

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

// buildMultiBankFZF synthesises an FZF whose bank sectors map distinct
// (bank, split) -> voice-slot references via vp[]. Each bankPlan is the
// list of voice-slot indices the bank's vp[] array points to (key-split
// position s -> bankPlan[s]). The voice area is sized to cover the
// largest referenced slot, and every referenced slot gets a plausible
// active voice header so InferVoiceCount and IsPlausibleVoiceSlot accept
// it.
func buildMultiBankFZF(t *testing.T, bankPlans [][]int) []byte {
	t.Helper()
	if len(bankPlans) == 0 {
		t.Fatal("buildMultiBankFZF: need at least one bank")
	}
	maxSlot := -1
	for _, plan := range bankPlans {
		for _, s := range plan {
			if s > maxSlot {
				maxSlot = s
			}
		}
	}
	nvoice := maxSlot + 1
	voiceSectors := disk.VoiceAreaSectors(nvoice)
	size := len(bankPlans)*disk.SectorSize + voiceSectors*disk.SectorSize
	data := make([]byte, size)
	for b, plan := range bankPlans {
		off := b * disk.SectorSize
		binary.LittleEndian.PutUint16(data[off+disk.BankVoiceCountOffset:], uint16(len(plan))) //nolint:gosec // G115: test constant
		bankName := disk.PadLabel(fmt.Sprintf("BANK%d", b))
		copy(data[off+disk.BankNameOffset:], bankName[:])
		for s, slot := range plan {
			binary.LittleEndian.PutUint16(data[off+disk.BankVoiceNumOffset+2*s:], uint16(slot)) //nolint:gosec // G115: test constant
			data[off+disk.BankAudioOutOffset+s] = disk.PolyphonicAudioOut
		}
	}
	voiceAreaStart := len(bankPlans) * disk.SectorSize
	for slot := 0; slot < nvoice; slot++ {
		voff := disk.VoiceSlotOffset(voiceAreaStart, slot)
		voiceName := disk.PadLabel(fmt.Sprintf("V%02d", slot+1))
		copy(data[voff+disk.VoiceNameOffset:], voiceName[:])
		binary.LittleEndian.PutUint16(data[voff+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	}
	return data
}

func writeFZFTo(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "multibank.fzf")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

const (
	testVoiceKick  = "KICK"
	testVoiceSnare = "SNARE"
	testVoiceReese = "REESE"
	testVoiceBass  = "BASS"
)

func readMIDIChan(t *testing.T, path string, voiceIdx int) uint8 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data[disk.BankMIDIRecvChanOffset+voiceIdx] + 1 // return 1-indexed
}

func TestSetByNameSingle(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceKick, testVoiceSnare, "HIHAT"})
	res, err := Set(p, []string{testVoiceSnare}, false, 2)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %d", len(res.Updated))
	}
	if res.Updated[0].Name != testVoiceSnare || res.Updated[0].NewChannel != 2 {
		t.Errorf("unexpected update: %+v", res.Updated[0])
	}
	if readMIDIChan(t, p, 1) != 2 {
		t.Error("SNARE (index 1) should be on channel 2")
	}
	if readMIDIChan(t, p, 0) != 1 {
		t.Error("KICK should still be on channel 1")
	}
}

func TestSetByNameCaseInsensitive(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceReese, "808"})
	_, err := Set(p, []string{"reese"}, false, 3)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if readMIDIChan(t, p, 0) != 3 {
		t.Error("REESE should be on channel 3")
	}
}

func TestSetByNameMultiple(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceKick, testVoiceSnare, testVoiceReese})
	res, err := Set(p, []string{testVoiceKick, testVoiceReese}, false, 4)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 2 {
		t.Errorf("expected 2 updated, got %d", len(res.Updated))
	}
	if readMIDIChan(t, p, 0) != 4 {
		t.Error("KICK should be on channel 4")
	}
	if readMIDIChan(t, p, 2) != 4 {
		t.Error("REESE should be on channel 4")
	}
	if readMIDIChan(t, p, 1) != 1 {
		t.Error("SNARE should still be on channel 1")
	}
}

func TestSetAll(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A", "B", "C", "D"})
	res, err := Set(p, nil, true, 5)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 4 {
		t.Errorf("expected 4 updated, got %d", len(res.Updated))
	}
	for i := range 4 {
		if readMIDIChan(t, p, i) != 5 {
			t.Errorf("voice %d should be on channel 5", i+1)
		}
	}
}

func TestSetAllResetToDefault(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A", "B"})
	// Set to channel 3
	Set(p, nil, true, 3) //nolint
	// Reset to 1
	res, err := Set(p, nil, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 2 {
		t.Errorf("expected 2 updated, got %d", len(res.Updated))
	}
	for i := range 2 {
		if readMIDIChan(t, p, i) != 1 {
			t.Errorf("voice %d should be reset to channel 1", i+1)
		}
	}
}

func TestSetNameNotFound(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceKick, testVoiceSnare})
	_, err := Set(p, []string{"NOSUCHVOICE"}, false, 2)
	if err == nil {
		t.Fatal("expected error for missing voice name")
	}
	if !strings.Contains(err.Error(), "NOSUCHVOICE") {
		t.Errorf("error should mention the missing name: %v", err)
	}
	// Error should list available voices.
	if !strings.Contains(err.Error(), testVoiceKick) || !strings.Contains(err.Error(), testVoiceSnare) {
		t.Errorf("error should list available voices: %v", err)
	}
}

func TestSetPartialNameNotFoundNoWrite(t *testing.T) {
	t.Parallel()
	// KICK exists, NOSUCH does not. File must not be modified at all.
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceKick, testVoiceSnare})
	before, _ := os.ReadFile(p)

	_, err := Set(p, []string{testVoiceKick, "NOSUCH"}, false, 2)
	if err == nil {
		t.Fatal("expected error")
	}

	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Error("file should not be modified when any voice name is missing")
	}
}

func TestSetChannelBoundaries(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})

	// Channel 1 and 16 are valid.
	if _, err := Set(p, nil, true, 1); err != nil {
		t.Errorf("channel 1 should be valid: %v", err)
	}
	if _, err := Set(p, nil, true, 16); err != nil {
		t.Errorf("channel 16 should be valid: %v", err)
	}

	// Channel 0 and 17 are invalid.
	if _, err := Set(p, nil, true, 0); err == nil {
		t.Error("channel 0 should be invalid")
	}
	if _, err := Set(p, nil, true, 17); err == nil {
		t.Error("channel 17 should be invalid")
	}
}

func TestSetBothVoiceAndAllErrors(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	_, err := Set(p, []string{"A"}, true, 2)
	if err == nil {
		t.Error("expected error when both --voice and --all specified")
	}
}

func TestSetNeitherVoiceNorAllErrors(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	_, err := Set(p, nil, false, 2)
	if err == nil {
		t.Error("expected error when neither --voice nor --all specified")
	}
}

func TestSetDuplicateStoredNames(t *testing.T) {
	t.Parallel()
	// Two voices with the same stored name. Both should be updated.
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceBass, testVoiceBass, "PAD"})
	res, err := Set(p, []string{testVoiceBass}, false, 3)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 2 {
		t.Errorf("expected 2 voices updated (both BASS), got %d", len(res.Updated))
	}
	if readMIDIChan(t, p, 0) != 3 || readMIDIChan(t, p, 1) != 3 {
		t.Error("both BASS voices should be on channel 3")
	}
	if readMIDIChan(t, p, 2) != 1 {
		t.Error("PAD should still be on channel 1")
	}
}

func TestSetNoChangeWhenAlreadyOnChannel(t *testing.T) {
	t.Parallel()
	// Setting channel 1 on voices already on channel 1 should produce no updates.
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A", "B"})
	res, err := Set(p, nil, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 0 {
		t.Errorf("expected 0 updates when channel unchanged, got %d", len(res.Updated))
	}
}

func TestSetRoundTrip(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceKick, testVoiceSnare, testVoiceBass})

	// Set BASS to channel 4.
	if _, err := Set(p, []string{testVoiceBass}, false, 4); err != nil {
		t.Fatal(err)
	}

	// Read back and verify raw byte.
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Voice index 2 (BASS), stored as 4-1=3.
	if data[disk.BankMIDIRecvChanOffset+2] != 3 {
		t.Errorf("raw byte for BASS channel: got %d, want 3 (channel 4 stored 0-indexed)", data[disk.BankMIDIRecvChanOffset+2])
	}

	// KICK and SNARE should still be 0 (channel 1).
	if data[disk.BankMIDIRecvChanOffset+0] != 0 {
		t.Errorf("KICK should still be channel 1 (raw 0), got %d", data[disk.BankMIDIRecvChanOffset+0])
	}
}

func TestFZFMidiSetNoChangeSkipsWrite(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A", "B"})
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	res, err := Set(p, nil, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 0 {
		t.Errorf("expected 0 updates, got %d", len(res.Updated))
	}

	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("file content should not change when no voices are updated")
	}
}

func TestSetMissingFile(t *testing.T) {
	t.Parallel()
	_, err := Set("/nonexistent/path.fzf", nil, true, 1)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// TestSetMultiBankWritesEveryBankSite is the regression test for F1: on
// real-hardware multi-bank dumps the per-voice mchn byte lives in each
// bank's own sector indexed by key-split position (spec §2-2). Writing
// only data[BankMIDIRecvChanOffset+voiceSlot] would patch bank 0 alone.
// Synthesise a 2-bank FZF where slot 0 is referenced from bank 0 split 0
// AND bank 1 split 2; both sites must be updated.
func TestSetMultiBankWritesEveryBankSite(t *testing.T) {
	t.Parallel()
	data := buildMultiBankFZF(t, [][]int{{0, 1}, {2, 3, 0}})
	p := writeFZFTo(t, data)

	res, err := Set(p, []string{"V01"}, false, 7)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 1 {
		t.Fatalf("expected 1 voice updated, got %d", len(res.Updated))
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Bank 0 split 0 -> data[BankMIDIRecvChanOffset+0] should be 6 (chan 7 stored 0-indexed).
	if v := got[disk.BankMIDIRecvChanOffset+0]; v != 6 {
		t.Errorf("bank 0 split 0 mchn: got %d, want 6", v)
	}
	// Bank 1 split 2 -> data[1*SectorSize+BankMIDIRecvChanOffset+2] should be 6.
	if v := got[disk.SectorSize+disk.BankMIDIRecvChanOffset+2]; v != 6 {
		t.Errorf("bank 1 split 2 mchn: got %d, want 6", v)
	}
	// Other splits in bank 1 must be unchanged (still 0 == channel 1).
	if v := got[disk.SectorSize+disk.BankMIDIRecvChanOffset+0]; v != 0 {
		t.Errorf("bank 1 split 0 mchn: got %d, want 0 (untouched)", v)
	}
}

// TestSetMultiBankWritesOnlyBank1Sites verifies the orphan case: a voice
// referenced only from bank 1 (not bank 0) is found via FindBankSitesForVoice
// and the write lands in bank 1's sector, not bank 0's stale byte.
func TestSetMultiBankWritesOnlyBank1Sites(t *testing.T) {
	t.Parallel()
	// Bank 0 references slot 0; bank 1 references slot 1 only.
	data := buildMultiBankFZF(t, [][]int{{0}, {1}})
	p := writeFZFTo(t, data)

	res, err := Set(p, []string{"V02"}, false, 5)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 1 {
		t.Fatalf("expected 1 voice updated, got %d", len(res.Updated))
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if v := got[disk.SectorSize+disk.BankMIDIRecvChanOffset+0]; v != 4 {
		t.Errorf("bank 1 split 0 mchn: got %d, want 4 (channel 5 stored 0-indexed)", v)
	}
	// Bank 0 split 0 (slot 0) MUST NOT be touched; it would corrupt slot 0's MIDI channel.
	if v := got[disk.BankMIDIRecvChanOffset+0]; v != 0 {
		t.Errorf("bank 0 split 0 mchn: got %d, want 0 (must not be written for a slot-1 update)", v)
	}
}
