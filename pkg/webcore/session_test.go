package webcore

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "synthetic", "TECHNO.img"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestNewSessionStartsEmpty(t *testing.T) {
	s := NewSession()
	snap := s.Snapshot()
	if snap.Revision != 0 {
		t.Fatalf("revision = %d, want 0", snap.Revision)
	}
	if snap.Disk != nil {
		t.Fatalf("disk = %+v, want nil", snap.Disk)
	}
}

func TestNewDisk(t *testing.T) {
	s := NewSession()
	snap, cerr := s.NewDisk("MY DISK")
	if cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if snap.Revision != 1 {
		t.Fatalf("revision = %d, want 1", snap.Revision)
	}
	if snap.Disk == nil || snap.Disk.Label != "MY DISK" {
		t.Fatalf("disk = %+v, want label MY DISK", snap.Disk)
	}
	if snap.Disk.CapacityBytes != disk.ImageSize {
		t.Fatalf("capacity = %d, want %d", snap.Disk.CapacityBytes, disk.ImageSize)
	}
}

func TestNewDiskRejectsBadLabel(t *testing.T) {
	s := NewSession()
	for name, label := range map[string]string{
		"empty":       "",
		"over length": "THIRTEEN CHRS",
		"non ASCII":   "café",
	} {
		t.Run(name, func(t *testing.T) {
			_, cerr := s.NewDisk(label)
			if cerr == nil {
				t.Fatalf("NewDisk(%q) accepted a bad label", label)
			}
			if cerr.Code != "invalid-label" {
				t.Fatalf("code = %q, want invalid-label", cerr.Code)
			}
			if cerr.Message == "" {
				t.Fatal("error message is empty")
			}
		})
	}
	if snap := s.Snapshot(); snap.Revision != 0 || snap.Disk != nil {
		t.Fatalf("rejected ops mutated state: %+v", snap)
	}
}

func TestNewDiskExportsBlankImage(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("FRESH"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	if len(out) != disk.ImageSize {
		t.Fatalf("export size = %d, want %d", len(out), disk.ImageSize)
	}
	img, err := disk.ReadImage(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("exported blank image does not parse: %v", err)
	}
	if img.Label() != "FRESH" {
		t.Fatalf("label = %q, want FRESH", img.Label())
	}
}

func TestOpenImageRoundTripsByteIdentical(t *testing.T) {
	s := NewSession()
	in := fixture(t)
	snap, cerr := s.OpenImage(in)
	if cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	if snap.Disk == nil || snap.Disk.Label == "" {
		t.Fatalf("disk = %+v, want a labelled disk", snap.Disk)
	}
	if snap.Disk.UsedBytes <= 0 || snap.Disk.UsedBytes > disk.ImageSize {
		t.Fatalf("usedBytes = %d, want within (0, %d]", snap.Disk.UsedBytes, disk.ImageSize)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	if sha256.Sum256(out) != sha256.Sum256(in) {
		t.Fatal("round trip is not byte identical")
	}
}

func TestOpenImageDoesNotAliasCallerBuffer(t *testing.T) {
	s := NewSession()
	in := fixture(t)
	if _, cerr := s.OpenImage(in); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	want := sha256.Sum256(in)
	for i := range in {
		in[i] = 0
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	if sha256.Sum256(out) != want {
		t.Fatal("export changed when the caller's buffer was scribbled on")
	}
}

func TestSnapshotDoesNotAliasSessionState(t *testing.T) {
	const changed = "CHANGED"
	s := twoVoiceSession(t)
	if _, cerr := s.ImportWAV("LOOSE.wav", wavBytes(t, 1000), 18000, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAV: %v", cerr)
	}
	snap := s.Snapshot()
	want, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}

	file := &snap.Disk.Files[len(snap.Disk.Files)-1]
	file.Params["name"] = changed
	file.Voice.Loops[0].Start++
	file.Voice.Dca.Rates[0]++
	file.Voice.Dcf.Stops[0]++
	inst := snap.Disk.Instrument
	inst.Banks[0].Areas[0].VoiceName = changed
	inst.Voices[0].Params["name"] = changed
	inst.Voices[0].Voice.Loops[0].Start++
	inst.Voices[0].Voice.Dca.Rates[0]++
	inst.Voices[0].Voice.Dcf.Stops[0]++
	inst.Effects.Matrix[0][0]++

	got, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("mutating a returned snapshot changed the session's next snapshot")
	}
}

func TestAdoptStateRejectsBadSecondImageWithoutChangingSession(t *testing.T) {
	s := NewSession()
	if _, cerr := s.OpenImage(fixture(t)); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	beforeImage := bytes.Clone(s.image)
	beforeSnapshot, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	beforeMode := s.disMode
	beforePast, beforeFuture := len(s.past), len(s.future)
	img, err := disk.ReadImage(bytes.NewReader(s.image))
	if err != nil {
		t.Fatal(err)
	}

	if _, cerr := s.adoptState(img, []byte{1}, s.disMode); cerr == nil {
		t.Fatal("adoptState accepted an unreadable second image")
	}
	afterSnapshot, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.image, beforeImage) || !bytes.Equal(afterSnapshot, beforeSnapshot) ||
		s.disMode != beforeMode || len(s.past) != beforePast || len(s.future) != beforeFuture {
		t.Fatal("failed adoption changed session state")
	}
}

func TestUndoFailurePreservesDocumentAndHistory(t *testing.T) {
	s := NewSession()
	if _, cerr := s.OpenImage(fixture(t)); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	s.past = []imagePair{{img1: bytes.Clone(s.image), img2: []byte{1}, disMode: !s.disMode}}
	before, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	beforeImage := bytes.Clone(s.image)
	beforeMode := s.disMode

	if _, cerr := s.Undo(); cerr == nil {
		t.Fatal("Undo succeeded with an unreadable disk 2 history entry")
	}
	after, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) || !bytes.Equal(s.image, beforeImage) || s.disMode != beforeMode ||
		len(s.past) != 1 || len(s.future) != 0 {
		t.Fatal("failed Undo changed the document or history")
	}
}

func TestRedoFailurePreservesDocumentAndHistory(t *testing.T) {
	s := NewSession()
	if _, cerr := s.OpenImage(fixture(t)); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	s.future = []imagePair{{img1: bytes.Clone(s.image), img2: []byte{1}, disMode: !s.disMode}}
	before, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	beforeImage := bytes.Clone(s.image)
	beforeMode := s.disMode

	if _, cerr := s.Redo(); cerr == nil {
		t.Fatal("Redo succeeded with an unreadable disk 2 history entry")
	}
	after, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) || !bytes.Equal(s.image, beforeImage) || s.disMode != beforeMode ||
		len(s.past) != 0 || len(s.future) != 1 {
		t.Fatal("failed Redo changed the document or history")
	}
}

func TestOpenImageRejectsWrongSize(t *testing.T) {
	s := NewSession()
	_, cerr := s.OpenImage(make([]byte, 16))
	if cerr == nil || cerr.Code != "invalid-image" {
		t.Fatalf("cerr = %v, want invalid-image", cerr)
	}
}

func TestOpenImageRejectsGarbage(t *testing.T) {
	s := NewSession()
	garbage := make([]byte, disk.ImageSize)
	for i := range garbage {
		garbage[i] = 0xff
	}
	_, cerr := s.OpenImage(garbage)
	if cerr == nil || cerr.Code != "invalid-image" {
		t.Fatalf("cerr = %v, want invalid-image", cerr)
	}
	if snap := s.Snapshot(); snap.Revision != 0 {
		t.Fatalf("rejected open advanced the revision to %d", snap.Revision)
	}
}

func TestExportWithoutDisk(t *testing.T) {
	s := NewSession()
	_, cerr := s.ExportImage()
	if cerr == nil || cerr.Code != codeNoDisk {
		t.Fatalf("cerr = %v, want no-disk", cerr)
	}
}

func TestRevisionAdvancesPerMutation(t *testing.T) {
	s := NewSession()
	a, _ := s.NewDisk("ONE")
	b, cerr := s.OpenImage(fixture(t))
	if cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	if b.Revision <= a.Revision {
		t.Fatalf("revision did not advance: %d then %d", a.Revision, b.Revision)
	}
}

func wavBytes(t *testing.T, n int) []byte {
	t.Helper()
	samples := make([]int16, n)
	for i := range samples {
		samples[i] = int16(i % 199)
	}
	var buf bytes.Buffer
	if err := wav.Write(&buf, &wav.File{SampleRate: 18000, Samples: samples, Channels: 1}); err != nil {
		t.Fatalf("wav.Write: %v", err)
	}
	return buf.Bytes()
}

func TestImportWAVYieldsOneVoiceSnapshot(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("SLICE2"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	before := s.Snapshot()

	snap, cerr := s.ImportWAV("Kick 1.wav", wavBytes(t, 3000), 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("ImportWAV: %v", cerr)
	}
	if snap.Disk == nil || len(snap.Disk.Files) != 1 {
		t.Fatalf("files = %+v, want one voice", snap.Disk)
	}
	f := snap.Disk.Files[0]
	if f.Name != "KICK 1" {
		t.Fatalf("name = %q, want KICK 1", f.Name)
	}
	if f.Type != "voice" {
		t.Fatalf("type = %q, want voice", f.Type)
	}
	if f.SizeBytes <= 0 {
		t.Fatalf("size = %d, want positive", f.SizeBytes)
	}
	if snap.Disk.UsedBytes <= before.Disk.UsedBytes {
		t.Fatalf("capacity did not move: %d then %d", before.Disk.UsedBytes, snap.Disk.UsedBytes)
	}
	if snap.Revision <= before.Revision {
		t.Fatal("revision did not advance")
	}
}

// The exported image after a browser-style import is byte identical to
// the CLI pipeline (disk new, voice import, disk add) on the same
// input. Q1: same packages, same bytes.
func TestImportWAVExportMatchesCLI(t *testing.T) {
	wavData := wavBytes(t, 5000)

	dir := t.TempDir()
	wavPath := filepath.Join(dir, "SNARE 2.wav")
	fzvPath := filepath.Join(dir, "SNARE 2.fzv")
	imgPath := filepath.Join(dir, "ref.img")
	if err := os.WriteFile(wavPath, wavData, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := diskformat.Format(imgPath, "SLICE2"); err != nil {
		t.Fatalf("Format: %v", err)
	}
	if err := voiceimport.Import(wavPath, fzvPath, 18000); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if err := diskadd.Add(imgPath, fzvPath, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ref, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	s := NewSession()
	if _, cerr := s.NewDisk("SLICE2"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.ImportWAV("SNARE 2.wav", wavData, 18000, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAV: %v", cerr)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	if sha256.Sum256(out) != sha256.Sum256(ref) {
		t.Fatal("browser import differs from the CLI pipeline")
	}
}

func TestImportWAVRejections(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("REJECT"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	before := s.Snapshot()

	const anyName = "x.wav"
	cases := []struct {
		name     string
		filename string
		data     []byte
		rate     uint32
		channel  string
		code     string
	}{
		{"garbage wav", anyName, []byte("not a wav"), 18000, ChannelMix, "invalid-wav"},
		{"bad rate", anyName, wavBytes(t, 100), 12345, ChannelMix, "invalid-rate"},
		{"bad channel", anyName, wavBytes(t, 100), 18000, "sideways", "invalid-channel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cerr := s.ImportWAV(tc.filename, tc.data, tc.rate, tc.channel)
			if cerr == nil || cerr.Code != tc.code {
				t.Fatalf("cerr = %v, want %s", cerr, tc.code)
			}
		})
	}
	if snap := s.Snapshot(); snap.Revision != before.Revision {
		t.Fatal("rejected imports mutated state")
	}

	sNoDisk := NewSession()
	if _, cerr := sNoDisk.ImportWAV(anyName, wavBytes(t, 100), 18000, ChannelMix); cerr == nil || cerr.Code != codeNoDisk {
		t.Fatalf("cerr = %v, want no-disk", cerr)
	}
}

// R10: an import the disk cannot hold is rejected with the core's
// error and leaves the document untouched.
func TestImportWAVOverCapacity(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("FULL"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	before := s.Snapshot()

	// 700k samples at 18 kHz is ~1.4 MB of wave data: more than one disk.
	_, cerr := s.ImportWAV("HUGE.wav", wavBytes(t, 700_000), 18000, ChannelMix)
	if cerr == nil || cerr.Code != "no-space" {
		t.Fatalf("cerr = %v, want no-space", cerr)
	}
	after := s.Snapshot()
	if after.Revision != before.Revision || after.Disk.UsedBytes != before.Disk.UsedBytes {
		t.Fatal("rejected import mutated state")
	}
	out, xerr := s.ExportImage()
	if xerr != nil {
		t.Fatalf("ExportImage: %v", xerr)
	}
	if len(out) != disk.ImageSize {
		t.Fatalf("export size = %d", len(out))
	}
}

func TestOpenImageListsFiles(t *testing.T) {
	s := NewSession()
	if _, cerr := s.OpenImage(fixture(t)); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	snap := s.Snapshot()
	if snap.Disk == nil || len(snap.Disk.Files) == 0 {
		t.Fatalf("files = %+v, want the fixture's listing", snap.Disk)
	}
	for _, f := range snap.Disk.Files {
		if f.Name == "" || f.Type == "" {
			t.Fatalf("entry incomplete: %+v", f)
		}
	}
}

// Opening a document starts history fresh. Otherwise undo would
// resurrect the disk the user just left, under the new disk's name.
func TestOpeningAnImageResetsHistory(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("FIRST"); cerr != nil {
		t.Fatal(cerr)
	}
	if _, cerr := s.ImportWAV("a.wav", wavBytes(t, 500), 18000, ChannelMix); cerr != nil {
		t.Fatal(cerr)
	}
	if !s.Snapshot().CanUndo {
		t.Fatal("the import should be undoable")
	}

	snap, cerr := s.OpenImage(fixture(t))
	if cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	if snap.CanUndo || snap.CanRedo {
		t.Error("a freshly opened document carries no history")
	}
	if _, cerr := s.Undo(); cerr == nil {
		t.Error("undo should find nothing to undo after an open")
	}
	// The open disk is the one the user opened, not the one left behind.
	if got := s.Snapshot().Disk.Label; got == "FIRST" {
		t.Error("undo resurrected the previous disk")
	}
}

// A new disk is likewise a fresh document.
func TestNewDiskResetsHistory(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("ONE"); cerr != nil {
		t.Fatal(cerr)
	}
	if _, cerr := s.ImportWAV("a.wav", wavBytes(t, 400), 18000, ChannelMix); cerr != nil {
		t.Fatal(cerr)
	}
	if _, cerr := s.NewDisk("TWO"); cerr != nil {
		t.Fatal(cerr)
	}
	if s.Snapshot().CanUndo {
		t.Error("a new disk starts with no history")
	}
}

// A drag commit that lands a history entry is a state change: the
// snapshot's revision has to move so revision-keyed caches refresh.
func TestCommitGestureBumpsRevision(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("REV"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	s.BeginGesture()
	if _, cerr := s.RenameDisk("REV TWO"); cerr != nil {
		t.Fatalf("RenameDisk: %v", cerr)
	}
	before := s.Snapshot().Revision
	if !s.CommitGesture() {
		t.Fatal("gesture with an edit did not land")
	}
	if got := s.Snapshot().Revision; got <= before {
		t.Errorf("revision = %d after landing commit, want above %d", got, before)
	}

	s.BeginGesture()
	still := s.Snapshot().Revision
	if s.CommitGesture() {
		t.Fatal("empty gesture landed an entry")
	}
	if got := s.Snapshot().Revision; got != still {
		t.Errorf("empty commit moved the revision from %d to %d", still, got)
	}
}
