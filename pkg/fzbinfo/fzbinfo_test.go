package fzbinfo

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

const (
	testNameKick  = "KICK"
	testNameSnare = "SNARE"
	testNameVox   = "VOX"
)

func buildFZB(t *testing.T, names []string) string {
	t.Helper()
	fzfData, _ := fzfbuilder.MakeTestFZF(t, names)

	nvoice := len(names)
	voiceSectors := disk.VoiceAreaSectors(nvoice)
	fzbEnd := disk.SectorSize + voiceSectors*disk.SectorSize
	if fzbEnd > len(fzfData) {
		t.Fatalf("FZF too small to truncate: %d < %d", len(fzfData), fzbEnd)
	}
	fzbData := fzfData[:fzbEnd]

	fzbPath := filepath.Join(t.TempDir(), "test.fzb")
	if err := os.WriteFile(fzbPath, fzbData, 0644); err != nil {
		t.Fatal(err)
	}
	return fzbPath
}

func TestParseMinimalFZB(t *testing.T) {
	t.Parallel()
	fzbPath := buildFZB(t, []string{testNameKick, testNameSnare})
	info, err := Parse(fzbPath)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.VoiceCount != 2 {
		t.Errorf("VoiceCount = %d, want 2", info.VoiceCount)
	}
	if len(info.Voices) != 2 {
		t.Fatalf("got %d voices, want 2", len(info.Voices))
	}
	if info.Voices[0].Name != testNameKick {
		t.Errorf("voice 0 name = %q, want %s", info.Voices[0].Name, testNameKick)
	}
	if info.Voices[1].Name != testNameSnare {
		t.Errorf("voice 1 name = %q, want %s", info.Voices[1].Name, testNameSnare)
	}
}

func TestParseFilename(t *testing.T) {
	t.Parallel()
	fzbPath := buildFZB(t, []string{testNameVox})
	info, err := Parse(fzbPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Filename != "test.fzb" {
		t.Errorf("Filename = %q, want test.fzb", info.Filename)
	}
}

func TestParseVoiceFields(t *testing.T) {
	t.Parallel()
	fzbPath := buildFZB(t, []string{"TESTPAD"})
	info, err := Parse(fzbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Voices) != 1 {
		t.Fatalf("expected 1 voice, got %d", len(info.Voices))
	}
	v := info.Voices[0]
	if v.Index != 1 {
		t.Errorf("Index = %d, want 1", v.Index)
	}
	if v.MIDIChannel < 1 {
		t.Errorf("MIDIChannel = %d, want >= 1", v.MIDIChannel)
	}
	if v.Output == "" {
		t.Error("Output should not be empty")
	}
}

func TestParseMissingFile(t *testing.T) {
	t.Parallel()
	_, err := Parse("/nonexistent/path.fzb")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParseTooSmall(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "tiny.fzb")
	if err := os.WriteFile(p, make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Parse(p)
	if err == nil {
		t.Error("expected error for file smaller than one sector")
	}
}

func TestParseTruncatedVoiceArea(t *testing.T) {
	t.Parallel()
	fzfData, _ := fzfbuilder.MakeTestFZF(t, []string{"A", "B", "C", "D", "E"})
	truncated := fzfData[:disk.SectorSize+100]
	p := filepath.Join(t.TempDir(), "trunc.fzb")
	if err := os.WriteFile(p, truncated, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Parse(p)
	if err == nil {
		t.Error("expected error for truncated voice area")
	}
}

func TestRenderShowsVoiceCount(t *testing.T) {
	t.Parallel()
	fzbPath := buildFZB(t, []string{"HI", "LO"})
	info, err := Parse(fzbPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	Render(&buf, info)
	if !strings.Contains(buf.String(), "Voices:    2") {
		t.Errorf("expected 'Voices:    2' in output:\n%s", buf.String())
	}
}

func TestRenderShowsFilenameNotFullPath(t *testing.T) {
	t.Parallel()
	fzbPath := buildFZB(t, []string{testNameVox})
	info, err := Parse(fzbPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	Render(&buf, info)
	dir := filepath.Dir(fzbPath)
	if strings.Contains(buf.String(), dir) {
		t.Errorf("full directory path leaked into output:\n%s", buf.String())
	}
}

func TestRenderJSONValid(t *testing.T) {
	t.Parallel()
	fzbPath := buildFZB(t, []string{testNameKick, testNameSnare})
	info, err := Parse(fzbPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, info); err != nil {
		t.Fatal(err)
	}
	var decoded BankDump
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output is not valid: %v\n%s", err, buf.String())
	}
	if decoded.VoiceCount != 2 {
		t.Errorf("decoded VoiceCount = %d, want 2", decoded.VoiceCount)
	}
	if len(decoded.Voices) != 2 {
		t.Errorf("decoded voices count = %d, want 2", len(decoded.Voices))
	}
}

func TestRenderJSONExcludesShowVelocity(t *testing.T) {
	t.Parallel()
	fzbPath := buildFZB(t, []string{testNameVox})
	info, err := Parse(fzbPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, info); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "show_velocity") {
		t.Error("JSON should not contain show_velocity (it is a display hint)")
	}
}

// TestParseRecoversFromStaleBstep pins fzbinfo to the voice-area walk
// rather than the bank sector's bstep field. Standalone FZF files lose
// the dBP file head's vn field, so fzfinfo recovers vn by walking; FZBs
// face the same gap, and trusting bstep lets a buggy upstream tool's
// stale value produce a phantom voice count.
//
// The synthetic FZB here claims bstep=5 with only 2 plausible voice slots
// followed by garbage. The parser must report VoiceCount=2.
func TestParseRecoversFromStaleBstep(t *testing.T) {
	t.Parallel()
	// Build a valid 2-voice FZF in memory, truncate to one bank + one
	// voice-area sector to get FZB shape, then corrupt it.
	fzfData, _ := fzfbuilder.MakeTestFZF(t, []string{testNameKick, testNameSnare})
	fzbEnd := disk.SectorSize + disk.VoiceAreaSectors(2)*disk.SectorSize
	if fzbEnd > len(fzfData) {
		t.Fatalf("FZF too small to truncate: %d < %d", len(fzfData), fzbEnd)
	}
	data := append([]byte(nil), fzfData[:fzbEnd]...)
	// Corrupt bstep to claim 5 voices...
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 5)
	// ...and stamp an unrecognised loop mode into slot 2 so the walk stops
	// there. Slot 2's all-zero bytes otherwise parse as
	// PlaybackModeNoSound, which IsActiveOrEmptyVoiceSlot accepts as an
	// empty placeholder, carrying the walk into the trailing padding.
	slot2 := disk.VoiceSlotOffset(disk.SectorSize, 2)
	binary.LittleEndian.PutUint16(data[slot2+disk.VoiceLoopModeOffset:], 0xBEEF)

	p := filepath.Join(t.TempDir(), "stale-bstep.fzb")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	info, err := Parse(p)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.VoiceCount != 2 {
		t.Errorf("VoiceCount = %d, want 2 (inferred from voice area, not bstep=5)", info.VoiceCount)
	}
	if len(info.Voices) != 2 {
		t.Errorf("len(Voices) = %d, want 2", len(info.Voices))
	}
}

// TestParseErrorsWhenNoPlausibleVoices covers a degenerate FZB with zero
// plausible voices: the walk returns 0 and the parser errors rather than
// handing back an empty BankDump.
func TestParseErrorsWhenNoPlausibleVoices(t *testing.T) {
	t.Parallel()
	// One-sector bank followed by a single all-zero voice sector. That
	// leaves PlaybackModeNoSound in the mode byte at offset 0x10, which
	// IsActiveOrEmptyVoiceSlot accepts, so InferVoiceCount walks and
	// returns a non-zero count. Stamping non-NoSound, non-Normal garbage
	// into the mode field gets it rejected and the count to zero.
	data := make([]byte, disk.SectorSize*2)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 3)
	for i := 0; i < disk.VoicesPerSector; i++ {
		voff := disk.VoiceSlotOffset(disk.SectorSize, i)
		// Set an unrecognised loop mode so InferVoiceCount rejects each slot.
		binary.LittleEndian.PutUint16(data[voff+disk.VoiceLoopModeOffset:], 0xBEEF)
	}
	p := filepath.Join(t.TempDir(), "noplausible.fzb")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(p); err == nil {
		t.Error("expected error when no plausible voice headers are present")
	}
}

// TestRenderVelocityZeroShowsOff pins Fix G: VelLow == 0 with VelHigh ==
// 0 matches no MIDI note-on (spec §1-5 gives htch/ltch the range 1 to
// 127), so the voice is silent on hardware and renders "off", the same as
// fzfinfo.
func TestRenderVelocityZeroShowsOff(t *testing.T) {
	t.Parallel()
	fzfData, _ := fzfbuilder.MakeTestFZF(t, []string{"SILENT"})
	fzbEnd := disk.SectorSize + disk.VoiceAreaSectors(1)*disk.SectorSize
	data := append([]byte(nil), fzfData[:fzbEnd]...)

	// Force vel_low and vel_high in the bank sector to zero for slot 0.
	data[disk.BankVelLowOffset+0] = 0
	data[disk.BankVelHighOffset+0] = 0

	p := filepath.Join(t.TempDir(), "off.fzb")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	info, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	Render(&buf, info)
	out := buf.String()
	if !strings.Contains(out, "off") {
		t.Errorf("velocity (0,0) should render as 'off':\n%s", out)
	}
	if strings.Contains(out, "any") {
		t.Errorf("velocity (0,0) should not render as 'any':\n%s", out)
	}
}

// TestParseInsertsNoSoundPlaceholders pins Fix F: when
// ParseBankVoiceEntry returns false for a NoSound slot, fzbinfo emits a
// placeholder so len(info.Voices) matches VoiceCount and bank vp[]
// references stay aligned. Skipping the slot shifts every later voice's
// index left.
func TestParseInsertsNoSoundPlaceholders(t *testing.T) {
	t.Parallel()
	// Build a 3-voice FZB whose middle slot is NoSound.
	fzfData, _ := fzfbuilder.MakeTestFZF(t, []string{"FIRST", "DROP", "LAST"})
	fzbEnd := disk.SectorSize + disk.VoiceAreaSectors(3)*disk.SectorSize
	data := append([]byte(nil), fzfData[:fzbEnd]...)

	// Stamp PlaybackModeNoSound (0x0000) over slot 1's loop_mode.
	slot1 := disk.VoiceSlotOffset(disk.SectorSize, 1)
	binary.LittleEndian.PutUint16(data[slot1+disk.VoiceLoopModeOffset:], disk.PlaybackModeNoSound)

	p := filepath.Join(t.TempDir(), "nosound.fzb")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	info, err := Parse(p)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.VoiceCount != 3 {
		t.Errorf("VoiceCount = %d, want 3", info.VoiceCount)
	}
	if len(info.Voices) != 3 {
		t.Fatalf("len(Voices) = %d, want 3 (NoSound placeholder must be retained)", len(info.Voices))
	}
	if info.Voices[1].PlaybackMode != disk.PlaybackModeNameNoSound {
		t.Errorf("Voices[1].PlaybackMode = %q, want %q", info.Voices[1].PlaybackMode, disk.PlaybackModeNameNoSound)
	}
	if info.Voices[0].Name != "FIRST" || info.Voices[2].Name != "LAST" {
		t.Errorf("voice ordering broken: [0]=%q [2]=%q", info.Voices[0].Name, info.Voices[2].Name)
	}
}

func TestRenderVelocityColumnHiddenByDefault(t *testing.T) {
	t.Parallel()
	fzbPath := buildFZB(t, []string{"A", "B"})
	info, err := Parse(fzbPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	Render(&buf, info)
	if strings.Contains(buf.String(), "Velocity") {
		t.Errorf("Velocity column should be hidden for standard velocity ranges:\n%s", buf.String())
	}
}
