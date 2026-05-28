package loopdetails

import (
	"encoding/binary"
	"strconv"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// newTestModel builds a single-voice FZF on disk, loads it, and returns
// the model plus the voice slot's FZF-absolute offset for direct
// byte-level assertions.
func newTestModel(t *testing.T) (*model.Model, int) {
	t.Helper()
	_, path := fzfbuilder.MakeTestFZF(t, []string{"ALPHA"})
	m, err := model.New(path)
	if err != nil {
		t.Fatalf("model.New: %v", err)
	}
	hdr := m.Header()
	if hdr == nil || hdr.NVoice < 1 {
		t.Fatalf("test fixture has no voices")
	}
	return m, disk.VoiceSlotOffset(hdr.VoiceAreaStart, 0)
}

func TestNewBuildsPrimitive(t *testing.T) {
	t.Parallel()
	m, _ := newTestModel(t)
	w := New(m)
	defer w.Close()
	if w.Primitive() == nil {
		t.Fatalf("Primitive: nil")
	}
}

func TestSustainDropDownReflectsByte(t *testing.T) {
	t.Parallel()
	m, voff := newTestModel(t)
	// Plant loop_sus = 3 before constructing the widget.
	m.Bytes()[voff+disk.VoiceLoopSusOffset] = 3

	w := New(m)
	defer w.Close()
	w.Bind(0)

	idx, label := w.susDD.GetCurrentOption()
	if idx != 3 || label != "3" {
		t.Errorf("sustain DD: got (%d, %q), want (3, \"3\")", idx, label)
	}
}

func TestSustainDropDownNoneByte(t *testing.T) {
	t.Parallel()
	m, voff := newTestModel(t)
	m.Bytes()[voff+disk.VoiceLoopSusOffset] = disk.NoSustainLoop // = 8 means "none"

	w := New(m)
	defer w.Close()
	w.Bind(0)

	idx, label := w.susDD.GetCurrentOption()
	if idx != stageCount || label != "none" {
		t.Errorf("sustain DD: got (%d, %q), want (%d, \"none\")", idx, label, stageCount)
	}
}

func TestReleaseDropDownAllByte(t *testing.T) {
	t.Parallel()
	m, voff := newTestModel(t)
	m.Bytes()[voff+disk.VoiceLoopEndOffset] = 8 // "all"

	w := New(m)
	defer w.Close()
	w.Bind(0)

	idx, label := w.endDD.GetCurrentOption()
	if idx != stageCount || label != "all" {
		t.Errorf("release DD: got (%d, %q), want (%d, \"all\")", idx, label, stageCount)
	}
}

func TestCommitXFadeWritesByte(t *testing.T) {
	t.Parallel()
	m, voff := newTestModel(t)
	w := New(m)
	defer w.Close()
	w.Bind(0)

	w.selStage = 2
	w.commitXFade(777)

	rel := stageXfOffset(2)
	got := binary.LittleEndian.Uint16(m.Bytes()[voff+rel:])
	if got != 777 {
		t.Errorf("XFade byte: got %d, want 777", got)
	}
}

func TestCommitStartPreservesFine(t *testing.T) {
	t.Parallel()
	m, voff := newTestModel(t)
	// Plant loopst[0] with fine = 0x42 and address = 0x000123.
	stPlant := (uint32(0x42) << disk.LoopStartFineShift) | 0x000123
	binary.LittleEndian.PutUint32(m.Bytes()[voff+stageStOffset(0):], stPlant)

	w := New(m)
	defer w.Close()
	w.Bind(0)
	w.selStage = 0

	w.commitStart(0xABCDEF)

	got := binary.LittleEndian.Uint32(m.Bytes()[voff+stageStOffset(0):])
	wantAddr := uint32(0xABCDEF) & disk.LoopStartAddressMask
	wantFine := uint32(0x42) << disk.LoopStartFineShift
	want := wantAddr | wantFine
	if got != want {
		t.Errorf("loopst[0]: got %#x, want %#x", got, want)
	}
}

func TestCommitEndPreservesSkipFlag(t *testing.T) {
	t.Parallel()
	m, voff := newTestModel(t)
	// Plant looped[3] with the Skip flag set and address = 0x7000_0000.
	edPlant := uint32(disk.LoopEndSkipMask) | uint32(0x70000000)
	binary.LittleEndian.PutUint32(m.Bytes()[voff+stageEdOffset(3):], edPlant)

	w := New(m)
	defer w.Close()
	w.Bind(0)
	w.selStage = 3

	w.commitEnd(0x12345)

	got := binary.LittleEndian.Uint32(m.Bytes()[voff+stageEdOffset(3):])
	want := uint32(disk.LoopEndSkipMask) | (uint32(0x12345) & uint32(disk.LoopEndAddressMask))
	if got != want {
		t.Errorf("looped[3]: got %#x, want %#x", got, want)
	}
}

func TestCommitNextTogglesSkipBit(t *testing.T) {
	t.Parallel()
	m, voff := newTestModel(t)
	binary.LittleEndian.PutUint32(m.Bytes()[voff+stageEdOffset(0):], 0x1000)

	w := New(m)
	defer w.Close()
	w.Bind(0)
	w.selStage = 0

	w.commitNext(true)
	got := binary.LittleEndian.Uint32(m.Bytes()[voff+stageEdOffset(0):])
	if got&disk.LoopEndSkipMask == 0 || disk.LoopEndAddress(got) != 0x1000 {
		t.Errorf("after Skip: got %#x, want skip-set, addr 0x1000", got)
	}

	w.commitNext(false)
	got = binary.LittleEndian.Uint32(m.Bytes()[voff+stageEdOffset(0):])
	if got&disk.LoopEndSkipMask != 0 || disk.LoopEndAddress(got) != 0x1000 {
		t.Errorf("after Trace: got %#x, want skip-clear, addr 0x1000", got)
	}
}

func TestCommitFinePreservesAddress(t *testing.T) {
	t.Parallel()
	m, voff := newTestModel(t)
	binary.LittleEndian.PutUint32(m.Bytes()[voff+stageStOffset(1):], 0x00ABCDEF)

	w := New(m)
	defer w.Close()
	w.Bind(0)
	w.selStage = 1

	w.commitFine(0x55)

	got := binary.LittleEndian.Uint32(m.Bytes()[voff+stageStOffset(1):])
	want := (uint32(0x55) << disk.LoopStartFineShift) | 0x00ABCDEF
	if got != want {
		t.Errorf("loopst[1]: got %#x, want %#x", got, want)
	}
}

func TestRefreshOnModelChange(t *testing.T) {
	t.Parallel()
	m, voff := newTestModel(t)
	w := New(m)
	defer w.Close()
	w.Bind(0)

	// Externally apply a XFade edit and ensure the widget's per-stage
	// editor picks it up via the Subscribe callback.
	w.selStage = 0
	if err := m.ApplyVoicePatch(0, voiceedit.Patch{
		Offset: stageXfOffset(0),
		Size:   2,
		Value:  555,
	}); err != nil {
		t.Fatalf("ApplyVoicePatch: %v", err)
	}

	// Confirm the byte landed.
	got := binary.LittleEndian.Uint16(m.Bytes()[voff+stageXfOffset(0):])
	if got != 555 {
		t.Fatalf("byte: got %d, want 555", got)
	}
	// And that the InputField was repainted.
	if w.xfIF.GetText() != strconv.Itoa(555) {
		t.Errorf("xfIF text: got %q, want %q", w.xfIF.GetText(), "555")
	}
}

func TestBindRebindsStageEditor(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"ALPHA", "BRAVO"})
	m, err := model.New(p)
	if err != nil {
		t.Fatalf("model.New: %v", err)
	}
	hdr := m.Header()
	voff0 := disk.VoiceSlotOffset(hdr.VoiceAreaStart, 0)
	voff1 := disk.VoiceSlotOffset(hdr.VoiceAreaStart, 1)

	// Different XFade values per voice at stage 0.
	binary.LittleEndian.PutUint16(m.Bytes()[voff0+stageXfOffset(0):], 100)
	binary.LittleEndian.PutUint16(m.Bytes()[voff1+stageXfOffset(0):], 900)

	w := New(m)
	defer w.Close()
	w.Bind(0)
	if w.xfIF.GetText() != "100" {
		t.Errorf("slot 0 xfIF: got %q, want %q", w.xfIF.GetText(), "100")
	}
	w.Bind(1)
	if w.xfIF.GetText() != "900" {
		t.Errorf("slot 1 xfIF: got %q, want %q", w.xfIF.GetText(), "900")
	}
}

func TestCloseUnsubscribesIdempotent(t *testing.T) {
	t.Parallel()
	m, _ := newTestModel(t)
	w := New(m)
	w.Close()
	w.Close() // second Close must not panic
}
