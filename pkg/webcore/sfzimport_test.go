package webcore

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

// sfzFolder builds a boundary file map: one .sfz mapping each WAV to
// one key from note 36 up.
func sfzFolder(t *testing.T, samplesPerWAV int, wavNames ...string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	var sfz strings.Builder
	for i, name := range wavNames {
		files["wavs/"+name] = wavBytes(t, samplesPerWAV)
		note := 36 + i
		fmt.Fprintf(&sfz, "<region>\nsample=wavs/%s\nlokey=%d hikey=%d pitch_keycenter=%d\n\n", name, note, note, note)
	}
	files["kit.sfz"] = []byte(sfz.String())
	return files
}

// R7 SFZ row: converts to the instrument, byte identical to the CLI
// pipeline on the same filesystem.
func TestImportSFZReplacesInstrument(t *testing.T) {
	s := twoVoiceSession(t)
	files := sfzFolder(t, 2000, "kick.wav", "snare.wav")

	res, cerr := s.ImportSFZ(files, "", 18000, false, false, ChannelMix)
	if cerr != nil {
		t.Fatalf("ImportSFZ: %v", cerr)
	}
	if res.Rate != 18000 {
		t.Errorf("rate = %d, want 18000", res.Rate)
	}
	inst := instrument(t, s)
	if got := voiceNames(inst); len(got) != 2 || got[0] != "KICK" || got[1] != "SNARE" {
		t.Fatalf("voices = %v, want [KICK SNARE]", got)
	}

	want, err := sfzconvert.ConvertFS(context.Background(), mapFS(files), "kit.sfz", 18000, false, voiceimport.ChannelMix)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dumpBytes(t, s), want.FZF) {
		t.Error("placed dump differs from the CLI pipeline's output")
	}

	// The replace is one undo step back to the old instrument.
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if got := voiceNames(instrument(t, s)); len(got) != 2 || got[0] != voiceLow {
		t.Fatalf("after undo, voices = %v", got)
	}
}

// R9: fit to disk steps the rate down and reports what it used.
func TestImportSFZFitToDiskStepsDown(t *testing.T) {
	s := NewSession()
	files := sfzFolder(t, 300000, "a.wav", "b.wav", "c.wav")

	res, cerr := s.ImportSFZ(files, "kit.sfz", 18000, true, false, ChannelMix)
	if cerr != nil {
		t.Fatalf("ImportSFZ: %v", cerr)
	}
	if res.Rate >= 18000 {
		t.Errorf("rate = %d, want stepped down below 18000", res.Rate)
	}
	if got := res.Snapshot.Disk.Disks; got != 1 {
		t.Errorf("disks = %d, want 1 after fit to disk", got)
	}
}

// R9: the two disk split lands as an image pair, byte identical to
// the CLI's split build.
func TestImportSFZSplitBuildsPair(t *testing.T) {
	s := NewSession()
	files := sfzFolder(t, 300000, "a.wav", "b.wav", "c.wav")

	res, cerr := s.ImportSFZ(files, "kit.sfz", 18000, false, true, ChannelMix)
	if cerr != nil {
		t.Fatalf("ImportSFZ split: %v", cerr)
	}
	if got := res.Snapshot.Disk.Disks; got != 2 {
		t.Fatalf("disks = %d, want 2", got)
	}

	want, err := sfzconvert.ConvertMultiDiskFS(context.Background(), mapFS(files), "kit.sfz", 18000, voiceimport.ChannelMix)
	if err != nil {
		t.Fatal(err)
	}
	for i := range want.Disks {
		image, cerr := s.ExportImageAt(i)
		if cerr != nil {
			t.Fatalf("ExportImageAt(%d): %v", i, cerr)
		}
		if !bytes.Equal(payload(t, image), want.Disks[i]) {
			t.Errorf("disk %d payload differs from the CLI split", i+1)
		}
	}
}

// R9: a missing referenced sample fails with an error naming it, and
// the document is untouched.
func TestImportSFZMissingSampleNamesIt(t *testing.T) {
	s := twoVoiceSession(t)
	before := mustExport(t, s)
	files := map[string][]byte{
		"kit.sfz": []byte("<region>\nsample=wavs/gone.wav lokey=36 hikey=36 pitch_keycenter=36\n"),
	}

	_, cerr := s.ImportSFZ(files, "", 18000, false, false, ChannelMix)
	if cerr == nil || cerr.Code != "missing-samples" {
		t.Fatalf("expected missing-samples, got %v", cerr)
	}
	if !strings.Contains(cerr.Message, "gone.wav") {
		t.Errorf("error should name the missing sample: %s", cerr.Message)
	}
	if !bytes.Equal(mustExport(t, s), before) {
		t.Error("a failed import must not change the document")
	}
}

func TestImportSFZFolderShapeErrors(t *testing.T) {
	s := NewSession()
	if _, cerr := s.ImportSFZ(map[string][]byte{"x.wav": wavBytes(t, 10)}, "", 18000, false, false, ChannelMix); cerr == nil || cerr.Code != "no-sfz" {
		t.Fatalf("expected no-sfz, got %v", cerr)
	}
	files := map[string][]byte{"a.sfz": []byte("x"), "b.sfz": []byte("x")}
	if _, cerr := s.ImportSFZ(files, "", 18000, false, false, ChannelMix); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("expected invalid-value for two .sfz files, got %v", cerr)
	}
	if _, cerr := s.ImportSFZ(files, "a.sfz", 18000, true, true, ChannelMix); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("expected invalid-value for fit plus split, got %v", cerr)
	}
}

// R8: a folder of WAVs maps sequentially up the keyboard from C2,
// byte identical to the CLI's zero SFZ pipeline.
func TestImportWAVFolderSequentialKeys(t *testing.T) {
	s := NewSession()
	files := map[string][]byte{
		"03 ride.wav": wavBytes(t, 1200),
		"01 kick.wav": wavBytes(t, 1000),
		"02 snap.wav": wavBytes(t, 1100),
	}

	res, cerr := s.ImportWAVFolder(files, 18000, false, ChannelMix)
	if cerr != nil {
		t.Fatalf("ImportWAVFolder: %v", cerr)
	}
	inst := instrument(t, s)
	if got := voiceNames(inst); len(got) != 3 || got[0] != "01 KICK" {
		t.Fatalf("voices = %v, want sorted [01 KICK 02 SNAP 03 RIDE]", got)
	}
	areas := inst.Banks[0].Areas
	for i, area := range areas {
		if area.KeyLow != 36+i || area.KeyHigh != 36+i {
			t.Errorf("area %d keys = %d..%d, want %d", i, area.KeyLow, area.KeyHigh, 36+i)
		}
	}
	if res.Rate != 18000 {
		t.Errorf("rate = %d, want 18000", res.Rate)
	}

	want, err := sfzconvert.ConvertDirFS(context.Background(), mapFS(files), 18000, false, voiceimport.ChannelMix)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dumpBytes(t, s), want.FZF) {
		t.Error("placed dump differs from the CLI pipeline's output")
	}
}

// stereoWAVBytes builds a 16-bit interleaved stereo WAV by hand
// (wav.Write refuses stereo). Frame i carries left = 100+i and right
// = 200+i, so the channel the caller chose is visible in the voice.
func stereoWAVBytes(frames int) []byte {
	const channels, bytesPerSample = 2, 2
	dataSize := uint32(frames * channels * bytesPerSample) //nolint:gosec // G115: test value, bounded by frames
	var buf bytes.Buffer
	put32 := func(v uint32) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	put16 := func(v uint16) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	puti16 := func(v int16) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	buf.WriteString("RIFF")
	put32(36 + dataSize)
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	put32(16)
	put16(1) // PCM
	put16(channels)
	put32(18000)
	put32(18000 * channels * bytesPerSample)
	put16(channels * bytesPerSample)
	put16(16)
	buf.WriteString("data")
	put32(dataSize)
	for i := range frames {
		puti16(int16(100 + i)) //nolint:gosec // G115: test value, bounded by frames
		puti16(int16(200 + i)) //nolint:gosec // G115: test value, bounded by frames
	}
	return buf.Bytes()
}

// The stereo answer the dialog collected must reach the samples, and it
// covers the whole batch.
func TestImportWAVFolderCarriesTheChannelChoice(t *testing.T) {
	const frames = 400
	for _, tc := range []struct {
		channel string
		want    func(i int) int16
	}{
		{ChannelLeft, func(i int) int16 { return int16(100 + i) }},  //nolint:gosec // G115: bounded by frames
		{ChannelRight, func(i int) int16 { return int16(200 + i) }}, //nolint:gosec // G115: bounded by frames
		{ChannelMix, func(i int) int16 { return int16(150 + i) }},   //nolint:gosec // G115: bounded by frames
	} {
		t.Run(tc.channel, func(t *testing.T) {
			s := NewSession()
			files := map[string][]byte{"stereo.wav": stereoWAVBytes(frames)}

			if _, cerr := s.ImportWAVFolder(files, 18000, false, tc.channel); cerr != nil {
				t.Fatalf("ImportWAVFolder: %v", cerr)
			}
			voices, _, err := voiceunpack.UnpackDataFromBytes(dumpBytes(t, s))
			if err != nil {
				t.Fatalf("UnpackDataFromBytes: %v", err)
			}
			_, got, err := voiceextract.Decode(voices[0])
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(got) != frames {
				t.Fatalf("samples = %d, want %d (the interleaved length is %d)", len(got), frames, frames*2)
			}
			for i, sample := range got {
				if sample != tc.want(i) {
					t.Fatalf("sample %d = %d, want %d", i, sample, tc.want(i))
				}
			}
		})
	}
}

// The same on the SFZ route, which is what the browser's folder drop
// takes and what J2 opens with. Without the answer, the same stereo
// bytes give a voice of twice the frames, the two channels alternating.
func TestImportSFZCarriesTheChannelChoice(t *testing.T) {
	const frames = 400
	for _, tc := range []struct {
		channel string
		want    func(i int) int16
	}{
		{ChannelLeft, func(i int) int16 { return int16(100 + i) }},  //nolint:gosec // G115: bounded by frames
		{ChannelRight, func(i int) int16 { return int16(200 + i) }}, //nolint:gosec // G115: bounded by frames
		{ChannelMix, func(i int) int16 { return int16(150 + i) }},   //nolint:gosec // G115: bounded by frames
	} {
		t.Run(tc.channel, func(t *testing.T) {
			s := NewSession()
			files := map[string][]byte{
				"stereo.wav": stereoWAVBytes(frames),
				"kit.sfz":    []byte("<region>\nsample=stereo.wav\nlokey=36 hikey=36 pitch_keycenter=36\n"),
			}

			if _, cerr := s.ImportSFZ(files, "", 18000, false, false, tc.channel); cerr != nil {
				t.Fatalf("ImportSFZ: %v", cerr)
			}
			voices, _, err := voiceunpack.UnpackDataFromBytes(dumpBytes(t, s))
			if err != nil {
				t.Fatalf("UnpackDataFromBytes: %v", err)
			}
			_, got, err := voiceextract.Decode(voices[0])
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(got) != frames {
				t.Fatalf("samples = %d, want %d (the interleaved length is %d)", len(got), frames, frames*2)
			}
			for i, sample := range got {
				if sample != tc.want(i) {
					t.Fatalf("sample %d = %d, want %d", i, sample, tc.want(i))
				}
			}
		})
	}
}

// The SFZ route refuses an answer it doesn't know, as the folder route
// does, rather than standing one of the three in for it.
func TestImportSFZRejectsAnUnknownChannel(t *testing.T) {
	s := twoVoiceSession(t)
	before := mustExport(t, s)
	files := sfzFolder(t, 100, "kick.wav")

	_, cerr := s.ImportSFZ(files, "", 18000, false, false, "middle")
	if cerr == nil || cerr.Code != codeInvalidChannel {
		t.Fatalf("expected invalid-channel, got %v", cerr)
	}
	if !bytes.Equal(mustExport(t, s), before) {
		t.Error("a refused import must not change the document")
	}
}

// An answer the boundary does not know is refused rather than
// silently defaulted to one of the three.
func TestImportWAVFolderRejectsAnUnknownChannel(t *testing.T) {
	s := twoVoiceSession(t)
	before := mustExport(t, s)

	_, cerr := s.ImportWAVFolder(map[string][]byte{"a.wav": wavBytes(t, 100)}, 18000, false, "middle")
	if cerr == nil || cerr.Code != codeInvalidChannel {
		t.Fatalf("expected invalid-channel, got %v", cerr)
	}
	if !strings.Contains(cerr.Message, "middle") {
		t.Errorf("error should name the answer it refused: %s", cerr.Message)
	}
	if !bytes.Equal(mustExport(t, s), before) {
		t.Error("a refused import must not change the document")
	}
}
