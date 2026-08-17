package webcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// monoRateWAV builds a mono 16-bit WAV of n samples at rate.
func monoRateWAV(t *testing.T, n int, rate uint32) []byte {
	t.Helper()
	samples := make([]int16, n)
	for i := range samples {
		samples[i] = int16(i % 173)
	}
	var buf bytes.Buffer
	if err := wav.Write(&buf, &wav.File{SampleRate: rate, Samples: samples, Channels: 1}); err != nil {
		t.Fatalf("wav.Write: %v", err)
	}
	return buf.Bytes()
}

// stereoRateWAV hand-rolls a 16-bit stereo PCM WAV of n frames at
// rate, since wav.Write only produces mono.
func stereoRateWAV(t *testing.T, n int, rate uint32) []byte {
	t.Helper()
	dataLen := n * 4
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataLen)) // #nosec G115 -- test fixture
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, rate)
	_ = binary.Write(&buf, binary.LittleEndian, rate*4)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(4))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataLen)) // #nosec G115 -- test fixture
	frame := make([]byte, 4)
	for i := 0; i < n; i++ {
		left := int16(i % 211)
		right := int16(-42)
		binary.LittleEndian.PutUint16(frame, uint16(left))      // #nosec G115 -- bit pattern round trip
		binary.LittleEndian.PutUint16(frame[2:], uint16(right)) // #nosec G115 -- bit pattern round trip
		buf.Write(frame)
	}
	return buf.Bytes()
}

// dumpLen reads the document's full dump length straight off the
// exported image, the same bytes the sampler would load.
func dumpLen(t *testing.T, s *Session) int {
	t.Helper()
	img, err := disk.ReadImage(bytes.NewReader(mustExport(t, s)))
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	fzf, gerr := diskget.FromImage(img, disk.FullDumpName)
	if gerr != nil {
		t.Fatalf("FromImage: %v", gerr)
	}
	return len(fzf)
}

func TestEstimateImportSmallMonoFits(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	est, cerr := s.EstimateImport(map[string][]byte{"hat.wav": monoRateWAV(t, 18000, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.Verdict != VerdictFits {
		t.Errorf("verdict = %q, want %q", est.Verdict, VerdictFits)
	}
	if est.Seconds < 0.99 || est.Seconds > 1.01 {
		t.Errorf("seconds = %f, want about 1.0", est.Seconds)
	}
	// One second of mono at 18 kHz is 36000 bytes of audio plus the
	// dump the voice brings into being around it.
	if est.Bytes < 36000 || est.Bytes > 36000+4*disk.SectorSize {
		t.Errorf("bytes = %d, want a shade over 36000", est.Bytes)
	}
	if est.AnyStereo {
		t.Error("mono batch reported stereo")
	}
	if est.RoomSeconds < 30 {
		t.Errorf("room seconds = %f, want a near-empty disk's worth", est.RoomSeconds)
	}
}

// The rate radio changes the estimate: the same source is a quarter
// the bytes at a quarter the rate.
func TestEstimateImportTracksRate(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	files := map[string][]byte{"pad.wav": monoRateWAV(t, 36000, 36000)}
	at36, cerr := s.EstimateImport(files, 36000, ChannelMix)
	if cerr != nil {
		t.Fatalf("at 36k: %v", cerr)
	}
	at9, cerr := s.EstimateImport(files, 9000, ChannelMix)
	if cerr != nil {
		t.Fatalf("at 9k: %v", cerr)
	}
	if at9.Bytes >= at36.Bytes {
		t.Errorf("9 kHz bytes %d not below 36 kHz bytes %d", at9.Bytes, at36.Bytes)
	}
	if at9.Seconds < at36.Seconds-0.01 || at9.Seconds > at36.Seconds+0.01 {
		t.Errorf("duration moved with the rate: %f vs %f", at9.Seconds, at36.Seconds)
	}
}

// The feedback scenario: 59.4 s of stereo 44.1 kHz is over the
// sampler's memory at 36 and 18 kHz and fits only at 9 kHz.
func TestEstimateImportOverSampleMemory(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	files := map[string][]byte{"long.wav": stereoRateWAV(t, 2619540, 44100)}
	for _, rate := range []uint32{36000, 18000} {
		est, cerr := s.EstimateImport(files, rate, ChannelMix)
		if cerr != nil {
			t.Fatalf("at %d: %v", rate, cerr)
		}
		if est.Verdict != VerdictWontFit || est.Reason != ReasonSampleMemory {
			t.Errorf("at %d: verdict %q reason %q, want %q %q", rate, est.Verdict, est.Reason, VerdictWontFit, ReasonSampleMemory)
		}
		if est.OverCapFile != "long.wav" {
			t.Errorf("at %d: over-cap file %q, want long.wav", rate, est.OverCapFile)
		}
		if est.FileSeconds < 59.3 || est.FileSeconds > 59.5 {
			t.Errorf("at %d: file seconds %f, want about 59.4", rate, est.FileSeconds)
		}
		wantCap := float64(fzutil.MaxResampleOut) / float64(rate)
		if est.CapSeconds != wantCap {
			t.Errorf("at %d: cap seconds %f, want %f", rate, est.CapSeconds, wantCap)
		}
		if len(est.FitsAtRates) != 1 || est.FitsAtRates[0] != 9000 {
			t.Errorf("at %d: fits at %v, want [9000]", rate, est.FitsAtRates)
		}
		if !est.AnyStereo {
			t.Errorf("at %d: stereo file not reported", rate)
		}
	}
	est, cerr := s.EstimateImport(files, 9000, ChannelMix)
	if cerr != nil {
		t.Fatalf("at 9000: %v", cerr)
	}
	if est.Verdict != VerdictFits {
		t.Errorf("at 9000: verdict %q, want %q", est.Verdict, VerdictFits)
	}
	if est.Seconds < 59.3 || est.Seconds > 59.5 {
		t.Errorf("at 9000: seconds %f, want about 59.4", est.Seconds)
	}
}

// A join that outgrows one disk but fits two reports the split.
func TestEstimateImportReportsSplit(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.ImportWAVToInstrument("first.wav", monoRateWAV(t, 450000, 18000), 18000, ChannelMix); cerr != nil {
		t.Fatalf("first import: %v", cerr)
	}
	est, cerr := s.EstimateImport(map[string][]byte{"overflow.wav": monoRateWAV(t, 450000, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.Verdict != VerdictSplits {
		t.Errorf("verdict = %q, want %q", est.Verdict, VerdictSplits)
	}
}

// A single first import has no split path: a dump too big for one
// disk is refused for room, and the rates that would fit are named.
func TestEstimateImportSingleOverOneDisk(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	est, cerr := s.EstimateImport(map[string][]byte{"big.wav": monoRateWAV(t, 720000, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.Verdict != VerdictWontFit || est.Reason != ReasonDiskRoom {
		t.Errorf("verdict %q reason %q, want %q %q", est.Verdict, est.Reason, VerdictWontFit, ReasonDiskRoom)
	}
	if len(est.FitsAtRates) != 1 || est.FitsAtRates[0] != 9000 {
		t.Errorf("fits at %v, want [9000]", est.FitsAtRates)
	}
}

// A multi file batch with no instrument converts as a folder kit,
// which can split: the same audio refused above fits as two files.
func TestEstimateImportBatchSplits(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	est, cerr := s.EstimateImport(map[string][]byte{
		"left half.wav":  monoRateWAV(t, 450000, 18000),
		"right half.wav": monoRateWAV(t, 450000, 18000),
	}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.Verdict != VerdictSplits {
		t.Errorf("verdict = %q, want %q", est.Verdict, VerdictSplits)
	}
}

// More audio than two disks hold at any rate: nothing to suggest.
func TestEstimateImportOverTwoDisks(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	files := map[string][]byte{
		"slab one.wav":   monoRateWAV(t, 1000000, 9000),
		"slab two.wav":   monoRateWAV(t, 1000000, 9000),
		"slab three.wav": monoRateWAV(t, 1000000, 9000),
	}
	est, cerr := s.EstimateImport(files, 9000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.Verdict != VerdictWontFit {
		t.Errorf("verdict = %q, want %q", est.Verdict, VerdictWontFit)
	}
	if len(est.FitsAtRates) != 0 {
		t.Errorf("fits at %v, want none", est.FitsAtRates)
	}
}

func TestEstimateImportUnreadableFile(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	_, cerr := s.EstimateImport(map[string][]byte{
		"ok.wav":  monoRateWAV(t, 100, 18000),
		"bad.wav": {1, 2, 3, 4},
	}, 18000, ChannelMix)
	if cerr == nil || cerr.Code != "invalid-wav" {
		t.Fatalf("error = %v, want invalid-wav", cerr)
	}
	if !bytes.Contains([]byte(cerr.Message), []byte("bad.wav")) {
		t.Errorf("error %q does not name the file", cerr.Message)
	}
}

// Estimating is a read: no revision bump, no history entry.
func TestEstimateImportLeavesSessionUntouched(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	before := s.Snapshot()
	if _, cerr := s.EstimateImport(map[string][]byte{"quiet.wav": monoRateWAV(t, 500, 18000)}, 18000, ChannelMix); cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	after := s.Snapshot()
	if after.Revision != before.Revision || after.CanUndo != before.CanUndo {
		t.Errorf("estimate mutated the session: revision %d to %d", before.Revision, after.Revision)
	}
}

// The estimate is the conversion's own arithmetic: a join's estimated
// bytes equal the dump growth the import then produces.
func TestEstimateImportMatchesActualGrowth(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.ImportWAVToInstrument("first.wav", monoRateWAV(t, 30000, 18000), 18000, ChannelMix); cerr != nil {
		t.Fatalf("first import: %v", cerr)
	}
	before := dumpLen(t, s)
	wavData := monoRateWAV(t, 44100, 44100)
	est, cerr := s.EstimateImport(map[string][]byte{"second.wav": wavData}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if _, cerr := s.ImportWAVToInstrument("second.wav", wavData, 18000, ChannelMix); cerr != nil {
		t.Fatalf("second import: %v", cerr)
	}
	if got := dumpLen(t, s) - before; est.Bytes != got {
		t.Errorf("estimated %d bytes, actual growth %d", est.Bytes, got)
	}
}

// The verdict thresholds come from the same constants the writers
// use, so a capacity change moves both together.
func TestEstimateImportRoomAgainstDiskFileMax(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	est, cerr := s.EstimateImport(map[string][]byte{"tick.wav": monoRateWAV(t, 100, 36000)}, 36000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	want := float64(voicebuild.MaxDiskFileBytes) / disk.BytesPerSample / 36000
	if est.RoomSeconds > want {
		t.Errorf("room %f s exceeds an empty disk's %f s", est.RoomSeconds, want)
	}
	if est.RoomSeconds < want-1 {
		t.Errorf("room %f s, want within a second of %f", est.RoomSeconds, want)
	}
}

// A full instrument refuses one more voice whatever the rate, and the
// estimate says so before the import fails with voice-limit.
func TestEstimateImportRefusesPastVoiceLimit(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	batch := make(map[string][]byte, disk.MaxVoices)
	for i := 0; i < disk.MaxVoices; i++ {
		batch[fmt.Sprintf("v%02d.wav", i)] = monoRateWAV(t, 64, 18000)
	}
	if _, cerr := s.ImportWAVFolder(batch, 18000, false, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAVFolder: %v", cerr)
	}
	est, cerr := s.EstimateImport(map[string][]byte{"extra.wav": monoRateWAV(t, 64, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.Verdict != VerdictWontFit || est.Reason != ReasonVoiceLimit {
		t.Errorf("verdict %q reason %q, want %q %q", est.Verdict, est.Reason, VerdictWontFit, ReasonVoiceLimit)
	}
	if len(est.FitsAtRates) != 0 {
		t.Errorf("fits at %v, want none: no rate adds a 65th voice", est.FitsAtRates)
	}
}

// A folder of 65 WAVs cannot become an instrument at any rate.
func TestEstimateImportRefusesOversizeBatch(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	batch := make(map[string][]byte, disk.MaxVoices+1)
	for i := 0; i <= disk.MaxVoices; i++ {
		batch[fmt.Sprintf("v%02d.wav", i)] = monoRateWAV(t, 64, 18000)
	}
	est, cerr := s.EstimateImport(batch, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.Verdict != VerdictWontFit || est.Reason != ReasonVoiceLimit {
		t.Errorf("verdict %q reason %q, want %q %q", est.Verdict, est.Reason, VerdictWontFit, ReasonVoiceLimit)
	}
}

// Loose files eat sectors the naive dump arithmetic never sees: the
// room figure and the fits verdict both have to shrink with them.
func TestEstimateImportHonoursLooseFiles(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	empty, cerr := s.EstimateImport(map[string][]byte{"probe.wav": monoRateWAV(t, 100, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("estimate on empty disk: %v", cerr)
	}
	for _, name := range []string{"looseone.wav", "loosetwo.wav"} {
		if _, cerr := s.ImportWAV(name, monoRateWAV(t, 250000, 18000), 18000, ChannelMix); cerr != nil {
			t.Fatalf("ImportWAV %s: %v", name, cerr)
		}
	}
	crowded, cerr := s.EstimateImport(map[string][]byte{"probe.wav": monoRateWAV(t, 100, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("estimate on crowded disk: %v", cerr)
	}
	if crowded.RoomSeconds >= empty.RoomSeconds {
		t.Errorf("room %f s did not shrink from %f s though loose files hold 1 MB", crowded.RoomSeconds, empty.RoomSeconds)
	}
	// Under the one disk dump maximum, but over what the crowded disk
	// actually has free: only the free sector check refuses this.
	est, cerr := s.EstimateImport(map[string][]byte{"big.wav": monoRateWAV(t, 200000, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("estimate big: %v", cerr)
	}
	if est.Verdict != VerdictWontFit || est.Reason != ReasonDiskRoom {
		t.Errorf("verdict %q reason %q, want %q %q", est.Verdict, est.Reason, VerdictWontFit, ReasonDiskRoom)
	}
}

// A split fills disk 1 completely, so a loose file blocks it: the
// estimate must refuse rather than promise the split the writer would
// then reject.
func TestEstimateImportSplitBlockedByLooseFile(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.ImportWAV("loose.wav", monoRateWAV(t, 500, 18000), 18000, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAV: %v", cerr)
	}
	if _, cerr := s.ImportWAVToInstrument("first.wav", monoRateWAV(t, 450000, 18000), 18000, ChannelMix); cerr != nil {
		t.Fatalf("first import: %v", cerr)
	}
	est, cerr := s.EstimateImport(map[string][]byte{"blocked.wav": monoRateWAV(t, 450000, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.Verdict != VerdictWontFit || est.Reason != ReasonDiskRoom {
		t.Errorf("verdict %q reason %q, want %q %q (a loose file blocks the split)", est.Verdict, est.Reason, VerdictWontFit, ReasonDiskRoom)
	}
}

// The first join fills an empty instrument's placeholder slot, so the
// estimate must charge no voice sector for it: pinned byte for byte
// against the import.
func TestEstimateImportPlaceholderGrowthMatches(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.NewInstrument("PAD"); cerr != nil {
		t.Fatalf("NewInstrument: %v", cerr)
	}
	before := dumpLen(t, s)
	wavData := monoRateWAV(t, 30000, 18000)
	est, cerr := s.EstimateImport(map[string][]byte{"first.wav": wavData}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if _, cerr := s.ImportWAVToInstrument("first.wav", wavData, 18000, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAVToInstrument: %v", cerr)
	}
	if got := dumpLen(t, s) - before; est.Bytes != got {
		t.Errorf("estimated %d bytes, actual growth %d", est.Bytes, got)
	}
}

// A split document estimates against the pair's capacity, with the
// sampler's 2 MB sample memory as the binding ceiling.
func TestEstimateImportTwoDiskRoom(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("EST"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	for _, name := range []string{"first.wav", "half two.wav"} {
		if _, cerr := s.ImportWAVToInstrument(name, monoRateWAV(t, 450000, 18000), 18000, ChannelMix); cerr != nil {
			t.Fatalf("import %s: %v", name, cerr)
		}
	}
	snap := s.Snapshot()
	if snap.Disk == nil || snap.Disk.Disks != 2 {
		t.Fatalf("document did not split to two disks")
	}
	est, cerr := s.EstimateImport(map[string][]byte{"probe two.wav": monoRateWAV(t, 100, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	// 2 MB sample memory minus the 1.8 MB of audio on the pair leaves
	// about 8.2 s at 18 kHz; the disk arithmetic alone would say 11.
	if est.RoomSeconds < 7.5 || est.RoomSeconds > 9 {
		t.Errorf("room = %f s, want the memory bound of about 8.2", est.RoomSeconds)
	}
}
