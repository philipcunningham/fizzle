package webcore

import (
	"encoding/binary"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// LoopSnapshot is one row of a voice's 8-loop table, in sample frames
// with the spec's flag bits masked.
type LoopSnapshot struct {
	Start int `json:"start"`
	End   int `json:"end"`
	XF    int `json:"xf"`
	Tm    int `json:"tm"`
}

// EnvelopeSnapshot carries one envelope in the hardware display scale
// (0 to 99 for rates and stops), the same units the setters accept.
type EnvelopeSnapshot struct {
	Sustain int   `json:"sustain"`
	End     int   `json:"end"`
	Rates   []int `json:"rates"`
	Stops   []int `json:"stops"`
}

// VoiceDetail is the bespoke-editor surface of a voice: the waveform
// extent, the generation window, the loop table, and both envelopes
// (R14's Sample group, R16, R17).
//
// GenStart and GenEnd are R14's generation start and end, in
// voice-relative sample frames like every other position the boundary
// carries. They sit here rather than in the schema because their bounds
// are this voice's own frame count: a schema field declares one static
// range for every voice, and a gened past waved would play the samples
// of whatever follows in the audio area.
type VoiceDetail struct {
	Frames      int              `json:"frames"`
	SampleRate  int              `json:"sampleRate"`
	GenStart    int              `json:"genStart"`
	GenEnd      int              `json:"genEnd"`
	LoopSustain int              `json:"loopSustain"`
	LoopRelease int              `json:"loopRelease"`
	Loops       []LoopSnapshot   `json:"loops"`
	Dca         EnvelopeSnapshot `json:"dca"`
	Dcf         EnvelopeSnapshot `json:"dcf"`
}

// voiceDetailFrom maps parsed voice values to the detail snapshot.
func voiceDetailFrom(vp *fzvinfo.VoiceParams) *VoiceDetail {
	loops := make([]LoopSnapshot, len(vp.AllLoops))
	for i, l := range vp.AllLoops {
		loops[i] = LoopSnapshot{Start: int(l.Start), End: int(l.End), XF: int(l.XF), Tm: int(l.Tm)}
	}
	env := func(sustain, end uint8, rates, stops [disk.EnvelopeStages]uint8) EnvelopeSnapshot {
		out := EnvelopeSnapshot{
			Sustain: int(sustain),
			End:     int(end),
			Rates:   make([]int, disk.EnvelopeStages),
			Stops:   make([]int, disk.EnvelopeStages),
		}
		for i := 0; i < disk.EnvelopeStages; i++ {
			out.Rates[i] = disk.RateByteToDisplay(rates[i])
			out.Stops[i] = disk.StopByteToDisplay(stops[i])
		}
		return out
	}
	// fzvinfo already localises the generation pointers against the
	// voice's wave start, so they arrive voice-relative whether the
	// voice came from a loose file or a slot in a dump.
	return &VoiceDetail{
		Frames:      int(vp.Samples),
		SampleRate:  int(vp.SampleRate),
		GenStart:    int(vp.GenStart),
		GenEnd:      int(vp.GenEnd),
		LoopSustain: int(vp.LoopSustain),
		LoopRelease: int(vp.LoopRelease),
		Loops:       loops,
		Dca:         env(vp.DCASustain, vp.DCAEnd, vp.DCARates, vp.DCAStops),
		Dcf:         env(vp.DCFSustain, vp.DCFEnd, vp.DCFRates, vp.DCFStops),
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// clampGeneration holds a generation window inside the voice it
// belongs to. Spec section 2-1 requires wavst <= genst <= gened <=
// waved, so the start clamps to the voice's frames and the end clamps
// up to the start rather than inverting the pair. A gened past waved
// would read on into whatever sits next in the audio area.
func clampGeneration(frames, startFrame, endFrame int) (start, end int) {
	start = clampInt(startFrame, 0, frames)
	return start, clampInt(endFrame, start, frames)
}

// generationPatches writes the generation window's two 4-byte cells.
// base is the voice's own wave start, which is zero for a standalone
// file and the slot's absolute address inside a dump.
func generationPatches(base uint32, start, end int) []voiceedit.Patch {
	cell := func(offset int, addr uint32) voiceedit.Patch {
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, addr)
		return voiceedit.Patch{Offset: offset, Bytes: buf}
	}
	// #nosec G115 -- start and end are clamped non-negative, and to the
	// voice's own frame count, by clampGeneration.
	return []voiceedit.Patch{
		cell(disk.VoiceGenStartOffset, base+uint32(start)),
		cell(disk.VoiceGenEndOffset, base+uint32(end)),
	}
}

// SetGeneration sets a voice file's generation window (R14's generation
// start and end) in sample frames, clamped to the voice's own extent.
// A standalone voice starts at sample zero, so the cells already hold
// the frames the boundary speaks and no rebase applies.
func (s *Session) SetGeneration(fileName string, startFrame, endFrame int) (Snapshot, *Error) {
	return s.patchVoice(fileName, func(voiceBytes []byte) ([]voiceedit.Patch, error) {
		vp, err := fzvinfo.ParseBytes(voiceBytes)
		if err != nil {
			return nil, err
		}
		start, end := clampGeneration(int(vp.Samples), startFrame, endFrame)
		return generationPatches(0, start, end), nil
	})
}

// SetLoop sets loop index's start and end on a voice, clamped to the
// voice's frame count. The snapshot reports the frames the core
// confirmed (R17).
func (s *Session) SetLoop(fileName string, index, startFrame, endFrame int) (Snapshot, *Error) {
	if index < 0 || index >= disk.MaxGenerators {
		return s.Snapshot(), errf(codeInvalidValue, "loop index must be 0 to %d, got %d", disk.MaxGenerators-1, index)
	}
	return s.patchVoice(fileName, func(voiceBytes []byte) ([]voiceedit.Patch, error) {
		vp, err := fzvinfo.ParseBytes(voiceBytes)
		if err != nil {
			return nil, err
		}
		frames := int(vp.Samples)
		start := clampInt(startFrame, 0, frames-1)
		end := clampInt(endFrame, start+1, frames)
		stOff := disk.VoiceLoopSt0Offset + index*4
		edOff := disk.VoiceLoopEd0Offset + index*4
		origSt := leU32(voiceBytes[stOff : stOff+4])
		origEd := leU32(voiceBytes[edOff : edOff+4])
		// #nosec G115 -- start and end are clamped non-negative above.
		return voiceedit.BuildLoopPatch(index, uint32(start), uint32(end), origSt, origEd)
	})
}

// SetLoopSelect sets the sustain and release loop designations,
// clamped to 0..8 where 8 means none.
func (s *Session) SetLoopSelect(fileName string, sustain, release int) (Snapshot, *Error) {
	sustain = clampInt(sustain, 0, disk.NoSustainLoop)
	release = clampInt(release, 0, disk.NoSustainLoop)
	return s.patchVoice(fileName, func([]byte) ([]voiceedit.Patch, error) {
		return voiceedit.BuildLoopSelectPatch(sustain, release)
	})
}

// Envelope names the boundary accepts.
const (
	envDCA = "dca"
	envDCF = "dcf"
)

// SetEnvelope sets a whole envelope (which is "dca" or "dcf"): the
// sustain and end stage designations plus all eight rates and stops in
// the hardware display scale, clamped (R16).
func (s *Session) SetEnvelope(fileName, which string, sustain, end int, rates, stops []int) (Snapshot, *Error) {
	if which != envDCA && which != envDCF {
		return s.Snapshot(), errf("invalid-field", "envelope must be dca or dcf, got %q", which)
	}
	if len(rates) != disk.EnvelopeStages || len(stops) != disk.EnvelopeStages {
		return s.Snapshot(), errf(codeInvalidValue, "envelopes carry %d stages", disk.EnvelopeStages)
	}
	sustain = clampInt(sustain, 0, disk.EnvelopeStages-1)
	end = clampInt(end, 0, disk.EnvelopeStages-1)
	var r, st [disk.EnvelopeStages]int
	for i, v := range rates {
		r[i] = clampInt(v, 0, 99)
	}
	for i, v := range stops {
		st[i] = clampInt(v, 0, 99)
	}
	return s.patchVoice(fileName, func(voiceBytes []byte) ([]voiceedit.Patch, error) {
		vp, err := fzvinfo.ParseBytes(voiceBytes)
		if err != nil {
			return nil, err
		}
		if which == envDCA {
			return voiceedit.BuildDCAPatches(sustain, end, r, st, vp.DCARates)
		}
		return voiceedit.BuildDCFPatches(sustain, end, r, st, vp.DCFRates)
	})
}

func leU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
