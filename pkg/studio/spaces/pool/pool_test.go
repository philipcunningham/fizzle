package pool

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// Pool-entry Source labels used as test constants to keep goconst
// happy. The Pool's Source field is a free-form string in the model;
// these are the values the production code writes for FZV and WAV
// imports respectively.
const (
	testSourceFZV = "fzv"
	testSourceWAV = "wav"
)

// synthSamples returns a deterministic mono PCM buffer used for the
// FZV and WAV fixtures so we don't have to ship a real test asset.
func synthSamples(n int) []int16 {
	out := make([]int16, n)
	for i := range out {
		// Triangle wave: alternates sign so the bytes are non-zero
		// and obviously not silence.
		if i%2 == 0 {
			out[i] = int16(1000 + (i % 16))
		} else {
			out[i] = int16(-1000 - (i % 16))
		}
	}
	return out
}

// writeFZV writes a synthetic FZV file at path so AddFZV has something
// to read. The bytes come from voiceimport.Encode (the same path the
// production importer uses) so the fixture matches real shape.
func writeFZV(t *testing.T, path, voiceName string) {
	t.Helper()
	data := voiceimport.Encode(synthSamples(2048), 0, voiceName, 0, voiceimport.NoLoop())
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writeFZV(%s): %v", path, err)
	}
}

// writeWAV writes a mono 16-bit PCM WAV at the given path.
func writeWAV(t *testing.T, path string, sampleRate uint32, samples []int16) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create wav: %v", err)
	}
	defer f.Close() //nolint:errcheck
	w := &wav.File{
		SampleRate:    sampleRate,
		Samples:       samples,
		LoopStart:     -1,
		LoopEnd:       -1,
		MIDIUnityNote: 60,
	}
	if err := wav.Write(f, w); err != nil {
		t.Fatalf("wav.Write: %v", err)
	}
}

// writeStereoWAV writes a synthetic stereo WAV by hand. The bundled
// wav package only emits mono, so we hand-roll the bytes here. The
// resulting file has 2 channels in the fmt chunk; pkg/wav.Read
// rejects it with ErrChannelCount, which is the condition we want to
// observe.
func writeStereoWAV(t *testing.T, path string) {
	t.Helper()
	// 44-byte header for 16-bit stereo PCM at 44100 Hz, 4 sample frames.
	// dataSize = 4 frames * 2 channels * 2 bytes = 16.
	dataSize := uint32(16)
	header := make([]byte, 0, 44+int(dataSize))
	header = append(header,
		'R', 'I', 'F', 'F',
		0, 0, 0, 0, // file size, patched below
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ',
		16, 0, 0, 0, // fmt chunk size
		1, 0, // PCM
		2, 0, // stereo
		0x44, 0xac, 0, 0, // 44100 Hz
		0x10, 0xb1, 0x02, 0, // byte rate (44100 * 4)
		4, 0, // block align (4 bytes per frame)
		16, 0, // bits per sample
		'd', 'a', 't', 'a',
		byte(dataSize), byte(dataSize>>8), byte(dataSize>>16), byte(dataSize>>24), //nolint:gosec // G115: dataSize fixed at 16, fits in byte
	)
	fileSize := uint32(len(header) - 8 + int(dataSize)) //nolint:gosec // G115: test value bounded (44+16, positive)
	header[4] = byte(fileSize)                          //nolint:gosec // G115: test value bounded (44+16)
	header[5] = byte(fileSize >> 8)                     //nolint:gosec // G115: test value bounded (44+16)
	header[6] = byte(fileSize >> 16)                    //nolint:gosec // G115: test value bounded (44+16)
	header[7] = byte(fileSize >> 24)
	data := make([]byte, dataSize) // silent stereo frames
	all := append(header, data...) //nolint:gocritic // simple test scaffolding
	if err := os.WriteFile(path, all, 0o644); err != nil {
		t.Fatalf("write stereo wav: %v", err)
	}
}

func TestNew(t *testing.T) {
	m := New()
	if len(m.Entries()) != 0 {
		t.Errorf("Entries() len = %d, want 0", len(m.Entries()))
	}
	if m.Selected() != nil {
		t.Errorf("Selected() = %v, want nil", m.Selected())
	}
	// View on an empty pool should render the placeholder copy.
	out := m.View(80, 24)
	if !strings.Contains(out, "Pool is empty") {
		t.Errorf("empty View missing placeholder: %q", out)
	}
}

func TestAddFZV(t *testing.T) {
	dir := t.TempDir()
	fzvPath := filepath.Join(dir, "piano.fzv")
	writeFZV(t, fzvPath, "PIANO C4")

	m := New()
	if err := m.AddFZV(fzvPath); err != nil {
		t.Fatalf("AddFZV: %v", err)
	}
	if got := len(m.Entries()); got != 1 {
		t.Fatalf("Entries len = %d, want 1", got)
	}
	e := m.Entries()[0]
	if e.Source != testSourceFZV {
		t.Errorf("Source = %q, want %q", e.Source, testSourceFZV)
	}
	if e.Name == "" {
		t.Errorf("Name empty; want voice header name")
	}
	if !strings.Contains(strings.ToUpper(e.Name), "PIANO") {
		t.Errorf("Name = %q, want to contain PIANO", e.Name)
	}
	if len(e.Bytes) < disk.SectorSize {
		t.Errorf("Bytes len = %d, want >= %d", len(e.Bytes), disk.SectorSize)
	}
}

func TestAddFZV_RejectsNonVoice(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "junk.fzv")
	// Sector-sized buffer of zeros: the name field is unprintable and
	// the rate index byte is 0 (valid), but the name check fails so
	// IsPlausibleVoiceHeader returns false.
	junk := make([]byte, disk.SectorSize)
	if err := os.WriteFile(path, junk, 0o644); err != nil {
		t.Fatalf("write junk: %v", err)
	}
	m := New()
	if err := m.AddFZV(path); err == nil {
		t.Errorf("AddFZV on zero bytes accepted; want rejection")
	}
}

func TestAddWAV_Mono(t *testing.T) {
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "kick.wav")
	writeWAV(t, wavPath, 36000, synthSamples(2048))

	m := New()
	if err := m.AddWAV(wavPath, -1); err != nil {
		t.Fatalf("AddWAV(mono): %v", err)
	}
	if got := len(m.Entries()); got != 1 {
		t.Fatalf("Entries len = %d, want 1", got)
	}
	e := m.Entries()[0]
	if e.Source != testSourceWAV {
		t.Errorf("Source = %q, want %q", e.Source, testSourceWAV)
	}
	if e.Name == "" {
		t.Errorf("Name empty; want derived from path stem")
	}
	// The encoded bytes must be at least the sector-sized header.
	if len(e.Bytes) < disk.SectorSize {
		t.Errorf("Bytes len = %d, want >= %d", len(e.Bytes), disk.SectorSize)
	}
	// The encoded bytes are an FZ voice header; smoke-check the rate
	// index byte is in range.
	if int(e.Bytes[disk.VoiceSampOffset]) >= len(disk.SampleRates) {
		t.Errorf("rate index byte %d out of range", e.Bytes[disk.VoiceSampOffset])
	}
}

func TestAddWAV_StereoWithoutChoiceReturnsSentinel(t *testing.T) {
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "stereo.wav")
	writeStereoWAV(t, wavPath)

	m := New()
	err := m.AddWAV(wavPath, -1)
	if err == nil {
		t.Fatalf("AddWAV(stereo, -1) = nil; want ErrStereoNeedsChoice")
	}
	if !errors.Is(err, ErrStereoNeedsChoice) {
		t.Errorf("AddWAV(stereo, -1) error = %v; want ErrStereoNeedsChoice", err)
	}
	if got := len(m.Entries()); got != 0 {
		t.Errorf("Entries len = %d after failed add; want 0", got)
	}
}

func TestAddFromAreaVoice_StoresCopy(t *testing.T) {
	bytesIn := bytes.Repeat([]byte{0x42}, disk.SectorSize+128)
	m := New()
	m.AddFromAreaVoice("BASS D2", "bank 3", bytesIn)

	if got := len(m.Entries()); got != 1 {
		t.Fatalf("Entries len = %d, want 1", got)
	}
	e := m.Entries()[0]
	if e.Name != "BASS D2" || e.Source != "bank 3" {
		t.Errorf("entry = %+v; want Name=BASS D2 Source=bank 3", e)
	}
	if !bytes.Equal(e.Bytes, bytesIn) {
		t.Errorf("stored bytes differ from input")
	}
	// Mutating the input must not affect the stored entry: the Pool
	// owns its copy.
	bytesIn[0] = 0
	if e.Bytes[0] == 0 {
		t.Errorf("Pool stored an aliased slice; want defensive copy")
	}
}

func TestRemove(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("A", "bank 0", []byte{1, 2, 3})
	m.AddFromAreaVoice("B", "bank 0", []byte{4, 5, 6})
	m.AddFromAreaVoice("C", "bank 0", []byte{7, 8, 9})

	m.Remove(1) // remove "B"
	if got := len(m.Entries()); got != 2 {
		t.Fatalf("Entries len = %d, want 2", got)
	}
	if m.Entries()[0].Name != "A" || m.Entries()[1].Name != "C" {
		t.Errorf("remaining = %q, %q; want A, C",
			m.Entries()[0].Name, m.Entries()[1].Name)
	}

	// Out-of-bounds is a no-op.
	m.Remove(99)
	m.Remove(-1)
	if got := len(m.Entries()); got != 2 {
		t.Errorf("Entries len = %d after no-op Remove; want 2", got)
	}
}

func TestRemove_ClampsCursor(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("A", "bank 0", []byte{1})
	m.AddFromAreaVoice("B", "bank 0", []byte{2})
	// Move cursor to the last entry, then remove it: cursor must clamp
	// to the new last index rather than dangle past the end.
	_, _ = m.Apply(nav.NavDown)
	if m.Cursor() != 1 {
		t.Fatalf("cursor = %d after NavDown, want 1", m.Cursor())
	}
	m.Remove(1)
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d after removing last; want 0", m.Cursor())
	}
}

func TestApply_NavMovesCursor(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("A", "bank 0", []byte{1})
	m.AddFromAreaVoice("B", "bank 0", []byte{2})
	m.AddFromAreaVoice("C", "bank 0", []byte{3})

	if m.Cursor() != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.Cursor())
	}

	msg, intent := m.Apply(nav.NavDown)
	if msg != "" || intent.Kind != IntentNone {
		t.Errorf("NavDown returned msg=%q intent=%v; want empty", msg, intent)
	}
	if m.Cursor() != 1 {
		t.Errorf("cursor = %d after NavDown, want 1", m.Cursor())
	}

	// NavDown twice more: the cursor saturates at the last index.
	_, _ = m.Apply(nav.NavDown)
	_, _ = m.Apply(nav.NavDown)
	_, _ = m.Apply(nav.NavDown)
	if m.Cursor() != 2 {
		t.Errorf("cursor = %d after multiple NavDown, want 2", m.Cursor())
	}

	// NavUp from the last entry walks backwards and clamps at zero.
	_, _ = m.Apply(nav.NavUp)
	_, _ = m.Apply(nav.NavUp)
	_, _ = m.Apply(nav.NavUp)
	_, _ = m.Apply(nav.NavUp)
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d after NavUp x4, want 0", m.Cursor())
	}
}

func TestApply_ConfirmInPickerModeEmitsAssignIntent(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("PIANO", testSourceFZV, []byte{1, 2, 3})
	m.SetPickerTarget("Bank 1 / Area 1")

	msg, intent := m.Apply(nav.Confirm)
	if msg != "" {
		t.Errorf("Confirm in picker returned status %q; want empty", msg)
	}
	if intent.Kind != IntentAssignToArea {
		t.Errorf("intent kind = %v; want IntentAssignToArea", intent.Kind)
	}
	if intent.Entry == nil || intent.Entry.Name != "PIANO" {
		t.Errorf("intent.Entry = %v; want PIANO", intent.Entry)
	}
}

// TestApply_ConfirmOutsidePickerModeIsNoOp pins the new flow: a
// confirmation in browse mode (no Layout-initiated picker) does not
// emit IntentAssignToArea. The user is steered to the Layout `i`
// gesture via a status message instead.
func TestApply_ConfirmOutsidePickerModeIsNoOp(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("PIANO", testSourceFZV, []byte{1, 2, 3})

	msg, intent := m.Apply(nav.Confirm)
	if intent.Kind == IntentAssignToArea {
		t.Errorf("Confirm in browse mode emitted IntentAssignToArea; expected no-op")
	}
	if msg == "" {
		t.Errorf("expected a status hint pointing at the Layout `i` gesture")
	}
}

func TestApply_CancelInPickerModeEmitsCancelIntent(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("PIANO", testSourceFZV, []byte{1, 2, 3})
	m.SetPickerTarget("Bank 1 / Area 1")

	_, intent := m.Apply(nav.Cancel)
	if intent.Kind != IntentCancelPicker {
		t.Errorf("intent kind = %v; want IntentCancelPicker", intent.Kind)
	}
}

func TestApply_AuditionEmitsAuditionIntent(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("KICK", testSourceWAV, []byte{1, 2, 3})

	_, intent := m.Apply(nav.Audition)
	if intent.Kind != IntentAuditionPoolEntry {
		t.Errorf("intent kind = %v; want IntentAuditionPoolEntry", intent.Kind)
	}
	if intent.Entry == nil || intent.Entry.Name != "KICK" {
		t.Errorf("intent.Entry = %v; want KICK", intent.Entry)
	}
}

func TestApply_DeleteRemovesFocused(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("A", "bank 0", []byte{1})
	m.AddFromAreaVoice("B", "bank 0", []byte{2})

	msg, intent := m.Apply(nav.Delete)
	if intent.Kind != IntentNone {
		t.Errorf("Delete intent kind = %v; want IntentNone", intent.Kind)
	}
	if !strings.Contains(msg, "A") {
		t.Errorf("Delete status = %q; want to mention removed entry", msg)
	}
	if got := len(m.Entries()); got != 1 {
		t.Fatalf("Entries len = %d, want 1", got)
	}
	if m.Entries()[0].Name != "B" {
		t.Errorf("remaining = %q, want B", m.Entries()[0].Name)
	}
}

func TestApply_EmptyPoolIsNoop(t *testing.T) {
	m := New()
	msg, intent := m.Apply(nav.NavDown)
	if msg != "" || intent.Kind != IntentNone {
		t.Errorf("Apply on empty pool returned msg=%q intent=%v; want empty", msg, intent)
	}
	msg, intent = m.Apply(nav.Confirm)
	if msg != "" || intent.Kind != IntentNone {
		t.Errorf("Confirm on empty pool returned msg=%q intent=%v; want empty", msg, intent)
	}
}

func TestView_ListsEntriesWithCursor(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("PIANO C4", testSourceFZV, []byte{1})
	m.AddFromAreaVoice("BASS D2", "bank 5", []byte{2})
	m.AddFromAreaVoice("kick", testSourceWAV, []byte{3})
	_, _ = m.Apply(nav.NavDown)
	_, _ = m.Apply(nav.NavDown)

	out := m.View(80, 24)
	for _, want := range []string{"Pool", "PIANO C4", "BASS D2", "kick", testSourceFZV, "bank 5", testSourceWAV} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q; got:\n%s", want, out)
		}
	}
	// The cursor marker should appear at least once when entries are present.
	if !strings.Contains(out, "▶") {
		t.Errorf("View missing cursor marker; got:\n%s", out)
	}
}

func TestSelected(t *testing.T) {
	m := New()
	if m.Selected() != nil {
		t.Errorf("Selected on empty pool = %v; want nil", m.Selected())
	}
	m.AddFromAreaVoice("ONE", testSourceFZV, []byte{1})
	m.AddFromAreaVoice("TWO", testSourceFZV, []byte{2})
	if got := m.Selected(); got == nil || got.Name != "ONE" {
		t.Errorf("Selected = %v; want ONE", got)
	}
	_, _ = m.Apply(nav.NavDown)
	if got := m.Selected(); got == nil || got.Name != "TWO" {
		t.Errorf("Selected after NavDown = %v; want TWO", got)
	}
}
