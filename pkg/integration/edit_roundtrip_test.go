package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"

	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"

	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

func TestEditFZFVoiceRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")
	if err := sfzconvert.Convert(context.Background(), "../../testdata/synthetic/JUNGLISM.sfz", fzfPath, 36000, false); err != nil {
		t.Fatalf("sfz convert: %v", err)
	}

	audioBefore := fzfAudioHash(t, fzfPath)

	patches, err := voiceedit.BuildLFOPatches(3, 25, 10, 20, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	filterPatches, err := voiceedit.BuildFilterPatches(64, 7)
	if err != nil {
		t.Fatal(err)
	}
	all := make([]voiceedit.Edit, 0, len(patches)+len(filterPatches))
	all = append(all, patches...)
	all = append(all, filterPatches...)

	if err := voiceedit.ApplyToFZFVoice(fzfPath, "REESE", all); err != nil {
		t.Fatal(err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	reese, err := fzvinfo.Parse(filepath.Join(unpackDir, "REESE.fzv"))
	if err != nil {
		t.Fatal(err)
	}
	if reese.LFOWaveform != "Triangle" {
		t.Errorf("REESE waveform: got %q, want Triangle", reese.LFOWaveform)
	}
	if reese.LFORate != 25 {
		t.Errorf("REESE LFO rate: got %d, want 25", reese.LFORate)
	}
	if reese.LFODepthFilter != 50 {
		t.Errorf("REESE LFO filter: got %d, want 50", reese.LFODepthFilter)
	}
	if reese.FilterCutoff != 64 {
		t.Errorf("REESE cutoff: got %d, want 64", reese.FilterCutoff)
	}
	if reese.FilterQ != 7 {
		t.Errorf("REESE resonance: got %d, want 7", reese.FilterQ)
	}

	amen, err := fzvinfo.Parse(filepath.Join(unpackDir, "AMEN 01.fzv"))
	if err != nil {
		t.Fatal(err)
	}
	if amen.LFORate != 0 {
		t.Errorf("AMEN 01 LFO rate should be unchanged, got %d", amen.LFORate)
	}
	if amen.FilterCutoff != disk.DCFMaxOffset {
		t.Errorf("AMEN 01 cutoff should be unchanged, got %d", amen.FilterCutoff)
	}

	audioAfter := fzfAudioHash(t, fzfPath)
	if audioBefore != audioAfter {
		t.Error("audio data should be unchanged after parameter edit")
	}
}

func TestEditFZFVoiceNameRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")
	if err := sfzconvert.Convert(context.Background(), "../../testdata/synthetic/JUNGLISM.sfz", fzfPath, 36000, false); err != nil {
		t.Fatalf("sfz convert: %v", err)
	}

	namePatches, err := voiceedit.BuildNamePatch("JUNGLE BASS")
	if err != nil {
		t.Fatal(err)
	}
	if err := voiceedit.ApplyToFZFVoice(fzfPath, "REESE", namePatches); err != nil {
		t.Fatal(err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "JUNGLE BASS") {
			found = true
			p, err := fzvinfo.Parse(filepath.Join(unpackDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if p.Name != "JUNGLE BASS" {
				t.Errorf("name: got %q, want JUNGLE BASS", p.Name)
			}
			if p.HasActiveLoop != false {
				t.Error("one-shot voice should not have active loop after name edit")
			}
			break
		}
	}
	if !found {
		t.Error("JUNGLE BASS.fzv not found after rename")
	}
}

func TestEditVoiceInImageRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()

	fzfPath := filepath.Join(dir, "junglism.fzf")
	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzfPath, 9000, false); err != nil {
		t.Fatalf("sfz convert: %v", err)
	}

	imgPath := filepath.Join(dir, "junglism.img")
	if err := diskformat.Format(imgPath, "JUNGLISM"); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(imgPath, fzfPath, 0); err != nil {
		t.Fatal(err)
	}

	extractedFZF := filepath.Join(dir, "extracted.fzf")
	if err := diskget.Get(imgPath, "FULL-DATA-FZ", extractedFZF); err != nil {
		t.Fatal(err)
	}
	audioBefore := fzfAudioHash(t, extractedFZF)

	patches, err := voiceedit.BuildLFOPatches(3, 25, 10, 20, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	filterPatches, err := voiceedit.BuildFilterPatches(64, 7)
	if err != nil {
		t.Fatal(err)
	}
	all := make([]voiceedit.Edit, 0, len(patches)+len(filterPatches))
	all = append(all, patches...)
	all = append(all, filterPatches...)

	if err := voiceedit.ApplyToFZFVoice(extractedFZF, "REESE", all); err != nil {
		t.Fatal(err)
	}

	editedFZF, err := os.ReadFile(extractedFZF)
	if err != nil {
		t.Fatal(err)
	}
	if err := diskadd.ReplaceOnImage(imgPath, "FULL-DATA-FZ", editedFZF, 0); err != nil {
		t.Fatal(err)
	}

	reExtracted := filepath.Join(dir, "re-extracted.fzf")
	if err := diskget.Get(imgPath, "FULL-DATA-FZ", reExtracted); err != nil {
		t.Fatal(err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(reExtracted, unpackDir); err != nil {
		t.Fatal(err)
	}

	reese, err := fzvinfo.Parse(filepath.Join(unpackDir, "REESE.fzv"))
	if err != nil {
		t.Fatal(err)
	}
	if reese.LFOWaveform != "Triangle" {
		t.Errorf("REESE waveform: got %q, want Triangle", reese.LFOWaveform)
	}
	if reese.LFORate != 25 {
		t.Errorf("REESE LFO rate: got %d, want 25", reese.LFORate)
	}
	if reese.LFODepthFilter != 50 {
		t.Errorf("REESE LFO filter depth: got %d, want 50", reese.LFODepthFilter)
	}
	if reese.FilterCutoff != 64 {
		t.Errorf("REESE cutoff: got %d, want 64", reese.FilterCutoff)
	}
	if reese.FilterQ != 7 {
		t.Errorf("REESE resonance: got %d, want 7", reese.FilterQ)
	}

	audioAfter := fzfAudioHash(t, reExtracted)
	if audioBefore != audioAfter {
		t.Error("audio data should be unchanged after parameter edit and replace")
	}
}

func TestEditDCAEnvelopeRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatal(err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}
	fzvPath := filepath.Join(unpackDir, "BRASS1 D3 1.fzv")

	origP, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	rates := [disk.EnvelopeStages]int{50, 30, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged}
	stops := [disk.EnvelopeStages]int{99, 50, 0, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged}
	patches, err := voiceedit.BuildDCAPatches(0, 7, rates, stops, origP.DCARates)
	if err != nil {
		t.Fatal(err)
	}
	if err := voiceedit.ApplyToFZV(fzvPath, patches); err != nil {
		t.Fatal(err)
	}

	p, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	if p.DCASustain != 0 {
		t.Errorf("DCASustain = %d, want 0", p.DCASustain)
	}
	if p.DCAEnd != 7 {
		t.Errorf("DCAEnd = %d, want 7", p.DCAEnd)
	}
	if disk.RateByteToDisplay(p.DCARates[0]) != 50 {
		t.Errorf("rate[0] display = %d, want 50", disk.RateByteToDisplay(p.DCARates[0]))
	}
	if disk.RateByteToDisplay(p.DCARates[1]) != 30 {
		t.Errorf("rate[1] display = %d, want 30", disk.RateByteToDisplay(p.DCARates[1]))
	}
	if disk.StopByteToDisplay(p.DCAStops[0]) != 99 {
		t.Errorf("stop[0] display = %d, want 99", disk.StopByteToDisplay(p.DCAStops[0]))
	}
}

func TestEditDCFEnvelopeRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatal(err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}
	fzvPath := filepath.Join(unpackDir, "BRASS1 D3 1.fzv")

	origP, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	rates := [disk.EnvelopeStages]int{40, 20, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged}
	stops := [disk.EnvelopeStages]int{79, 60, 0, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged}
	patches, err := voiceedit.BuildDCFPatches(1, 5, rates, stops, origP.DCFRates)
	if err != nil {
		t.Fatal(err)
	}
	if err := voiceedit.ApplyToFZV(fzvPath, patches); err != nil {
		t.Fatal(err)
	}

	p, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	if p.DCFSustain != 1 {
		t.Errorf("DCFSustain = %d, want 1", p.DCFSustain)
	}
	if p.DCFEnd != 5 {
		t.Errorf("DCFEnd = %d, want 5", p.DCFEnd)
	}
	if disk.RateByteToDisplay(p.DCFRates[0]) != 40 {
		t.Errorf("rate[0] display = %d, want 40", disk.RateByteToDisplay(p.DCFRates[0]))
	}
	if disk.RateByteToDisplay(p.DCFRates[1]) != 20 {
		t.Errorf("rate[1] display = %d, want 20", disk.RateByteToDisplay(p.DCFRates[1]))
	}
	if disk.StopByteToDisplay(p.DCFStops[0]) != 79 {
		t.Errorf("stop[0] display = %d, want 79", disk.StopByteToDisplay(p.DCFStops[0]))
	}
	if p.DCFStops[2] != 0 {
		t.Errorf("stop[2] = %d, want 0", p.DCFStops[2])
	}
}

func TestBrassHardwareDisplayValues(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, brassImg, "BRASS1 D3 1")

	if disk.RateByteToDisplay(p.DCARates[0]) != 99 {
		t.Errorf("DCA rate[0] display = %d, want 99", disk.RateByteToDisplay(p.DCARates[0]))
	}
	if disk.StopByteToDisplay(p.DCAStops[0]) != 85 {
		t.Errorf("DCA stop[0] display = %d, want 85", disk.StopByteToDisplay(p.DCAStops[0]))
	}
	if p.DCAStops[1] != 255 {
		t.Errorf("DCA stop[1] byte = %d, want 255", p.DCAStops[1])
	}
	if disk.StopByteToDisplay(p.DCAStops[1]) != 99 {
		t.Errorf("DCA stop[1] display = %d, want 99", disk.StopByteToDisplay(p.DCAStops[1]))
	}
	if disk.RateByteToDisplay(p.DCFRates[0]) != 99 {
		t.Errorf("DCF rate[0] display = %d, want 99", disk.RateByteToDisplay(p.DCFRates[0]))
	}
	if p.DCFStops[0] != 66 {
		t.Errorf("DCF stop[0] byte = %d, want 66", p.DCFStops[0])
	}
	if disk.StopByteToDisplay(p.DCFStops[0]) != 26 {
		t.Errorf("DCF stop[0] display = %d, want 26", disk.StopByteToDisplay(p.DCFStops[0]))
	}
	if p.DCFStops[1] != 56 {
		t.Errorf("DCF stop[1] byte = %d, want 56", p.DCFStops[1])
	}
	if disk.StopByteToDisplay(p.DCFStops[1]) != 22 {
		t.Errorf("DCF stop[1] display = %d, want 22", disk.StopByteToDisplay(p.DCFStops[1]))
	}
}

func fzfAudioHash(t *testing.T, fzfPath string) string {
	t.Helper()
	data, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	voiceSectors := disk.VoiceAreaSectors(hdr.NVoice)
	audioStart := hdr.VoiceAreaStart + voiceSectors*disk.SectorSize
	if audioStart >= len(data) {
		t.Fatal("no audio data in FZF")
	}
	h := sha256.Sum256(data[audioStart:])
	return hex.EncodeToString(h[:])
}
