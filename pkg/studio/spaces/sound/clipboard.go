package sound

import (
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// Status / label strings shared across Copy and Paste status paths.
// Hoisted into constants so the goconst linter doesn't flag every
// summary string, and so a future label tweak lands in one place.
const (
	labelDCA         = "DCA"
	labelDCF         = "DCF"
	labelDCAStage    = "DCA stage"
	labelDCFStage    = "DCF stage"
	labelDCAEnvelope = "DCA envelope"
	labelDCFEnvelope = "DCF envelope"
	labelLFORow      = "LFO row"
	labelLoop        = "loop"
	msgPasteNoChange = "Paste: no change"
)

// ClipboardKind discriminates the typed payload a Clipboard holds.
// Paste rejects targets whose kind does not match. Mixing DCA and
// DCF stages or envelopes is allowed (envelopes share Sus / End /
// Rates / Stops; stages share Role / Rate / Stop level), so those
// are NOT separate kinds; the envelope payload tracks whether the
// source was DCA or DCF so DCF-only filter fields can be dropped on
// a cross-paste.
type ClipboardKind int

// Clipboard kinds, in spec-table order.
const (
	// ClipboardKindNone is the empty / no-payload state.
	ClipboardKindNone ClipboardKind = iota
	// ClipboardKindStage carries a single DCA or DCF stage cell
	// (Role + Rate + Stop level).
	ClipboardKindStage
	// ClipboardKindEnvelope carries the whole DCA or DCF envelope
	// (Sus, End, 8 rates, 8 stops, KF/VF; DCF also carries cutoff,
	// resonance, velRes).
	ClipboardKindEnvelope
	// ClipboardKindLFORow carries the entire LFO row (waveform +
	// rate + attack + delay + 4 depths).
	ClipboardKindLFORow
	// ClipboardKindLoop carries a single loop's five fields (start,
	// end, crossfade, time, fine).
	ClipboardKindLoop
	// Sample row: each cell has its own type. Paste targets the
	// same cell only.
	clipboardKindSampleRate
	clipboardKindSampleWave
	clipboardKindSampleRoot
	clipboardKindSampleMode
)

// Clipboard is the studio sound-space typed clipboard. One instance
// lives on App; Sound.Copy populates it and Sound.Paste consumes it.
// The payload is opaque to App: a kind enum plus the raw bytes of the
// copied region (with sub-discriminators for envelope source row and
// stage role).
type Clipboard struct {
	kind ClipboardKind

	// stageRole captures the source stage's role (0 normal, 1 SUS,
	// 2 END) so Paste can transfer the role to the target stage.
	stageRole int
	// stageRate / stageStop are the raw byte values copied from the
	// source stage's rate and stop-level slots.
	stageRate byte
	stageStop byte

	// envelopeFromDCF is true when the envelope payload was copied
	// from a DCF cell; the cutoff / resonance / velRes bytes carry
	// meaningful values only in that case.
	envelopeFromDCF bool
	envelopeSus     byte
	envelopeEnd     byte
	envelopeRates   [8]byte
	envelopeStops   [8]byte
	envelopeKF      byte
	envelopeVF      byte
	envelopeRS      byte
	envelopeRSVF    byte
	// DCF-only filter triple. Zero / unused for DCA-sourced
	// envelopes; envelopeFromDCF gates whether Paste writes them.
	envelopeCutoff byte
	envelopeRes    byte
	envelopeVelRes byte

	// LFO row payload. The waveform lives in the low bits of the
	// LFOName byte; we store the whole byte to preserve any unused
	// high-bit content the firmware may set.
	lfoName  byte
	lfoRate  byte
	lfoAtck  byte
	lfoDelay [2]byte
	lfoDcp   byte
	lfoDca   byte
	lfoDcf   byte
	lfoDcq   byte

	// Loop payload: the four loop fields packed into raw bytes so
	// fine / skip flag bits survive the copy/paste cycle.
	loopStart [4]byte
	loopEnd   [4]byte
	loopXf    [2]byte
	loopTm    [2]byte

	// Sample-cell payloads. Each one stashes the raw bytes of the
	// cell so paste is a memcpy onto the same fields on the target.
	sampleByte byte
	sampleWave [8]byte // 4 bytes wavst + 4 bytes waved
	sampleMode [2]byte // 2-byte little-endian loopmode
}

// Kind returns the clipboard's payload discriminator. ClipboardKindNone
// when empty.
func (c *Clipboard) Kind() ClipboardKind {
	if c == nil {
		return ClipboardKindNone
	}
	return c.kind
}

// Summary returns a short human-readable description of what is on
// the clipboard, suitable for the status line.
func (c *Clipboard) Summary() string {
	if c == nil || c.kind == ClipboardKindNone {
		return ""
	}
	return "Clipboard: " + kindLabel(c.kind, c.envelopeFromDCF)
}

// kindLabel returns the user-facing label for a clipboard kind, used
// when reporting a mismatched paste.
func kindLabel(k ClipboardKind, fromDCF bool) string {
	switch k {
	case ClipboardKindNone:
		return "(empty)"
	case ClipboardKindStage:
		if fromDCF {
			return labelDCFStage
		}
		return labelDCAStage
	case ClipboardKindEnvelope:
		if fromDCF {
			return labelDCFEnvelope
		}
		return labelDCAEnvelope
	case ClipboardKindLFORow:
		return labelLFORow
	case ClipboardKindLoop:
		return labelLoop
	case clipboardKindSampleRate:
		return "sample rate"
	case clipboardKindSampleWave:
		return "sample wave bounds"
	case clipboardKindSampleRoot:
		return "sample root"
	case clipboardKindSampleMode:
		return "sample mode"
	}
	return "?"
}

// cellClipboardKind reports the clipboard kind a Copy at the focused
// cell would produce, and whether the source is a DCF cell. Returns
// (ClipboardKindNone, false) for cells with no copy gesture (notably
// the DCA/DCF level-KF/VF and rate-KF/VF cells, the DCF cutoff/res
// cell, and the sample / loops visual cells).
func cellClipboardKind(r row, col int) (ClipboardKind, bool) {
	switch r {
	case rowDCA:
		switch {
		case col == 0:
			return ClipboardKindEnvelope, false
		case col >= 3:
			return ClipboardKindStage, false
		}
	case rowDCF:
		switch {
		case col == 0:
			return ClipboardKindEnvelope, true
		case col >= 4:
			return ClipboardKindStage, true
		}
	case rowLFO:
		// The LFO row's three cells (visual, shape, depths) all
		// belong to one copy gesture.
		return ClipboardKindLFORow, false
	case rowSample:
		switch col {
		case 1:
			return clipboardKindSampleRate, false
		case 2:
			return clipboardKindSampleWave, false
		case 3:
			return clipboardKindSampleRoot, false
		case 5:
			return clipboardKindSampleMode, false
		}
	case rowLoops:
		if col >= 2 {
			return ClipboardKindLoop, false
		}
	case numRows:
		// sentinel
	}
	return ClipboardKindNone, false
}

// Copy reads the focused cell's editable state into c and returns a
// status line describing the result. A no-op cell (one without a
// copy gesture, e.g. the loops visual cell) leaves c untouched and
// returns the empty string.
func (sm *Model) Copy(c *Clipboard) string {
	if c == nil || !sm.hasVoice {
		return ""
	}
	kind, fromDCF := cellClipboardKind(sm.row, sm.col)
	if kind == ClipboardKindNone {
		return ""
	}
	data := sm.containerBytes
	switch kind {
	case ClipboardKindStage:
		return sm.copyStage(c, fromDCF, data)
	case ClipboardKindEnvelope:
		return sm.copyEnvelope(c, fromDCF, data)
	case ClipboardKindLFORow:
		return sm.copyLFORow(c, data)
	case ClipboardKindLoop:
		return sm.copyLoop(c, data)
	case clipboardKindSampleRate, clipboardKindSampleRoot:
		return sm.copySampleByte(c, kind, data)
	case clipboardKindSampleWave:
		return sm.copySampleWave(c, data)
	case clipboardKindSampleMode:
		return sm.copySampleMode(c, data)
	case ClipboardKindNone:
		// Unreachable due to early return above; kept exhaustive.
	}
	return ""
}

// Paste applies the clipboard's payload to the focused cell, returning
// a status line. If the clipboard's kind does not match what the
// focused cell expects, Paste reports the mismatch and leaves the
// container untouched (Copy is non-destructive, Paste of an
// incompatible payload is also non-destructive).
func (sm *Model) Paste(c *Clipboard) string {
	if c == nil || c.kind == ClipboardKindNone || !sm.hasVoice {
		return ""
	}
	tgtKind, tgtFromDCF := cellClipboardKind(sm.row, sm.col)
	if tgtKind == ClipboardKindNone {
		return fmt.Sprintf("Cannot paste %s here", kindLabel(c.kind, c.envelopeFromDCF))
	}
	if tgtKind != c.kind {
		return fmt.Sprintf("Cannot paste %s into %s",
			kindLabel(c.kind, c.envelopeFromDCF),
			kindLabel(tgtKind, tgtFromDCF))
	}
	switch c.kind {
	case ClipboardKindStage:
		return sm.pasteStage(c, tgtFromDCF)
	case ClipboardKindEnvelope:
		return sm.pasteEnvelope(c, tgtFromDCF)
	case ClipboardKindLFORow:
		return sm.pasteLFORow(c)
	case ClipboardKindLoop:
		return sm.pasteLoop(c)
	case clipboardKindSampleRate, clipboardKindSampleRoot:
		return sm.pasteSampleByte(c)
	case clipboardKindSampleWave:
		return sm.pasteSampleWave(c)
	case clipboardKindSampleMode:
		return sm.pasteSampleMode(c)
	case ClipboardKindNone:
		// Unreachable: gated by the c.kind == None early return.
	}
	return ""
}

// ---- stage --------------------------------------------------------

// stageOffsets returns (rate, stop, sus, end) byte offsets for the
// stage at the given (row, col). Caller has already verified the
// focused cell is a stage cell.
func (sm *Model) stageOffsets(r row, col int) (rateOff, stopOff, susOff, endOff, stageIdx int) {
	if r == rowDCA {
		stageIdx = col - 3
		return sm.voiceOff + disk.VoiceDCARateOffset + stageIdx,
			sm.voiceOff + disk.VoiceDCAStopOffset + stageIdx,
			sm.voiceOff + disk.VoiceDCASusOffset,
			sm.voiceOff + disk.VoiceDCAEndOffset,
			stageIdx
	}
	stageIdx = col - 4
	return sm.voiceOff + disk.VoiceDCFRateOffset + stageIdx,
		sm.voiceOff + disk.VoiceDCFStopOffset + stageIdx,
		sm.voiceOff + disk.VoiceDCFSusOffset,
		sm.voiceOff + disk.VoiceDCFEndOffset,
		stageIdx
}

func (sm *Model) copyStage(c *Clipboard, fromDCF bool, data []byte) string {
	rateOff, stopOff, susOff, endOff, stageIdx := sm.stageOffsets(sm.row, sm.col)
	c.kind = ClipboardKindStage
	c.envelopeFromDCF = fromDCF
	c.stageRate = data[rateOff]
	c.stageStop = data[stopOff]
	switch {
	case int(data[susOff]) == stageIdx:
		c.stageRole = 1
	case int(data[endOff]) == stageIdx:
		c.stageRole = 2
	default:
		c.stageRole = 0
	}
	return c.Summary()
}

func (sm *Model) pasteStage(c *Clipboard, tgtFromDCF bool) string {
	rateOff, stopOff, susOff, endOff, stageIdx := sm.stageOffsets(sm.row, sm.col)
	data := sm.containerBytes
	patches := []model.Patch{}
	if data[rateOff] != c.stageRate {
		patches = append(patches, model.Patch{
			Offset: rateOff,
			Old:    []byte{data[rateOff]},
			New:    []byte{c.stageRate},
		})
	}
	if data[stopOff] != c.stageStop {
		patches = append(patches, model.Patch{
			Offset: stopOff,
			Old:    []byte{data[stopOff]},
			New:    []byte{c.stageStop},
		})
	}
	// Role transfer: SUS / END carry to the target stage so the
	// envelope pointer points at the new stage. Normal-role copies
	// leave the target's sus/end pointers untouched.
	switch c.stageRole {
	case 1:
		stageByte := byte(stageIdx) //nolint:gosec // G115: stageIdx is 0..7 (envelope stage index)
		if data[susOff] != stageByte {
			patches = append(patches, model.Patch{
				Offset: susOff,
				Old:    []byte{data[susOff]},
				New:    []byte{stageByte},
			})
		}
	case 2:
		stageByte := byte(stageIdx) //nolint:gosec // G115: stageIdx is 0..7 (envelope stage index)
		if data[endOff] != stageByte {
			patches = append(patches, model.Patch{
				Offset: endOff,
				Old:    []byte{data[endOff]},
				New:    []byte{stageByte},
			})
		}
	}
	if len(patches) == 0 {
		return msgPasteNoChange
	}
	if err := sm.m.ApplyBatch(patches); err != nil {
		return fmt.Sprintf("Paste failed: %v", err)
	}
	sm.containerBytes = sm.m.Bytes()
	return fmt.Sprintf("Pasted %s stage onto %s stage %d",
		envelopeShortLabel(c.envelopeFromDCF),
		envelopeShortLabel(tgtFromDCF),
		stageIdx+1)
}

// envelopeShortLabel returns "DCA" or "DCF" for a fromDCF flag.
// Hoisted out so paste-status formatting reads in one line.
func envelopeShortLabel(fromDCF bool) string {
	if fromDCF {
		return labelDCF
	}
	return labelDCA
}

// ---- envelope -----------------------------------------------------

func (sm *Model) envelopeOffsets(fromDCF bool) (susOff, endOff, rateBase, stopBase, kfOff, vfOff, rsOff, rsVfOff int) {
	if fromDCF {
		return sm.voiceOff + disk.VoiceDCFSusOffset,
			sm.voiceOff + disk.VoiceDCFEndOffset,
			sm.voiceOff + disk.VoiceDCFRateOffset,
			sm.voiceOff + disk.VoiceDCFStopOffset,
			sm.voiceOff + disk.VoiceDCFKFOffset,
			sm.voiceOff + disk.VoiceVelDCFKFOffset,
			sm.voiceOff + disk.VoiceDCFRSOffset,
			sm.voiceOff + disk.VoiceVelDCFRSOffset
	}
	return sm.voiceOff + disk.VoiceDCASusOffset,
		sm.voiceOff + disk.VoiceDCAEndOffset,
		sm.voiceOff + disk.VoiceDCARateOffset,
		sm.voiceOff + disk.VoiceDCAStopOffset,
		sm.voiceOff + disk.VoiceDCAKFOffset,
		sm.voiceOff + disk.VoiceVelDCAKFOffset,
		sm.voiceOff + disk.VoiceDCARSOffset,
		sm.voiceOff + disk.VoiceVelDCARSOffset
}

func (sm *Model) copyEnvelope(c *Clipboard, fromDCF bool, data []byte) string {
	susOff, endOff, rateBase, stopBase, kfOff, vfOff, rsOff, rsVfOff := sm.envelopeOffsets(fromDCF)
	c.kind = ClipboardKindEnvelope
	c.envelopeFromDCF = fromDCF
	c.envelopeSus = data[susOff]
	c.envelopeEnd = data[endOff]
	for i := 0; i < 8; i++ {
		c.envelopeRates[i] = data[rateBase+i]
		c.envelopeStops[i] = data[stopBase+i]
	}
	c.envelopeKF = data[kfOff]
	c.envelopeVF = data[vfOff]
	c.envelopeRS = data[rsOff]
	c.envelopeRSVF = data[rsVfOff]
	if fromDCF {
		c.envelopeCutoff = data[sm.voiceOff+disk.VoiceDCFOffset]
		c.envelopeRes = data[sm.voiceOff+disk.VoiceDCQOffset]
		c.envelopeVelRes = data[sm.voiceOff+disk.VoiceVelDCQKFOffset]
	}
	return c.Summary()
}

func (sm *Model) pasteEnvelope(c *Clipboard, tgtFromDCF bool) string {
	susOff, endOff, rateBase, stopBase, kfOff, vfOff, rsOff, rsVfOff := sm.envelopeOffsets(tgtFromDCF)
	data := sm.containerBytes
	patches := []model.Patch{}
	add := func(off int, newByte byte) {
		if data[off] == newByte {
			return
		}
		patches = append(patches, model.Patch{
			Offset: off,
			Old:    []byte{data[off]},
			New:    []byte{newByte},
		})
	}
	add(susOff, c.envelopeSus)
	add(endOff, c.envelopeEnd)
	for i := 0; i < 8; i++ {
		add(rateBase+i, c.envelopeRates[i])
		add(stopBase+i, c.envelopeStops[i])
	}
	add(kfOff, c.envelopeKF)
	add(vfOff, c.envelopeVF)
	add(rsOff, c.envelopeRS)
	add(rsVfOff, c.envelopeRSVF)
	// DCF-to-DCF carries the filter triple. DCF-to-DCA or
	// DCA-to-anything must NOT touch the DCF filter fields on the
	// target voice (they live outside the DCA envelope cell).
	if c.envelopeFromDCF && tgtFromDCF {
		add(sm.voiceOff+disk.VoiceDCFOffset, c.envelopeCutoff)
		add(sm.voiceOff+disk.VoiceDCQOffset, c.envelopeRes)
		add(sm.voiceOff+disk.VoiceVelDCQKFOffset, c.envelopeVelRes)
	}
	if len(patches) == 0 {
		return msgPasteNoChange
	}
	if err := sm.m.ApplyBatch(patches); err != nil {
		return fmt.Sprintf("Paste failed: %v", err)
	}
	sm.containerBytes = sm.m.Bytes()
	return fmt.Sprintf("Pasted %s envelope onto %s envelope",
		envelopeShortLabel(c.envelopeFromDCF),
		envelopeShortLabel(tgtFromDCF))
}

// ---- LFO row ------------------------------------------------------

func (sm *Model) copyLFORow(c *Clipboard, data []byte) string {
	c.kind = ClipboardKindLFORow
	c.envelopeFromDCF = false
	c.lfoName = data[sm.voiceOff+disk.VoiceLFONameOffset]
	c.lfoRate = data[sm.voiceOff+disk.VoiceLFORateOffset]
	c.lfoAtck = data[sm.voiceOff+disk.VoiceLFOAtckOffset]
	delayAbs := sm.voiceOff + disk.VoiceLFODelayOffset
	c.lfoDelay[0] = data[delayAbs]
	c.lfoDelay[1] = data[delayAbs+1]
	c.lfoDcp = data[sm.voiceOff+disk.VoiceLFODCPOffset]
	c.lfoDca = data[sm.voiceOff+disk.VoiceLFODCAOffset]
	c.lfoDcf = data[sm.voiceOff+disk.VoiceLFODCFOffset]
	c.lfoDcq = data[sm.voiceOff+disk.VoiceLFODCQOffset]
	return c.Summary()
}

func (sm *Model) pasteLFORow(c *Clipboard) string {
	data := sm.containerBytes
	patches := []model.Patch{}
	add := func(off int, newByte byte) {
		if data[off] == newByte {
			return
		}
		patches = append(patches, model.Patch{
			Offset: off,
			Old:    []byte{data[off]},
			New:    []byte{newByte},
		})
	}
	addBytes := func(off int, newBytes []byte) {
		if data[off] == newBytes[0] && data[off+1] == newBytes[1] {
			return
		}
		old := make([]byte, len(newBytes))
		copy(old, data[off:off+len(newBytes)])
		patches = append(patches, model.Patch{
			Offset: off,
			Old:    old,
			New:    append([]byte(nil), newBytes...),
		})
	}
	add(sm.voiceOff+disk.VoiceLFONameOffset, c.lfoName)
	add(sm.voiceOff+disk.VoiceLFORateOffset, c.lfoRate)
	add(sm.voiceOff+disk.VoiceLFOAtckOffset, c.lfoAtck)
	addBytes(sm.voiceOff+disk.VoiceLFODelayOffset, c.lfoDelay[:])
	add(sm.voiceOff+disk.VoiceLFODCPOffset, c.lfoDcp)
	add(sm.voiceOff+disk.VoiceLFODCAOffset, c.lfoDca)
	add(sm.voiceOff+disk.VoiceLFODCFOffset, c.lfoDcf)
	add(sm.voiceOff+disk.VoiceLFODCQOffset, c.lfoDcq)
	if len(patches) == 0 {
		return msgPasteNoChange
	}
	if err := sm.m.ApplyBatch(patches); err != nil {
		return fmt.Sprintf("Paste failed: %v", err)
	}
	sm.containerBytes = sm.m.Bytes()
	return "Pasted LFO row"
}

// ---- loop ---------------------------------------------------------

func (sm *Model) copyLoop(c *Clipboard, data []byte) string {
	loopIdx := sm.col - 2
	stOff := sm.voiceOff + disk.VoiceLoopSt0Offset + loopIdx*4
	edOff := sm.voiceOff + disk.VoiceLoopEd0Offset + loopIdx*4
	xfOff := sm.voiceOff + disk.VoiceLoopXFOffset + loopIdx*2
	tmOff := sm.voiceOff + disk.VoiceLoopTmOffset + loopIdx*2
	c.kind = ClipboardKindLoop
	c.envelopeFromDCF = false
	copy(c.loopStart[:], data[stOff:stOff+4])
	copy(c.loopEnd[:], data[edOff:edOff+4])
	copy(c.loopXf[:], data[xfOff:xfOff+2])
	copy(c.loopTm[:], data[tmOff:tmOff+2])
	return c.Summary()
}

func (sm *Model) pasteLoop(c *Clipboard) string {
	loopIdx := sm.col - 2
	stOff := sm.voiceOff + disk.VoiceLoopSt0Offset + loopIdx*4
	edOff := sm.voiceOff + disk.VoiceLoopEd0Offset + loopIdx*4
	xfOff := sm.voiceOff + disk.VoiceLoopXFOffset + loopIdx*2
	tmOff := sm.voiceOff + disk.VoiceLoopTmOffset + loopIdx*2
	data := sm.containerBytes
	patches := []model.Patch{}
	addRange := func(off int, newBytes []byte) {
		eq := true
		for i, b := range newBytes {
			if data[off+i] != b {
				eq = false
				break
			}
		}
		if eq {
			return
		}
		old := make([]byte, len(newBytes))
		copy(old, data[off:off+len(newBytes)])
		patches = append(patches, model.Patch{
			Offset: off,
			Old:    old,
			New:    append([]byte(nil), newBytes...),
		})
	}
	addRange(stOff, c.loopStart[:])
	addRange(edOff, c.loopEnd[:])
	addRange(xfOff, c.loopXf[:])
	addRange(tmOff, c.loopTm[:])
	if len(patches) == 0 {
		return msgPasteNoChange
	}
	if err := sm.m.ApplyBatch(patches); err != nil {
		return fmt.Sprintf("Paste failed: %v", err)
	}
	sm.containerBytes = sm.m.Bytes()
	return fmt.Sprintf("Pasted loop onto loop %d", loopIdx+1)
}

// ---- sample cells -------------------------------------------------

// sampleByteOffset returns the absolute offset of the single-byte
// sample field for the given clipboard sub-kind. Caller has already
// verified kind is one of the byte-valued sample kinds (Rate, Root);
// other kinds map to -1 so the caller can detect a routing mistake.
//
//nolint:exhaustive // only byte-valued sample kinds are valid inputs here; the wave / mode / non-sample kinds fall through to the default
func (sm *Model) sampleByteOffset(k ClipboardKind) int {
	switch k {
	case clipboardKindSampleRate:
		return sm.voiceOff + disk.VoiceSampOffset
	case clipboardKindSampleRoot:
		return sm.voiceOff + disk.VoiceKeyCentOffset
	default:
		return -1
	}
}

func (sm *Model) copySampleByte(c *Clipboard, k ClipboardKind, data []byte) string {
	off := sm.sampleByteOffset(k)
	c.kind = k
	c.envelopeFromDCF = false
	c.sampleByte = data[off]
	return c.Summary()
}

func (sm *Model) pasteSampleByte(c *Clipboard) string {
	off := sm.sampleByteOffset(c.kind)
	data := sm.containerBytes
	if data[off] == c.sampleByte {
		return msgPasteNoChange
	}
	if err := sm.m.Apply(model.Patch{
		Offset: off,
		Old:    []byte{data[off]},
		New:    []byte{c.sampleByte},
	}); err != nil {
		return fmt.Sprintf("Paste failed: %v", err)
	}
	sm.containerBytes = sm.m.Bytes()
	return fmt.Sprintf("Pasted %s", kindLabel(c.kind, false))
}

func (sm *Model) copySampleWave(c *Clipboard, data []byte) string {
	startOff := sm.voiceOff + disk.VoiceWaveStartOffset
	c.kind = clipboardKindSampleWave
	c.envelopeFromDCF = false
	copy(c.sampleWave[:], data[startOff:startOff+8])
	return c.Summary()
}

func (sm *Model) pasteSampleWave(c *Clipboard) string {
	startOff := sm.voiceOff + disk.VoiceWaveStartOffset
	data := sm.containerBytes
	eq := true
	for i := 0; i < 8; i++ {
		if data[startOff+i] != c.sampleWave[i] {
			eq = false
			break
		}
	}
	if eq {
		return msgPasteNoChange
	}
	old := make([]byte, 8)
	copy(old, data[startOff:startOff+8])
	if err := sm.m.Apply(model.Patch{
		Offset: startOff,
		Old:    old,
		New:    append([]byte(nil), c.sampleWave[:]...),
	}); err != nil {
		return fmt.Sprintf("Paste failed: %v", err)
	}
	sm.containerBytes = sm.m.Bytes()
	return "Pasted sample wave bounds"
}

func (sm *Model) copySampleMode(c *Clipboard, data []byte) string {
	off := sm.voiceOff + disk.VoiceLoopModeOffset
	c.kind = clipboardKindSampleMode
	c.envelopeFromDCF = false
	c.sampleMode[0] = data[off]
	c.sampleMode[1] = data[off+1]
	return c.Summary()
}

func (sm *Model) pasteSampleMode(c *Clipboard) string {
	off := sm.voiceOff + disk.VoiceLoopModeOffset
	data := sm.containerBytes
	if data[off] == c.sampleMode[0] && data[off+1] == c.sampleMode[1] {
		return msgPasteNoChange
	}
	old := make([]byte, 2)
	copy(old, data[off:off+2])
	if err := sm.m.Apply(model.Patch{
		Offset: off,
		Old:    old,
		New:    append([]byte(nil), c.sampleMode[:]...),
	}); err != nil {
		return fmt.Sprintf("Paste failed: %v", err)
	}
	sm.containerBytes = sm.m.Bytes()
	return "Pasted sample mode"
}
