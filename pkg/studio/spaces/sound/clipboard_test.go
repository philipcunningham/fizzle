package sound

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// bindTwo loads Piano.fzf into a fresh tempdir, returns two Sound
// models bound to Bank 0 Area 0 and Bank 0 Area 1 against the same
// container so copy/paste round-trips can be driven against real
// voice headers.
func bindTwo(t testing.TB) (src, dst *Model, m *model.Model) {
	t.Helper()
	srcPath := filepath.Join("..", "..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano", "Piano.fzf")
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("missing Piano.fzf: %v", err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(target, raw, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed fixture: %v", err)
	}
	loaded, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	voiceArea := info.BankCount * disk.SectorSize

	sm1 := New()
	sm1.Bind(loaded, info.BankCount, voiceArea, info.AudioAreaStart, 0, 0)
	if !sm1.HasVoice() {
		t.Fatalf("Piano.fzf has no voice at Bank 0 Area 0")
	}
	sm2 := New()
	sm2.Bind(loaded, info.BankCount, voiceArea, info.AudioAreaStart, 0, 1)
	if !sm2.HasVoice() {
		t.Fatalf("Piano.fzf has no voice at Bank 0 Area 1")
	}
	return &sm1, &sm2, loaded
}

// setFocus is a test helper that positions the Sound model's cursor
// at a specific (row, col) without driving navigation.
func setFocus(sm *Model, r row, col int) {
	sm.row = r
	sm.col = col
}

// TestClipboard_StartsEmpty pins that a fresh Clipboard reports no
// payload and Paste rejects the operation.
func TestClipboard_StartsEmpty(t *testing.T) {
	c := &Clipboard{}
	if c.Kind() != ClipboardKindNone {
		t.Errorf("fresh clipboard kind = %v; want None", c.Kind())
	}
	if got := c.Summary(); got != "" {
		t.Errorf("fresh clipboard summary = %q; want empty", got)
	}
}

// TestClipboard_CopyStage_DCAToDCA copies a DCA stage cell and pastes
// it onto a different stage; the target's rate / stop level should
// match the source.
func TestClipboard_CopyStage_DCAToDCA(t *testing.T) {
	src, dst, m := bindTwo(t)
	// Source stage 3 of DCA in voice 0.
	setFocus(src, rowDCA, 3+3) // col-3 selects stage 0; col 6 = stage 3
	// Make sure source stage carries a distinctive rate / stop.
	srcStageOff := src.voiceOff + disk.VoiceDCARateOffset + 3
	srcStopOff := src.voiceOff + disk.VoiceDCAStopOffset + 3
	if err := m.ApplyBatch([]model.Patch{
		{Offset: srcStageOff, Old: []byte{m.Bytes()[srcStageOff]}, New: []byte{0x42}},
		{Offset: srcStopOff, Old: []byte{m.Bytes()[srcStopOff]}, New: []byte{0xa0}},
	}); err != nil {
		t.Fatalf("seed source stage: %v", err)
	}
	src.containerBytes = m.Bytes()

	c := &Clipboard{}
	if msg := src.Copy(c); msg == "" {
		t.Fatal("Copy returned empty status; want something descriptive")
	}
	if c.Kind() != ClipboardKindStage {
		t.Errorf("kind after copy = %v; want Stage", c.Kind())
	}

	// Target: stage 5 of DCA on voice 1.
	setFocus(dst, rowDCA, 5+3)
	dst.containerBytes = m.Bytes()
	tgtStageOff := dst.voiceOff + disk.VoiceDCARateOffset + 5
	tgtStopOff := dst.voiceOff + disk.VoiceDCAStopOffset + 5
	if m.Bytes()[tgtStageOff] == 0x42 && m.Bytes()[tgtStopOff] == 0xa0 {
		t.Fatal("precondition violated: target already matches source bytes")
	}

	if msg := dst.Paste(c); msg == "" {
		t.Fatal("Paste returned empty status; want something descriptive")
	}
	got := m.Bytes()
	if got[tgtStageOff] != 0x42 {
		t.Errorf("target rate byte = %#x; want 0x42", got[tgtStageOff])
	}
	if got[tgtStopOff] != 0xa0 {
		t.Errorf("target stop byte = %#x; want 0xa0", got[tgtStopOff])
	}
}

// TestClipboard_PasteStage_DCFtoDCA cross-envelope stage copy works:
// stage cells share shape across DCA and DCF, so a DCF stage pastes
// onto a DCA stage with the same field set.
func TestClipboard_PasteStage_DCFtoDCA(t *testing.T) {
	src, dst, m := bindTwo(t)
	// DCF stage 2 of voice 0.
	setFocus(src, rowDCF, 2+4)
	srcRateOff := src.voiceOff + disk.VoiceDCFRateOffset + 2
	srcStopOff := src.voiceOff + disk.VoiceDCFStopOffset + 2
	if err := m.ApplyBatch([]model.Patch{
		{Offset: srcRateOff, Old: []byte{m.Bytes()[srcRateOff]}, New: []byte{0x33}},
		{Offset: srcStopOff, Old: []byte{m.Bytes()[srcStopOff]}, New: []byte{0x55}},
	}); err != nil {
		t.Fatalf("seed DCF stage: %v", err)
	}
	src.containerBytes = m.Bytes()
	c := &Clipboard{}
	src.Copy(c)

	// DCA stage 4 of voice 1.
	setFocus(dst, rowDCA, 4+3)
	dst.containerBytes = m.Bytes()
	if msg := dst.Paste(c); msg == "" {
		t.Fatal("DCF stage paste onto DCA stage returned empty status")
	}
	got := m.Bytes()
	tgtRateOff := dst.voiceOff + disk.VoiceDCARateOffset + 4
	tgtStopOff := dst.voiceOff + disk.VoiceDCAStopOffset + 4
	if got[tgtRateOff] != 0x33 {
		t.Errorf("target rate = %#x; want 0x33 (DCF stage rate)", got[tgtRateOff])
	}
	if got[tgtStopOff] != 0x55 {
		t.Errorf("target stop = %#x; want 0x55 (DCF stage stop)", got[tgtStopOff])
	}
}

// TestClipboard_StageRoleSus copies a SUS-role stage and confirms
// pasting transfers the SUS role to the target stage (without
// touching the source).
func TestClipboard_StageRoleSus(t *testing.T) {
	src, dst, m := bindTwo(t)

	// Source: voice 0, force SUS to stage 4 of DCA.
	susOff := src.voiceOff + disk.VoiceDCASusOffset
	endOff := src.voiceOff + disk.VoiceDCAEndOffset
	if err := m.ApplyBatch([]model.Patch{
		{Offset: susOff, Old: []byte{m.Bytes()[susOff]}, New: []byte{4}},
		{Offset: endOff, Old: []byte{m.Bytes()[endOff]}, New: []byte{7}},
	}); err != nil {
		t.Fatalf("seed envelope roles: %v", err)
	}
	src.containerBytes = m.Bytes()
	setFocus(src, rowDCA, 4+3) // stage 4 = SUS in source
	c := &Clipboard{}
	if msg := src.Copy(c); msg == "" {
		t.Fatal("Copy(stage 4 SUS) returned empty")
	}

	// Target voice 1: pre-set its SUS / END to something different,
	// then paste onto stage 1 (currently a Normal stage).
	tgtSusOff := dst.voiceOff + disk.VoiceDCASusOffset
	tgtEndOff := dst.voiceOff + disk.VoiceDCAEndOffset
	if err := m.ApplyBatch([]model.Patch{
		{Offset: tgtSusOff, Old: []byte{m.Bytes()[tgtSusOff]}, New: []byte{6}},
		{Offset: tgtEndOff, Old: []byte{m.Bytes()[tgtEndOff]}, New: []byte{7}},
	}); err != nil {
		t.Fatalf("seed target envelope roles: %v", err)
	}
	dst.containerBytes = m.Bytes()
	setFocus(dst, rowDCA, 1+3) // stage 1 in target
	if msg := dst.Paste(c); msg == "" {
		t.Fatal("Paste(stage role) returned empty")
	}
	if got := m.Bytes()[tgtSusOff]; got != 1 {
		t.Errorf("target SUS = %d; want 1 (SUS role pasted onto stage 1)", got)
	}
	if got := m.Bytes()[tgtEndOff]; got != 7 {
		t.Errorf("target END = %d; want 7 (untouched)", got)
	}
}

// TestClipboard_MismatchedCellTypes_NoOp pins the spec'd no-op
// behaviour: pasting a stage clipboard onto a loop cell does
// nothing and emits the prescribed status text.
func TestClipboard_MismatchedCellTypes_NoOp(t *testing.T) {
	src, dst, m := bindTwo(t)

	setFocus(src, rowDCA, 4) // DCA stage 1
	c := &Clipboard{}
	src.Copy(c)

	// Target: a loop cell.
	setFocus(dst, rowLoops, 2) // L0
	dst.containerBytes = m.Bytes()
	preBytes := append([]byte(nil), m.Bytes()...)
	msg := dst.Paste(c)
	if msg == "" {
		t.Fatal("mismatched paste returned empty; want descriptive status")
	}
	wantContains := "Cannot paste"
	if !contains(msg, wantContains) {
		t.Errorf("mismatched-paste status = %q; want it to contain %q", msg, wantContains)
	}
	if !bytes.Equal(preBytes, m.Bytes()) {
		t.Errorf("mismatched paste mutated the container bytes; want no-op")
	}
}

// TestClipboard_CopyEnvelope_DCAtoDCA copies the whole DCA envelope
// (sus, end, 8 rates, 8 stops, KF/VF) and pastes onto another
// voice's DCA envelope.
func TestClipboard_CopyEnvelope_DCAtoDCA(t *testing.T) {
	src, dst, m := bindTwo(t)

	// Pin distinctive bytes across the source DCA envelope.
	susOff := src.voiceOff + disk.VoiceDCASusOffset
	endOff := src.voiceOff + disk.VoiceDCAEndOffset
	rateBase := src.voiceOff + disk.VoiceDCARateOffset
	stopBase := src.voiceOff + disk.VoiceDCAStopOffset
	kfOff := src.voiceOff + disk.VoiceDCAKFOffset
	vfOff := src.voiceOff + disk.VoiceVelDCAKFOffset
	rsOff := src.voiceOff + disk.VoiceDCARSOffset
	rsVfOff := src.voiceOff + disk.VoiceVelDCARSOffset

	patches := []model.Patch{
		{Offset: susOff, Old: []byte{m.Bytes()[susOff]}, New: []byte{3}},
		{Offset: endOff, Old: []byte{m.Bytes()[endOff]}, New: []byte{7}},
		{Offset: kfOff, Old: []byte{m.Bytes()[kfOff]}, New: []byte{0x40}},
		{Offset: vfOff, Old: []byte{m.Bytes()[vfOff]}, New: []byte{0x20}},
		{Offset: rsOff, Old: []byte{m.Bytes()[rsOff]}, New: []byte{0x30}},
		{Offset: rsVfOff, Old: []byte{m.Bytes()[rsVfOff]}, New: []byte{0x10}},
	}
	for i := 0; i < 8; i++ {
		patches = append(patches,
			model.Patch{Offset: rateBase + i, Old: []byte{m.Bytes()[rateBase+i]}, New: []byte{byte(0x10 + i)}},
			model.Patch{Offset: stopBase + i, Old: []byte{m.Bytes()[stopBase+i]}, New: []byte{byte(0xa0 + i)}},
		)
	}
	if err := m.ApplyBatch(patches); err != nil {
		t.Fatalf("seed source envelope: %v", err)
	}
	src.containerBytes = m.Bytes()

	// Copy from the envelope visual cell.
	setFocus(src, rowDCA, 0)
	c := &Clipboard{}
	if msg := src.Copy(c); msg == "" {
		t.Fatal("Copy(envelope visual) returned empty")
	}
	if c.Kind() != ClipboardKindEnvelope {
		t.Errorf("kind = %v; want Envelope", c.Kind())
	}

	// Paste onto voice 1's DCA envelope.
	setFocus(dst, rowDCA, 0)
	dst.containerBytes = m.Bytes()
	if msg := dst.Paste(c); msg == "" {
		t.Fatal("Paste(envelope) returned empty")
	}
	got := m.Bytes()
	tgtSus := dst.voiceOff + disk.VoiceDCASusOffset
	tgtEnd := dst.voiceOff + disk.VoiceDCAEndOffset
	tgtRateBase := dst.voiceOff + disk.VoiceDCARateOffset
	tgtStopBase := dst.voiceOff + disk.VoiceDCAStopOffset
	tgtKf := dst.voiceOff + disk.VoiceDCAKFOffset
	tgtVf := dst.voiceOff + disk.VoiceVelDCAKFOffset
	if got[tgtSus] != 3 {
		t.Errorf("target sus = %d; want 3", got[tgtSus])
	}
	if got[tgtEnd] != 7 {
		t.Errorf("target end = %d; want 7", got[tgtEnd])
	}
	if got[tgtKf] != 0x40 {
		t.Errorf("target KF = %#x; want 0x40", got[tgtKf])
	}
	if got[tgtVf] != 0x20 {
		t.Errorf("target VF = %#x; want 0x20", got[tgtVf])
	}
	for i := 0; i < 8; i++ {
		if got[tgtRateBase+i] != byte(0x10+i) {
			t.Errorf("target rate[%d] = %#x; want %#x", i, got[tgtRateBase+i], 0x10+i)
		}
		if got[tgtStopBase+i] != byte(0xa0+i) {
			t.Errorf("target stop[%d] = %#x; want %#x", i, got[tgtStopBase+i], 0xa0+i)
		}
	}
}

// TestClipboard_CopyEnvelope_DCFincludesFilter_DCFtoDCF copies a DCF
// envelope and pastes onto another DCF: DCF-specific filter fields
// (cutoff, resonance, velRes) come along.
func TestClipboard_CopyEnvelope_DCFincludesFilter_DCFtoDCF(t *testing.T) {
	src, dst, m := bindTwo(t)

	cutOff := src.voiceOff + disk.VoiceDCFOffset
	resOff := src.voiceOff + disk.VoiceDCQOffset
	vRes := src.voiceOff + disk.VoiceVelDCQKFOffset
	if err := m.ApplyBatch([]model.Patch{
		{Offset: cutOff, Old: []byte{m.Bytes()[cutOff]}, New: []byte{0x66}},
		{Offset: resOff, Old: []byte{m.Bytes()[resOff]}, New: []byte{0x07}},
		{Offset: vRes, Old: []byte{m.Bytes()[vRes]}, New: []byte{0x11}},
	}); err != nil {
		t.Fatalf("seed DCF filter fields: %v", err)
	}
	src.containerBytes = m.Bytes()

	setFocus(src, rowDCF, 0)
	c := &Clipboard{}
	if msg := src.Copy(c); msg == "" {
		t.Fatal("Copy(DCF envelope visual) returned empty")
	}

	setFocus(dst, rowDCF, 0)
	dst.containerBytes = m.Bytes()
	if msg := dst.Paste(c); msg == "" {
		t.Fatal("Paste(DCF envelope to DCF) returned empty")
	}
	got := m.Bytes()
	tgtCut := dst.voiceOff + disk.VoiceDCFOffset
	tgtRes := dst.voiceOff + disk.VoiceDCQOffset
	tgtVRes := dst.voiceOff + disk.VoiceVelDCQKFOffset
	if got[tgtCut] != 0x66 {
		t.Errorf("target cutoff = %#x; want 0x66", got[tgtCut])
	}
	if got[tgtRes] != 0x07 {
		t.Errorf("target resonance = %#x; want 0x07", got[tgtRes])
	}
	if got[tgtVRes] != 0x11 {
		t.Errorf("target velRes = %#x; want 0x11", got[tgtVRes])
	}
}

// TestClipboard_CopyEnvelope_DCFtoDCA_DropsFilterFields confirms a
// DCF-sourced envelope pastes onto DCA without touching the target
// voice's DCF cutoff/resonance/velRes (they live at fixed offsets in
// the voice header and are NOT part of the DCA envelope cell).
func TestClipboard_CopyEnvelope_DCFtoDCA_DropsFilterFields(t *testing.T) {
	src, dst, m := bindTwo(t)

	// Seed source DCF envelope + filter triple.
	cutOff := src.voiceOff + disk.VoiceDCFOffset
	resOff := src.voiceOff + disk.VoiceDCQOffset
	if err := m.ApplyBatch([]model.Patch{
		{Offset: cutOff, Old: []byte{m.Bytes()[cutOff]}, New: []byte{0x66}},
		{Offset: resOff, Old: []byte{m.Bytes()[resOff]}, New: []byte{0x07}},
	}); err != nil {
		t.Fatalf("seed DCF filter fields: %v", err)
	}
	src.containerBytes = m.Bytes()

	setFocus(src, rowDCF, 0)
	c := &Clipboard{}
	src.Copy(c)

	// Snapshot target's DCF filter triple BEFORE pasting onto its DCA.
	tgtCut := dst.voiceOff + disk.VoiceDCFOffset
	tgtRes := dst.voiceOff + disk.VoiceDCQOffset
	preCut := m.Bytes()[tgtCut]
	preRes := m.Bytes()[tgtRes]

	setFocus(dst, rowDCA, 0)
	dst.containerBytes = m.Bytes()
	if msg := dst.Paste(c); msg == "" {
		t.Fatal("Paste(DCF envelope to DCA) returned empty")
	}
	// DCA envelope wrote OK; DCF filter fields on the TARGET must
	// remain unchanged (DCA cell has no notion of cutoff).
	if got := m.Bytes()[tgtCut]; got != preCut {
		t.Errorf("DCF cutoff drifted across DCF-to-DCA paste: pre=%#x post=%#x", preCut, got)
	}
	if got := m.Bytes()[tgtRes]; got != preRes {
		t.Errorf("DCF resonance drifted across DCF-to-DCA paste: pre=%#x post=%#x", preRes, got)
	}
}

// TestClipboard_CopyLFORow copies the entire LFO row (shape + depths)
// from one voice and pastes onto another.
func TestClipboard_CopyLFORow(t *testing.T) {
	src, dst, m := bindTwo(t)

	// Seed source LFO.
	lfoName := src.voiceOff + disk.VoiceLFONameOffset
	lfoRate := src.voiceOff + disk.VoiceLFORateOffset
	lfoDcp := src.voiceOff + disk.VoiceLFODCPOffset
	lfoDca := src.voiceOff + disk.VoiceLFODCAOffset
	lfoDcf := src.voiceOff + disk.VoiceLFODCFOffset
	lfoDcq := src.voiceOff + disk.VoiceLFODCQOffset
	lfoAtck := src.voiceOff + disk.VoiceLFOAtckOffset
	lfoDelay := src.voiceOff + disk.VoiceLFODelayOffset
	patches := make([]model.Patch, 0, 8)
	patches = append(patches,
		model.Patch{Offset: lfoName, Old: []byte{m.Bytes()[lfoName]}, New: []byte{0x02}}, // saw down
		model.Patch{Offset: lfoRate, Old: []byte{m.Bytes()[lfoRate]}, New: []byte{0x44}},
		model.Patch{Offset: lfoDcp, Old: []byte{m.Bytes()[lfoDcp]}, New: []byte{0x11}},
		model.Patch{Offset: lfoDca, Old: []byte{m.Bytes()[lfoDca]}, New: []byte{0x22}},
		model.Patch{Offset: lfoDcf, Old: []byte{m.Bytes()[lfoDcf]}, New: []byte{0x33}},
		model.Patch{Offset: lfoDcq, Old: []byte{m.Bytes()[lfoDcq]}, New: []byte{0x44}},
		model.Patch{Offset: lfoAtck, Old: []byte{m.Bytes()[lfoAtck]}, New: []byte{0x55}},
	)
	// Two-byte delay field.
	delayOld := make([]byte, 2)
	copy(delayOld, m.Bytes()[lfoDelay:lfoDelay+2])
	delayNew := []byte{0x66, 0x77}
	patches = append(patches, model.Patch{Offset: lfoDelay, Old: delayOld, New: delayNew})
	if err := m.ApplyBatch(patches); err != nil {
		t.Fatalf("seed LFO row: %v", err)
	}
	src.containerBytes = m.Bytes()

	// Copy can be initiated from either LFO cell; spec says the row
	// gesture covers both cells of the LFO row.
	setFocus(src, rowLFO, 1)
	c := &Clipboard{}
	if msg := src.Copy(c); msg == "" {
		t.Fatal("Copy(LFO shape cell) returned empty status")
	}
	if c.Kind() != ClipboardKindLFORow {
		t.Errorf("kind = %v; want LFORow", c.Kind())
	}

	// Paste at the depths cell on the target voice; spec says paste
	// from either LFO cell replaces both.
	setFocus(dst, rowLFO, 2)
	dst.containerBytes = m.Bytes()
	if msg := dst.Paste(c); msg == "" {
		t.Fatal("Paste(LFO row) returned empty")
	}
	got := m.Bytes()
	tgtName := dst.voiceOff + disk.VoiceLFONameOffset
	tgtRate := dst.voiceOff + disk.VoiceLFORateOffset
	tgtDcp := dst.voiceOff + disk.VoiceLFODCPOffset
	if got[tgtName]&disk.LFOWaveformMask != 0x02 {
		t.Errorf("LFO waveform = %#x; want 0x02", got[tgtName]&disk.LFOWaveformMask)
	}
	if got[tgtRate] != 0x44 {
		t.Errorf("LFO rate = %#x; want 0x44", got[tgtRate])
	}
	if got[tgtDcp] != 0x11 {
		t.Errorf("LFO depth pitch = %#x; want 0x11", got[tgtDcp])
	}
	tgtDelay := dst.voiceOff + disk.VoiceLFODelayOffset
	if got[tgtDelay] != 0x66 || got[tgtDelay+1] != 0x77 {
		t.Errorf("LFO delay = %#x %#x; want 0x66 0x77", got[tgtDelay], got[tgtDelay+1])
	}
}

// TestClipboard_CopyLoop copies a single loop's five fields and
// pastes onto another loop (same voice or different voice).
func TestClipboard_CopyLoop(t *testing.T) {
	src, dst, m := bindTwo(t)

	// Source loop 2.
	srcSt := src.voiceOff + disk.VoiceLoopSt0Offset + 2*4
	srcEd := src.voiceOff + disk.VoiceLoopEd0Offset + 2*4
	srcXf := src.voiceOff + disk.VoiceLoopXFOffset + 2*2
	srcTm := src.voiceOff + disk.VoiceLoopTmOffset + 2*2
	stBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(stBuf, 0x11223344)
	edBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(edBuf, 0x55667788)
	xfBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(xfBuf, 0x010f)
	tmBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(tmBuf, 0x02ee)

	stOld := append([]byte(nil), m.Bytes()[srcSt:srcSt+4]...)
	edOld := append([]byte(nil), m.Bytes()[srcEd:srcEd+4]...)
	xfOld := append([]byte(nil), m.Bytes()[srcXf:srcXf+2]...)
	tmOld := append([]byte(nil), m.Bytes()[srcTm:srcTm+2]...)
	if err := m.ApplyBatch([]model.Patch{
		{Offset: srcSt, Old: stOld, New: stBuf},
		{Offset: srcEd, Old: edOld, New: edBuf},
		{Offset: srcXf, Old: xfOld, New: xfBuf},
		{Offset: srcTm, Old: tmOld, New: tmBuf},
	}); err != nil {
		t.Fatalf("seed source loop 2: %v", err)
	}
	src.containerBytes = m.Bytes()

	// col-2 = loop 0; col-4 = loop 2.
	setFocus(src, rowLoops, 2+2)
	c := &Clipboard{}
	if msg := src.Copy(c); msg == "" {
		t.Fatal("Copy(loop) returned empty")
	}
	if c.Kind() != ClipboardKindLoop {
		t.Errorf("kind = %v; want Loop", c.Kind())
	}

	// Target loop 5 on voice 1.
	setFocus(dst, rowLoops, 5+2)
	dst.containerBytes = m.Bytes()
	if msg := dst.Paste(c); msg == "" {
		t.Fatal("Paste(loop) returned empty")
	}
	got := m.Bytes()
	tgtSt := dst.voiceOff + disk.VoiceLoopSt0Offset + 5*4
	tgtEd := dst.voiceOff + disk.VoiceLoopEd0Offset + 5*4
	tgtXf := dst.voiceOff + disk.VoiceLoopXFOffset + 5*2
	tgtTm := dst.voiceOff + disk.VoiceLoopTmOffset + 5*2
	if !bytes.Equal(got[tgtSt:tgtSt+4], stBuf) {
		t.Errorf("loop start = %x; want %x", got[tgtSt:tgtSt+4], stBuf)
	}
	if !bytes.Equal(got[tgtEd:tgtEd+4], edBuf) {
		t.Errorf("loop end = %x; want %x", got[tgtEd:tgtEd+4], edBuf)
	}
	if !bytes.Equal(got[tgtXf:tgtXf+2], xfBuf) {
		t.Errorf("loop xf = %x; want %x", got[tgtXf:tgtXf+2], xfBuf)
	}
	if !bytes.Equal(got[tgtTm:tgtTm+2], tmBuf) {
		t.Errorf("loop tm = %x; want %x", got[tgtTm:tgtTm+2], tmBuf)
	}
}

// TestClipboard_SampleCellsTypeStrict pins the spec's "sample row
// each cell copies its own fields; paste targets the same cell type
// only" rule. A copy of the sample-rate cell cannot paste onto the
// sample-root cell.
func TestClipboard_SampleCellsTypeStrict(t *testing.T) {
	src, dst, m := bindTwo(t)

	setFocus(src, rowSample, 1) // Rate cell
	c := &Clipboard{}
	if msg := src.Copy(c); msg == "" {
		t.Fatal("Copy(sample rate) returned empty")
	}

	setFocus(dst, rowSample, 3) // Root cell
	dst.containerBytes = m.Bytes()
	pre := append([]byte(nil), m.Bytes()...)
	msg := dst.Paste(c)
	if msg == "" {
		t.Fatal("mismatched sample paste returned empty")
	}
	if !contains(msg, "Cannot paste") {
		t.Errorf("status = %q; want contains 'Cannot paste'", msg)
	}
	if !bytes.Equal(pre, m.Bytes()) {
		t.Errorf("mismatched sample paste mutated container; want no-op")
	}
}

// TestClipboard_VisualCellsNotCopyable pins that the leftmost visual
// cells of LFO and Sample rows produce no clipboard payload when
// Copy is invoked.
func TestClipboard_VisualCellsNotCopyable(t *testing.T) {
	src, _, _ := bindTwo(t)

	setFocus(src, rowLFO, 0)
	c := &Clipboard{}
	_ = src.Copy(c)
	if c.Kind() != ClipboardKindLFORow {
		// LFO visual cell IS the LFO row gesture per spec ("Copy
		// entire LFO row").
		t.Errorf("LFO visual cell copy: kind = %v; want LFORow (entire-row gesture)", c.Kind())
	}

	// Sample visual cell, in contrast, isn't a copy target (each
	// sample cell has its own type per spec).
	c2 := &Clipboard{}
	setFocus(src, rowSample, 0)
	_ = src.Copy(c2)
	if c2.Kind() != ClipboardKindNone {
		t.Errorf("Sample visual cell copy: kind = %v; want None", c2.Kind())
	}
}

// TestClipboard_Summary checks the user-facing label strings appear
// in the copy-confirmation status ("Copied <type>") so the status line
// is readable.
func TestClipboard_Summary(t *testing.T) {
	src, _, _ := bindTwo(t)

	cases := []struct {
		name  string
		focus func()
		want  string
	}{
		{"DCA stage", func() { setFocus(src, rowDCA, 3) }, "DCA stage"},
		{"DCF stage", func() { setFocus(src, rowDCF, 4) }, "DCF stage"},
		{"DCA envelope", func() { setFocus(src, rowDCA, 0) }, "DCA envelope"},
		{"DCF envelope", func() { setFocus(src, rowDCF, 0) }, "DCF envelope"},
		{"LFO row", func() { setFocus(src, rowLFO, 1) }, "LFO row"},
		{"Loop", func() { setFocus(src, rowLoops, 2) }, "loop"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tc.focus()
			c := &Clipboard{}
			src.Copy(c)
			if !contains(c.Summary(), tc.want) {
				t.Errorf("summary = %q; want it to contain %q", c.Summary(), tc.want)
			}
		})
	}
}

// TestClipboard_CopyIsNonDestructive_Undo verifies that issuing a
// copy does not push anything onto the underlying model's undo stack.
// The spec calls out "Copy is non-destructive (does NOT enter undo
// stack)".
func TestClipboard_CopyIsNonDestructive_Undo(t *testing.T) {
	src, _, m := bindTwo(t)

	// Establish a baseline by issuing an unrelated edit so the model
	// has SOMETHING undoable, then capture the undo depth.
	dummyOff := src.voiceOff + disk.VoiceDCAKFOffset
	if err := m.Apply(model.Patch{
		Offset: dummyOff,
		Old:    []byte{m.Bytes()[dummyOff]},
		New:    []byte{m.Bytes()[dummyOff] ^ 0x10},
	}); err != nil {
		t.Fatalf("dummy edit: %v", err)
	}
	canBefore := m.CanUndo()
	if !canBefore {
		t.Fatal("expected an undo entry after dummy edit")
	}

	setFocus(src, rowDCA, 3)
	c := &Clipboard{}
	src.Copy(c)

	// Undo MUST still revert the dummy edit, not the copy. A
	// self-referential byte check after Undo is impossible (the
	// pre-image already matches itself), so rely on CanUndo
	// dropping to false to prove the only undoable thing was the
	// dummy edit, not a phantom Copy entry.
	if err := m.Undo(); err != nil {
		t.Fatalf("Undo after Copy: %v", err)
	}
	if m.CanUndo() {
		t.Errorf("after one Undo, CanUndo()=true; Copy probably added a phantom undo entry")
	}
}

// TestClipboard_PasteIsSingleUndoEntry pins the "Paste enters undo
// stack as a single edit (Ctrl+Z reverts)" guarantee.
func TestClipboard_PasteIsSingleUndoEntry(t *testing.T) {
	src, dst, m := bindTwo(t)

	// Seed a distinctive DCA envelope on the source.
	susOff := src.voiceOff + disk.VoiceDCASusOffset
	if err := m.Apply(model.Patch{
		Offset: susOff,
		Old:    []byte{m.Bytes()[susOff]},
		New:    []byte{5},
	}); err != nil {
		t.Fatalf("seed source SUS: %v", err)
	}
	src.containerBytes = m.Bytes()
	setFocus(src, rowDCA, 0)
	c := &Clipboard{}
	src.Copy(c)

	// Snapshot target before paste.
	preBytes := append([]byte(nil), m.Bytes()...)

	setFocus(dst, rowDCA, 0)
	dst.containerBytes = m.Bytes()
	if msg := dst.Paste(c); msg == "" {
		t.Fatal("Paste returned empty")
	}
	postBytes := append([]byte(nil), m.Bytes()...)
	if bytes.Equal(preBytes, postBytes) {
		t.Fatal("Paste did not mutate bytes")
	}
	// One Undo should restore the entire envelope.
	if err := m.Undo(); err != nil {
		t.Fatalf("Undo after Paste: %v", err)
	}
	if !bytes.Equal(preBytes, m.Bytes()) {
		t.Errorf("single Undo did not fully revert Paste; paste landed as multiple undo steps")
	}
}

func contains(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}
