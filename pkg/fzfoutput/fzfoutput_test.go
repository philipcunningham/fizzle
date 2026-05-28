package fzfoutput

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

const (
	testVoiceKick  = "KICK"
	testVoiceSnare = "SNARE"
	testVoiceBass  = "BASS"
	testVoicePad   = "PAD"
	testMultiOut   = "2,4,6,8"
)

func TestParseOutputFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    uint8
		wantErr bool
	}{
		{allValue, 0xff, false},
		{"ALL", 0xff, false},
		{"1", 0x01, false},
		{"2", 0x02, false},
		{"3", 0x04, false},
		{"4", 0x08, false},
		{"5", 0x10, false},
		{"6", 0x20, false},
		{"7", 0x40, false},
		{"8", 0x80, false},
		{"1,3,5", 0x15, false},
		{"1,2,3,4,5,6,7,8", 0xff, false},
		{"1,1", 0x01, false},
		{testMultiOut, 0xaa, false},
		{"", 0, true},
		{"0", 0, true},
		{"9", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
		{"all,1", 0, true},
		{"1,0", 0, true},
		{"1,9", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseOutputFlag(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseOutputFlag(%q) = 0x%02x, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseOutputFlag(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseOutputFlag(%q) = 0x%02x, want 0x%02x", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	t.Parallel()
	inputs := []string{"1", "2", "8", "1,3,5", testMultiOut, allValue}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			gchn, err := ParseOutputFlag(in)
			if err != nil {
				t.Fatalf("ParseOutputFlag(%q): %v", in, err)
			}
			formatted := disk.FormatAudioOut(gchn)
			back, err := ParseOutputFlag(formatted)
			if err != nil {
				t.Fatalf("re-parse %q: %v", formatted, err)
			}
			if back != gchn {
				t.Errorf("round-trip: %q -> 0x%02x -> %q -> 0x%02x", in, gchn, formatted, back)
			}
		})
	}
}

func readGCHN(t *testing.T, path string, voiceIdx int) uint8 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data[disk.BankAudioOutOffset+voiceIdx]
}

func TestSetSingleVoiceOutput(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceKick, testVoiceSnare, "HIHAT"})
	res, err := Set(p, []string{testVoiceSnare}, false, 0x04)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %d", len(res.Updated))
	}
	if res.Updated[0].Name != testVoiceSnare {
		t.Errorf("updated voice: got %q, want %s", res.Updated[0].Name, testVoiceSnare)
	}
	if readGCHN(t, p, 1) != 0x04 {
		t.Errorf("SNARE gchn: got 0x%02x, want 0x04", readGCHN(t, p, 1))
	}
	if readGCHN(t, p, 0) != disk.PolyphonicAudioOut {
		t.Error("KICK should still be polyphonic")
	}
	if readGCHN(t, p, 2) != disk.PolyphonicAudioOut {
		t.Error("HIHAT should still be polyphonic")
	}
}

func TestSetMultipleOutputs(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoicePad, testVoiceBass})
	gchn, _ := ParseOutputFlag("1,3,5")
	res, err := Set(p, []string{testVoicePad}, false, gchn)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %d", len(res.Updated))
	}
	if readGCHN(t, p, 0) != 0x15 {
		t.Errorf("PAD gchn: got 0x%02x, want 0x15", readGCHN(t, p, 0))
	}
}

func TestSetAllVoices(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A", "B", "C", "D"})
	res, err := Set(p, nil, true, 0x02)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 4 {
		t.Errorf("expected 4 updated, got %d", len(res.Updated))
	}
	for i := range 4 {
		if readGCHN(t, p, i) != 0x02 {
			t.Errorf("voice %d gchn: got 0x%02x, want 0x02", i+1, readGCHN(t, p, i))
		}
	}
}

func TestResetToPoly(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A", "B"})
	Set(p, nil, true, 0x02) //nolint:errcheck
	res, err := Set(p, nil, true, disk.PolyphonicAudioOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 2 {
		t.Errorf("expected 2 updated, got %d", len(res.Updated))
	}
	for i := range 2 {
		if readGCHN(t, p, i) != disk.PolyphonicAudioOut {
			t.Errorf("voice %d should be polyphonic", i+1)
		}
	}
}

func TestSetNameCaseInsensitive(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"REESE", "808"})
	_, err := Set(p, []string{"reese"}, false, 0x08)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if readGCHN(t, p, 0) != 0x08 {
		t.Errorf("REESE gchn: got 0x%02x, want 0x08", readGCHN(t, p, 0))
	}
}

func TestSetNameNotFound(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceKick, testVoiceSnare})
	_, err := Set(p, []string{"NOSUCHVOICE"}, false, 0x01)
	if err == nil {
		t.Fatal("expected error for missing voice name")
	}
	if !strings.Contains(err.Error(), "NOSUCHVOICE") {
		t.Errorf("error should mention missing name: %v", err)
	}
	if !strings.Contains(err.Error(), testVoiceKick) || !strings.Contains(err.Error(), testVoiceSnare) {
		t.Errorf("error should list available voices: %v", err)
	}
}

func TestSetPartialNameNotFoundNoWrite(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceKick, testVoiceSnare})
	before, _ := os.ReadFile(p)
	_, err := Set(p, []string{testVoiceKick, "NOSUCH"}, false, 0x02)
	if err == nil {
		t.Fatal("expected error")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Error("file should not be modified when any voice name is missing")
	}
}

func TestSetNoChangeWhenAlreadySet(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A", "B"})
	res, err := Set(p, nil, true, disk.PolyphonicAudioOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 0 {
		t.Errorf("expected 0 updates when output unchanged, got %d", len(res.Updated))
	}
}

func TestSetNoChangeSkipsWrite(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A", "B"})
	before, _ := os.ReadFile(p)
	res, err := Set(p, nil, true, disk.PolyphonicAudioOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 0 {
		t.Errorf("expected 0 updates, got %d", len(res.Updated))
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Error("file content should not change when no voices are updated")
	}
}

func TestSetBothVoiceAndAllErrors(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	_, err := Set(p, []string{"A"}, true, 0x01)
	if err == nil {
		t.Error("expected error when both --voice and --all specified")
	}
}

func TestSetNeitherVoiceNorAllErrors(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	_, err := Set(p, nil, false, 0x01)
	if err == nil {
		t.Error("expected error when neither --voice nor --all specified")
	}
}

func TestSetDuplicateStoredNames(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceBass, testVoiceBass, testVoicePad})
	res, err := Set(p, []string{testVoiceBass}, false, 0x04)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 2 {
		t.Errorf("expected 2 voices updated (both %s), got %d", testVoiceBass, len(res.Updated))
	}
	if readGCHN(t, p, 0) != 0x04 || readGCHN(t, p, 1) != 0x04 {
		t.Error("both BASS voices should have gchn 0x04")
	}
	if readGCHN(t, p, 2) != disk.PolyphonicAudioOut {
		t.Error("PAD should still be polyphonic")
	}
}

func TestSetMissingFile(t *testing.T) {
	t.Parallel()
	_, err := Set("/nonexistent/path.fzf", nil, true, 0x01)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSetRoundTrip(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{testVoiceKick, testVoiceSnare, testVoiceBass})
	if _, err := Set(p, []string{testVoiceBass}, false, 0x08); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if data[disk.BankAudioOutOffset+2] != 0x08 {
		t.Errorf("raw byte for BASS output: got 0x%02x, want 0x08", data[disk.BankAudioOutOffset+2])
	}
	if data[disk.BankAudioOutOffset+0] != disk.PolyphonicAudioOut {
		t.Errorf("KICK should still be polyphonic, got 0x%02x", data[disk.BankAudioOutOffset+0])
	}
}

// TestSetMultiBankWritesEveryBankSite is the regression test for F2: on
// multi-bank dumps the gchn byte lives in every bank that references the
// voice via vp[]. Writing only data[BankAudioOutOffset+voiceSlot] would
// patch bank 0; voices owned only by banks 1-7 stay on their old output.
func TestSetMultiBankWritesEveryBankSite(t *testing.T) {
	t.Parallel()
	// Bank 0 maps slot 0 at split 0; bank 1 maps slot 0 again at split 2.
	data := buildMultiBankFZF(t, [][]int{{0, 1}, {2, 3, 0}})
	p := writeFZFTo(t, data)

	res, err := Set(p, []string{"V01"}, false, 0x04)
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
	if v := got[disk.BankAudioOutOffset+0]; v != 0x04 {
		t.Errorf("bank 0 split 0 gchn: got 0x%02x, want 0x04", v)
	}
	if v := got[disk.SectorSize+disk.BankAudioOutOffset+2]; v != 0x04 {
		t.Errorf("bank 1 split 2 gchn: got 0x%02x, want 0x04", v)
	}
}

func buildMultiBankFZF(t *testing.T, bankPlans [][]int) []byte {
	t.Helper()
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
		binary.LittleEndian.PutUint16(data[off+disk.BankVoiceCountOffset:], uint16(len(plan))) //nolint:gosec // G115: test
		bankName := disk.PadLabel(fmt.Sprintf("BANK%d", b))
		copy(data[off+disk.BankNameOffset:], bankName[:])
		for s, slot := range plan {
			binary.LittleEndian.PutUint16(data[off+disk.BankVoiceNumOffset+2*s:], uint16(slot)) //nolint:gosec // G115: test
			data[off+disk.BankAudioOutOffset+s] = disk.PolyphonicAudioOut
		}
	}
	voiceAreaStart := len(bankPlans) * disk.SectorSize
	for slot := 0; slot < nvoice; slot++ {
		voff := disk.VoiceSlotOffset(voiceAreaStart, slot)
		vName := disk.PadLabel(fmt.Sprintf("V%02d", slot+1))
		copy(data[voff+disk.VoiceNameOffset:], vName[:])
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
