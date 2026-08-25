package webcore

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
)

func voiceDetail(t *testing.T, s *Session, file string) *VoiceDetail {
	t.Helper()
	snap := s.Snapshot()
	if snap.Disk == nil {
		t.Fatal("no disk")
	}
	for _, f := range snap.Disk.Files {
		if f.Name == file {
			if f.Voice == nil {
				t.Fatalf("file %q has no voice detail", file)
			}
			return f.Voice
		}
	}
	t.Fatalf("file %q not in snapshot", file)
	return nil
}

func TestSnapshotCarriesVoiceDetail(t *testing.T) {
	s, voice := importedSession(t)
	d := voiceDetail(t, s, voice)
	if d.Frames != 3000 {
		t.Fatalf("frames = %d, want 3000", d.Frames)
	}
	if d.SampleRate != 18000 {
		t.Fatalf("rate = %d, want 18000", d.SampleRate)
	}
	if len(d.Loops) != 8 {
		t.Fatalf("loops = %d, want 8", len(d.Loops))
	}
	if len(d.Dca.Rates) != 8 || len(d.Dcf.Stops) != 8 {
		t.Fatal("envelope arrays are not 8 stages")
	}
	for _, r := range d.Dca.Rates {
		if r < 0 || r > 99 {
			t.Fatalf("DCA rate %d outside the display scale", r)
		}
	}
}

// R14's generation start and end on a loose voice file. A standalone
// .fzv starts at sample zero, so no rebase applies, but the same clamp
// to the voice's own frame count does.
func TestSetGenerationRoundTripsAndClamps(t *testing.T) {
	s, voice := importedSession(t)

	if _, cerr := s.SetGeneration(voice, 400, 2500); cerr != nil {
		t.Fatalf("SetGeneration: %v", cerr)
	}
	d := voiceDetail(t, s, voice)
	if d.GenStart != 400 || d.GenEnd != 2500 {
		t.Fatalf("generation = %d..%d, want 400..2500", d.GenStart, d.GenEnd)
	}

	if _, cerr := s.SetGeneration(voice, -10, 99_999); cerr != nil {
		t.Fatalf("SetGeneration clamp: %v", cerr)
	}
	d = voiceDetail(t, s, voice)
	if d.GenStart != 0 || d.GenEnd != d.Frames {
		t.Fatalf("clamped generation = %d..%d, want 0..%d", d.GenStart, d.GenEnd, d.Frames)
	}

	if _, cerr := s.SetGeneration("NOWHERE", 0, 10); cerr == nil || cerr.Code != codeNotFound {
		t.Fatalf("expected not-found for a missing file, got %v", cerr)
	}
}

// R17: whatever the UI displays after an edit is the frame the core
// confirmed.
func TestSetLoopRoundTripsConfirmedFrames(t *testing.T) {
	s, voice := importedSession(t)

	snap, cerr := s.SetLoop(voice, 0, 250, 2200)
	if cerr != nil {
		t.Fatalf("SetLoop: %v", cerr)
	}
	if snap.Revision == 0 {
		t.Fatal("no revision")
	}
	d := voiceDetail(t, s, voice)
	if d.Loops[0].Start != 250 || d.Loops[0].End != 2200 {
		t.Fatalf("loop 0 = %+v, want 250..2200", d.Loops[0])
	}

	// Out-of-range input clamps to the voice's frames; the snapshot
	// reports the clamped truth.
	if _, cerr := s.SetLoop(voice, 1, -50, 99999); cerr != nil {
		t.Fatalf("SetLoop clamp: %v", cerr)
	}
	d = voiceDetail(t, s, voice)
	if d.Loops[1].Start != 0 || d.Loops[1].End != d.Frames {
		t.Fatalf("loop 1 = %+v, want 0..%d", d.Loops[1], d.Frames)
	}

	if _, cerr := s.SetLoop(voice, 9, 0, 10); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("index 9: %v, want invalid-value", cerr)
	}
}

func TestSetLoopSelect(t *testing.T) {
	s, voice := importedSession(t)
	if _, cerr := s.SetLoopSelect(voice, 3, 6); cerr != nil {
		t.Fatalf("SetLoopSelect: %v", cerr)
	}
	d := voiceDetail(t, s, voice)
	if d.LoopSustain != 3 || d.LoopRelease != 6 {
		t.Fatalf("sustain/release = %d/%d, want 3/6", d.LoopSustain, d.LoopRelease)
	}
}

// R16: stage, sustain, and end edits round trip.
func TestSetEnvelopeRoundTrips(t *testing.T) {
	s, voice := importedSession(t)

	rates := []int{10, 20, 30, 40, 50, 60, 70, 80}
	stops := []int{99, 90, 80, 70, 60, 50, 40, 0}
	if _, cerr := s.SetEnvelope(voice, "dca", 3, 7, rates, stops); cerr != nil {
		t.Fatalf("SetEnvelope: %v", cerr)
	}
	d := voiceDetail(t, s, voice)
	if d.Dca.Sustain != 3 || d.Dca.End != 7 {
		t.Fatalf("sustain/end = %d/%d, want 3/7", d.Dca.Sustain, d.Dca.End)
	}
	for i := range rates {
		if d.Dca.Rates[i] != rates[i] {
			t.Fatalf("rate %d = %d, want %d", i, d.Dca.Rates[i], rates[i])
		}
		if d.Dca.Stops[i] != stops[i] {
			t.Fatalf("stop %d = %d, want %d", i, d.Dca.Stops[i], stops[i])
		}
	}

	// DCF takes the same path; out-of-range values clamp.
	if _, cerr := s.SetEnvelope(voice, "dcf", 99, -2, []int{500, 0, 0, 0, 0, 0, 0, 0}, stops); cerr != nil {
		t.Fatalf("SetEnvelope dcf: %v", cerr)
	}
	d = voiceDetail(t, s, voice)
	if d.Dcf.Sustain != 7 || d.Dcf.End != 0 {
		t.Fatalf("dcf sustain/end = %d/%d, want clamped 7/0", d.Dcf.Sustain, d.Dcf.End)
	}
	if d.Dcf.Rates[0] != 99 {
		t.Fatalf("dcf rate 0 = %d, want clamped 99", d.Dcf.Rates[0])
	}

	if _, cerr := s.SetEnvelope(voice, "dcx", 0, 0, rates, stops); cerr == nil || cerr.Code != codeInvalidField {
		t.Fatalf("dcx: %v, want invalid-field", cerr)
	}
	if _, cerr := s.SetEnvelope(voice, "dca", 0, 0, []int{1, 2}, stops); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("short rates: %v, want invalid-value", cerr)
	}
}

// Loop and envelope edits join the same undo history as everything
// else.
func TestLoopAndEnvelopeEditsUndo(t *testing.T) {
	s, voice := importedSession(t)
	before := voiceDetail(t, s, voice).Loops[0]

	if _, cerr := s.SetLoop(voice, 0, 100, 900); cerr != nil {
		t.Fatalf("SetLoop: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	after := voiceDetail(t, s, voice).Loops[0]
	if after != before {
		t.Fatalf("undo did not restore loop 0: %+v then %+v", before, after)
	}
}

func displayRates(b [disk.EnvelopeStages]uint8) []int {
	out := make([]int, disk.EnvelopeStages)
	for i := range b {
		out[i] = disk.RateByteToDisplay(b[i])
	}
	return out
}

func displayStops(b [disk.EnvelopeStages]uint8) []int {
	out := make([]int, disk.EnvelopeStages)
	for i := range b {
		out[i] = disk.StopByteToDisplay(b[i])
	}
	return out
}

// 28 of 128 rate bytes and 156 of 256 stop bytes fail a display round
// trip, so writing every stage from display values changes stages the
// user never moved. Only the stages that actually changed get patched.
func TestEnvelopePatchesOnlyChangedStages(t *testing.T) {
	// A stop byte that doesn't survive a display round trip. 156 of the
	// 256 possible stop bytes fail it; this is one of them.
	const lossyStop = 2
	if disk.StopDisplayToByte(disk.StopByteToDisplay(lossyStop)) == lossyStop {
		t.Fatalf("stop byte %d round trips cleanly, so this test proves nothing", lossyStop)
	}

	vp := &fzvinfo.VoiceParams{}
	for i := range vp.DCARates {
		vp.DCARates[i] = 127
		vp.DCAStops[i] = lossyStop
	}

	rates := displayRates(vp.DCARates)
	stops := displayStops(vp.DCAStops)
	rates[0] = 50 // move exactly one stage's rate

	build, cerr := buildEnvelopePatches(envDCA, 0, 7, rates, stops)
	if cerr != nil {
		t.Fatalf("buildEnvelopePatches: %v", cerr)
	}
	patches, err := build(vp)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	sawStage0 := false
	for _, p := range patches {
		switch {
		case p.Offset >= disk.VoiceDCAStopOffset && p.Offset < disk.VoiceDCAStopOffset+disk.EnvelopeStages:
			t.Errorf("stop stage %d was patched, but no stop changed", p.Offset-disk.VoiceDCAStopOffset)
		case p.Offset == disk.VoiceDCARateOffset:
			sawStage0 = true
		case p.Offset > disk.VoiceDCARateOffset && p.Offset < disk.VoiceDCARateOffset+disk.EnvelopeStages:
			t.Errorf("rate stage %d was patched, but only stage 0 changed", p.Offset-disk.VoiceDCARateOffset)
		}
	}
	if !sawStage0 {
		t.Error("the edited stage was not patched")
	}
}
