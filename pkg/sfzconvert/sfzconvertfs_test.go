package sfzconvert

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/sfz"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// TestConvertFSMatchesConvert pins the in-memory pipeline to the path
// pipeline byte for byte on the JUNGLISM fixture.
func TestConvertFSMatchesConvert(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "path.fzf")
	if err := Convert(context.Background(), junglismSFZ, out, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	res, err := ConvertFS(context.Background(), os.DirFS("../../testdata/synthetic"), "JUNGLISM.sfz", 36000, false, voiceimport.ChannelMonoOnly)
	if err != nil {
		t.Fatalf("ConvertFS: %v", err)
	}
	if res.Rate != 36000 {
		t.Errorf("rate = %d, want 36000", res.Rate)
	}
	if !bytes.Equal(res.FZF, want) {
		t.Errorf("ConvertFS output differs from Convert: %d vs %d bytes", len(res.FZF), len(want))
	}
}

// TestConvertDirFSMatchesConvertDir pins the zero-SFZ drum kit path.
func TestConvertDirFSMatchesConvertDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range 3 {
		testutil.WriteTestWAV(t, filepath.Join(dir, fmt.Sprintf("v%02d.wav", i)), 36000, 2000+i*100)
	}

	out := filepath.Join(t.TempDir(), "kit.fzf")
	if err := ConvertDir(context.Background(), dir, out, 18000, false); err != nil {
		t.Fatalf("ConvertDir: %v", err)
	}
	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	res, err := ConvertDirFS(context.Background(), os.DirFS(dir), 18000, false, voiceimport.ChannelMonoOnly)
	if err != nil {
		t.Fatalf("ConvertDirFS: %v", err)
	}
	if !bytes.Equal(res.FZF, want) {
		t.Errorf("ConvertDirFS output differs from ConvertDir: %d vs %d bytes", len(res.FZF), len(want))
	}
}

// stereoWAVBytes builds a 16-bit interleaved stereo PCM WAV in memory.
// Frame i carries left = 100+i and right = 200+i, so each channel
// choice shows in the decoded voice: left yields the 100s, right the
// 200s, mix their average, the 150s. wav.Write refuses stereo, so the
// header is laid out by hand.
func stereoWAVBytes(sampleRate uint32, frames int) []byte {
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
	put32(sampleRate)
	put32(sampleRate * channels * bytesPerSample)
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

// Fixture names and rate shared across the stereo cases.
const (
	stereoWAV   = "stereo.wav"
	sampleWAV   = "s.wav"
	fixtureRate = 18000
)

// monoWAVBytes builds a 16-bit mono PCM WAV in memory at the fixture
// rate, the counterpart to stereoWAVBytes for the cases that compare
// the two.
func monoWAVBytes(samples int) []byte {
	pcm := make([]int16, samples)
	for i := range pcm {
		pcm[i] = int16(i % 1000) //nolint:gosec // G115: test value below 1000
	}
	var buf bytes.Buffer
	if err := wav.Write(&buf, &wav.File{SampleRate: fixtureRate, Samples: pcm, LoopStart: -1, LoopEnd: -1}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// TestConvertDirFSStereoReducesToChosenChannel covers B2: the folder
// import asks for left, right, or mix once and that answer must reach
// the samples. An unreduced stereo file resamples as one interleaved
// stream, so the voice comes out twice as long and an octave low with
// the channels alternating sample by sample.
func TestConvertDirFSStereoReducesToChosenChannel(t *testing.T) {
	t.Parallel()
	const frames = 512
	data := stereoWAVBytes(18000, frames)

	cases := []struct {
		name    string
		channel voiceimport.Channel
		want    func(i int) int16
	}{
		{"left", voiceimport.ChannelLeft, func(i int) int16 { return int16(100 + i) }},   //nolint:gosec // G115: bounded by frames
		{"right", voiceimport.ChannelRight, func(i int) int16 { return int16(200 + i) }}, //nolint:gosec // G115: bounded by frames
		{"mix", voiceimport.ChannelMix, func(i int) int16 { return int16(150 + i) }},     //nolint:gosec // G115: bounded by frames
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := fstest.MapFS{stereoWAV: &fstest.MapFile{Data: data}}
			// Source and target rate match, so Resample copies and any
			// difference in the output is the channel reduction alone.
			res, err := ConvertDirFS(context.Background(), fsys, 18000, false, tc.channel)
			if err != nil {
				t.Fatalf("ConvertDirFS: %v", err)
			}
			voices, _, err := voiceunpack.UnpackDataFromBytes(res.FZF)
			if err != nil {
				t.Fatalf("UnpackDataFromBytes: %v", err)
			}
			if len(voices) != 1 {
				t.Fatalf("voices = %d, want 1", len(voices))
			}
			_, got, err := voiceextract.Decode(voices[0])
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(got) != frames {
				t.Fatalf("samples = %d, want %d (the interleaved length is %d)", len(got), frames, frames*2)
			}
			for i, s := range got {
				if s != tc.want(i) {
					t.Fatalf("sample %d = %d, want %d", i, s, tc.want(i))
				}
			}
		})
	}
}

// TestConvertDirFSStereoWithoutAChoiceIsRefused covers the other half
// of B2: a caller that never asked must not get a silent default.
func TestConvertDirFSStereoWithoutAChoiceIsRefused(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{stereoWAV: &fstest.MapFile{Data: stereoWAVBytes(18000, 256)}}
	_, err := ConvertDirFS(context.Background(), fsys, 18000, false, voiceimport.ChannelMonoOnly)
	if err == nil {
		t.Fatal("expected an error for stereo input with no channel choice")
	}
	if !strings.Contains(err.Error(), "stereo.wav") {
		t.Errorf("error should name the stereo file: %v", err)
	}
}

// stereoKitFS builds a one region SFZ over a single stereo sample.
func stereoKitFS(frames int) fstest.MapFS {
	return fstest.MapFS{
		stereoWAV: &fstest.MapFile{Data: stereoWAVBytes(18000, frames)},
		"kit.sfz": &fstest.MapFile{Data: []byte(
			"<region>\nsample=stereo.wav\nlokey=36\nhikey=36\npitch_keycenter=36\n",
		)},
	}
}

// TestConvertFSStereoReducesToChosenChannel covers B2 on the SFZ route
// the browser uses. Before the channel was threaded through, the
// interleaved buffer resampled as one stream: 1024 samples where 512
// frames went in, the two channels alternating (100, 200, 101, 201).
func TestConvertFSStereoReducesToChosenChannel(t *testing.T) {
	t.Parallel()
	const frames = 512

	cases := []struct {
		name    string
		channel voiceimport.Channel
		want    func(i int) int16
	}{
		{"left", voiceimport.ChannelLeft, func(i int) int16 { return int16(100 + i) }},   //nolint:gosec // G115: bounded by frames
		{"right", voiceimport.ChannelRight, func(i int) int16 { return int16(200 + i) }}, //nolint:gosec // G115: bounded by frames
		{"mix", voiceimport.ChannelMix, func(i int) int16 { return int16(150 + i) }},     //nolint:gosec // G115: bounded by frames
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Source and target rate match, so Resample copies and any
			// difference in the output is the channel reduction alone.
			res, err := ConvertFS(context.Background(), stereoKitFS(frames), "kit.sfz", 18000, false, tc.channel)
			if err != nil {
				t.Fatalf("ConvertFS: %v", err)
			}
			voices, _, err := voiceunpack.UnpackDataFromBytes(res.FZF)
			if err != nil {
				t.Fatalf("UnpackDataFromBytes: %v", err)
			}
			if len(voices) != 1 {
				t.Fatalf("voices = %d, want 1", len(voices))
			}
			_, got, err := voiceextract.Decode(voices[0])
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(got) != frames {
				t.Fatalf("samples = %d, want %d (the interleaved length is %d)", len(got), frames, frames*2)
			}
			for i, s := range got {
				if s != tc.want(i) {
					t.Fatalf("sample %d = %d, want %d", i, s, tc.want(i))
				}
			}
		})
	}
}

// TestConvertFSStereoWithoutAChoiceIsRefused is B2's other half on the
// SFZ route: a caller that never asked gets no silent default.
func TestConvertFSStereoWithoutAChoiceIsRefused(t *testing.T) {
	t.Parallel()
	_, err := ConvertFS(context.Background(), stereoKitFS(256), "kit.sfz", 18000, false, voiceimport.ChannelMonoOnly)
	if err == nil {
		t.Fatal("expected an error for stereo input with no channel choice")
	}
	if !strings.Contains(err.Error(), "stereo.wav") {
		t.Errorf("error should name the stereo file: %v", err)
	}
}

// TestConvertFSInvalidChannelIsRefused pins the argument check to the
// call rather than to the first stereo file: an out of range value used
// to pass unnoticed on an all mono instrument.
func TestConvertFSInvalidChannelIsRefused(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"mono.wav": &fstest.MapFile{Data: monoWAVBytes(256)},
		"kit.sfz": &fstest.MapFile{Data: []byte(
			"<region>\nsample=mono.wav\nlokey=36\nhikey=36\npitch_keycenter=36\n",
		)},
	}
	_, err := ConvertFS(context.Background(), fsys, "kit.sfz", 18000, false, voiceimport.Channel(99))
	if err == nil {
		t.Fatal("expected an error for a channel outside the enum")
	}
	if !strings.Contains(err.Error(), "invalid channel") {
		t.Errorf("error should name the invalid channel: %v", err)
	}
}

// TestConvertDirFSInvalidChannelIsRefused is the same check on the
// folder route.
func TestConvertDirFSInvalidChannelIsRefused(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"mono.wav": &fstest.MapFile{Data: monoWAVBytes(256)}}
	_, err := ConvertDirFS(context.Background(), fsys, 18000, false, voiceimport.Channel(99))
	if err == nil {
		t.Fatal("expected an error for a channel outside the enum")
	}
	if !strings.Contains(err.Error(), "invalid channel") {
		t.Errorf("error should name the invalid channel: %v", err)
	}
}

// TestConvertDirFSWalksSubdirectories pins the browser's dropped
// folder to the count the dialog showed. Every WAV in the tree crosses
// the boundary as one selection, so every WAV becomes a voice. A tree
// mixing the two levels converted the top level alone, without saying
// so.
func TestConvertDirFSWalksSubdirectories(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"snares/06.wav": &fstest.MapFile{Data: monoWAVBytes(768)},
		"kicks/01.wav":  &fstest.MapFile{Data: monoWAVBytes(256)},
		"root.wav":      &fstest.MapFile{Data: monoWAVBytes(512)},
		"readme.txt":    &fstest.MapFile{Data: []byte("hi")},
	}
	res, err := ConvertDirFS(context.Background(), fsys, fixtureRate, false, voiceimport.ChannelMix)
	if err != nil {
		t.Fatalf("ConvertDirFS: %v", err)
	}
	voices, _, err := voiceunpack.UnpackDataFromBytes(res.FZF)
	if err != nil {
		t.Fatalf("UnpackDataFromBytes: %v", err)
	}
	// Sorted by full path, so the keyboard runs kicks, root, snares
	// from C2 up. The frame counts say which file landed where.
	want := []struct {
		name   string
		frames int
	}{
		{"01", 256},
		{"ROOT", 512},
		{"06", 768},
	}
	if len(voices) != len(want) {
		t.Fatalf("voices = %d, want %d (every WAV in the tree)", len(voices), len(want))
	}
	for i, w := range want {
		got := strings.TrimRight(string(voices[i][disk.VoiceNameOffset:disk.VoiceNameOffset+disk.LabelSize]), " \x00")
		if got != w.name {
			t.Errorf("voice %d is named %q, want %q", i, got, w.name)
		}
		_, samples, err := voiceextract.Decode(voices[i])
		if err != nil {
			t.Fatalf("Decode voice %d: %v", i, err)
		}
		if len(samples) != w.frames {
			t.Errorf("voice %d holds %d samples, want %d", i, len(samples), w.frames)
		}
	}
}

// TestConvertDirFSNestedOnlyFolderConverts is the shape a DAW export
// takes when every sample sits in a category folder. The top level
// holds no WAV at all, and the whole import used to be refused.
func TestConvertDirFSNestedOnlyFolderConverts(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"kicks/01.wav":  &fstest.MapFile{Data: monoWAVBytes(256)},
		"snares/06.wav": &fstest.MapFile{Data: monoWAVBytes(512)},
	}
	res, err := ConvertDirFS(context.Background(), fsys, fixtureRate, false, voiceimport.ChannelMix)
	if err != nil {
		t.Fatalf("ConvertDirFS: %v", err)
	}
	hdr, err := fzutil.ParseFZFHeader(res.FZF)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}
	if hdr.NVoice != 2 {
		t.Errorf("voices = %d, want 2", hdr.NVoice)
	}
}

// TestConvertDirFSEmptyTreeStillRefuses keeps the refusal for a folder
// holding no WAV anywhere, which is the only case left once the walk
// reaches every depth.
func TestConvertDirFSEmptyTreeStillRefuses(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"readme.txt":      &fstest.MapFile{Data: []byte("hi")},
		"docs/notes.txt":  &fstest.MapFile{Data: []byte("hi")},
		"docs/cover.jpeg": &fstest.MapFile{Data: []byte("hi")},
	}
	_, err := ConvertDirFS(context.Background(), fsys, fixtureRate, false, voiceimport.ChannelMix)
	if err == nil {
		t.Fatal("expected an error when the folder holds no WAV at all")
	}
	if !strings.Contains(err.Error(), "no WAV files found") {
		t.Errorf("error %q should say no WAV files were found", err)
	}
}

// TestEstimateFZFSizeCountsFrames pins the fit to disk estimate to
// frames. Measuring the interleaved buffer doubled a stereo
// instrument's estimate and stepped the rate down earlier than needed.
func TestEstimateFZFSizeCountsFrames(t *testing.T) {
	t.Parallel()
	const frames = 4096
	stereo, err := wav.Read(bytes.NewReader(stereoWAVBytes(18000, frames)))
	if err != nil {
		t.Fatal(err)
	}
	mono, err := wav.Read(bytes.NewReader(monoWAVBytes(frames)))
	if err != nil {
		t.Fatal(err)
	}
	region := sfz.NewRegion()
	region.Sample = sampleWAV
	regions := []sfz.Region{region}

	got := estimateFZFSize(regions, map[string]*wav.File{sampleWAV: stereo}, 18000)
	want := estimateFZFSize(regions, map[string]*wav.File{sampleWAV: mono}, 18000)
	if got != want {
		t.Errorf("stereo estimate = %d bytes, want the %d bytes its %d frames need", got, want, frames)
	}
}

// TestConvertMultiDiskFSMatchesConvertMultiDisk pins the two disk split:
// the in-memory disks must equal the payloads extracted from the images
// the path pipeline writes.
func TestConvertMultiDiskFSMatchesConvertMultiDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var sfzContent strings.Builder
	for i := range 5 {
		name := fmt.Sprintf("voice%02d.wav", i)
		testutil.WriteTestWAV(t, filepath.Join(dir, name), 36000, 150000)
		note := 36 + i
		fmt.Fprintf(&sfzContent, "<region>\nsample=%s\nlokey=%d\nhikey=%d\npitch_keycenter=%d\n\n",
			name, note, note, note)
	}
	sfzPath := filepath.Join(dir, "test.sfz")
	if err := os.WriteFile(sfzPath, []byte(sfzContent.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	outPrefix := filepath.Join(t.TempDir(), "multi")
	if err := ConvertMultiDisk(context.Background(), sfzPath, outPrefix, 36000); err != nil {
		t.Fatalf("ConvertMultiDisk: %v", err)
	}

	result, err := ConvertMultiDiskFS(context.Background(), os.DirFS(dir), "test.sfz", 36000, voiceimport.ChannelMonoOnly)
	if err != nil {
		t.Fatalf("ConvertMultiDiskFS: %v", err)
	}
	if len(result.Disks) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(result.Disks))
	}

	for i, wantPayload := range result.Disks {
		img, err := disk.OpenImage(fmt.Sprintf("%s-%d.img", outPrefix, i+1))
		if err != nil {
			t.Fatalf("opening disk %d: %v", i+1, err)
		}
		got, err := diskget.FromImage(img, disk.FullDumpName)
		if err != nil {
			t.Fatalf("extracting from disk %d: %v", i+1, err)
		}
		if !bytes.Equal(got, wantPayload) {
			t.Errorf("disk %d payload differs: image %d bytes, in-memory %d bytes", i+1, len(got), len(wantPayload))
		}
	}
}

// TestConvertMultiDiskFSStereoReducesToChosenChannel covers B2 on the
// split route. The doubled length made an instrument that fits two
// disks measure 2.9 MB against their 2.5 MB, so the call used to be
// refused outright: "use --fit-to-disk instead".
func TestConvertMultiDiskFSStereoReducesToChosenChannel(t *testing.T) {
	t.Parallel()
	const (
		frames = 150000
		voices = 5
	)
	fsys := fstest.MapFS{}
	var sfzContent strings.Builder
	for i := range voices {
		name := fmt.Sprintf("voice%02d.wav", i)
		fsys[name] = &fstest.MapFile{Data: stereoWAVBytes(36000, frames)}
		note := 36 + i
		fmt.Fprintf(&sfzContent, "<region>\nsample=%s\nlokey=%d\nhikey=%d\npitch_keycenter=%d\n\n",
			name, note, note, note)
	}
	fsys["kit.sfz"] = &fstest.MapFile{Data: []byte(sfzContent.String())}

	result, err := ConvertMultiDiskFS(context.Background(), fsys, "kit.sfz", 36000, voiceimport.ChannelLeft)
	if err != nil {
		t.Fatalf("ConvertMultiDiskFS: %v", err)
	}
	if len(result.Disks) != 2 {
		t.Fatalf("disks = %d, want 2", len(result.Disks))
	}
	wantSectors := voices * (disk.PadToSector(frames*disk.BytesPerSample) / disk.SectorSize)
	if result.WaveCount != wantSectors {
		t.Errorf("wave sectors = %d, want %d (the interleaved count is %d)",
			result.WaveCount, wantSectors, wantSectors*2)
	}
}

// TestConvertFSMissingSampleNamesIt covers R9: a missing referenced sample
// produces an error that names the file.
func TestConvertFSMissingSampleNamesIt(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"broken.sfz": &fstest.MapFile{Data: []byte(
			"<region>\nsample=hats/missing.wav lokey=36 hikey=36 pitch_keycenter=36\n",
		)},
	}
	_, err := ConvertFS(context.Background(), fsys, "broken.sfz", 36000, false, voiceimport.ChannelMonoOnly)
	if err == nil {
		t.Fatal("expected an error for a missing sample")
	}
	if !strings.Contains(err.Error(), "missing.wav") {
		t.Errorf("error should name the missing sample: %v", err)
	}
}

// TestConvertFSFitToDiskStepsDown verifies fit to disk reports the stepped
// down rate through Result.Rate.
func TestConvertFSFitToDiskStepsDown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var sfzContent strings.Builder
	for i := range 3 {
		name := fmt.Sprintf("long%02d.wav", i)
		testutil.WriteTestWAV(t, filepath.Join(dir, name), 36000, 300000)
		note := 36 + i
		fmt.Fprintf(&sfzContent, "<region>\nsample=%s\nlokey=%d\nhikey=%d\npitch_keycenter=%d\n\n",
			name, note, note, note)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.sfz"), []byte(sfzContent.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ConvertFS(context.Background(), os.DirFS(dir), "big.sfz", 36000, true, voiceimport.ChannelMonoOnly)
	if err != nil {
		t.Fatalf("ConvertFS: %v", err)
	}
	if res.Rate >= 36000 {
		t.Errorf("expected a stepped down rate, got %d", res.Rate)
	}
	if len(res.FZF) > disk.UsableDataSize {
		t.Errorf("fit to disk output is %d bytes, above the %d limit", len(res.FZF), disk.UsableDataSize)
	}
}
