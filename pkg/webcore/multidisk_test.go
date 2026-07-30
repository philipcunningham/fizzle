package webcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

// bigVoices builds three ~600 KB voices: together too large for one
// disk, well inside a two disk set.
func bigVoices(t *testing.T) ([][]byte, []voicebuild.Keygroup) {
	t.Helper()
	const n = 3
	voices := make([][]byte, n)
	groups := make([]voicebuild.Keygroup, n)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("BIG%02d", i+1), 300000)
		lo := uint8(36 + i) // #nosec G115 -- small test values
		groups[i] = voicebuild.NewKeygroup(lo, lo, lo)
	}
	return voices, groups
}

// splitSession loads an oversized dump, yielding a two disk document.
func splitSession(t *testing.T) (*Session, voicebuild.MultiDiskResult) {
	t.Helper()
	voices, groups := bigVoices(t)
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatalf("AssembleWithKeygroups: %v", err)
	}
	want, err := voicebuild.AssembleMultiDisk(voices, groups)
	if err != nil {
		t.Fatalf("AssembleMultiDisk: %v", err)
	}
	s := NewSession()
	if _, cerr := s.LoadFZF(fzf); cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	return s, want
}

// payload extracts the FULL-DATA-FZ payload from raw image bytes.
func payload(t *testing.T, image []byte) []byte {
	t.Helper()
	img, err := disk.ReadImage(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	data, gerr := diskget.FromImage(img, disk.FullDumpName)
	if gerr != nil {
		t.Fatalf("FromImage: %v", gerr)
	}
	return data
}

// An oversized dump splits across two disks (R25), with payloads byte
// identical to the CLI builder's own split.
func TestLoadFZFOversizedSplits(t *testing.T) {
	s, want := splitSession(t)

	snap := s.Snapshot()
	if snap.Disk == nil || snap.Disk.Disks != 2 {
		t.Fatalf("disks = %+v, want 2", snap.Disk)
	}
	if snap.Disk.CapacityBytes != 2*disk.ImageSize {
		t.Errorf("capacity = %d, want %d", snap.Disk.CapacityBytes, 2*disk.ImageSize)
	}
	if snap.Disk.MissingDisk != 0 {
		t.Errorf("missing disk = %d, want 0", snap.Disk.MissingDisk)
	}
	if got := len(instrument(t, s).Voices); got != 3 {
		t.Fatalf("voices = %d, want 3", got)
	}

	for i, wantPayload := range want.Disks {
		image, cerr := s.ExportImageAt(i)
		if cerr != nil {
			t.Fatalf("ExportImageAt(%d): %v", i, cerr)
		}
		if len(image) != disk.ImageSize {
			t.Fatalf("disk %d image is %d bytes", i+1, len(image))
		}
		if !bytes.Equal(payload(t, image), wantPayload) {
			t.Errorf("disk %d payload differs from the CLI split", i+1)
		}
	}

	// A voice whose audio lives on disk 2 auditions through the stitch.
	aud, aerr := s.AuditionSlot(2)
	if aerr != nil {
		t.Fatalf("AuditionSlot(2): %v", aerr)
	}
	if len(aud.PCM) != 300000 {
		t.Errorf("disk 2 voice decodes %d frames, want 300000", len(aud.PCM))
	}
}

// R5: both images open in either order as one instrument, and export
// round trips byte identically.
func TestOpenImagePairEitherOrder(t *testing.T) {
	s, _ := splitSession(t)
	disk1, _ := s.ExportImageAt(0)
	disk2, _ := s.ExportImageAt(1)

	for name, pair := range map[string][2][]byte{
		"in order": {disk1, disk2},
		"reversed": {disk2, disk1},
	} {
		fresh := NewSession()
		snap, cerr := fresh.OpenImagePair(pair[0], pair[1])
		if cerr != nil {
			t.Fatalf("%s: OpenImagePair: %v", name, cerr)
		}
		if snap.Disk == nil || snap.Disk.Disks != 2 || snap.Disk.MissingDisk != 0 {
			t.Fatalf("%s: disk = %+v, want a whole 2 disk document", name, snap.Disk)
		}
		if got := len(instrument(t, fresh).Voices); got != 3 {
			t.Fatalf("%s: voices = %d, want 3", name, got)
		}
		out1, _ := fresh.ExportImageAt(0)
		out2, _ := fresh.ExportImageAt(1)
		if !bytes.Equal(out1, disk1) || !bytes.Equal(out2, disk2) {
			t.Errorf("%s: exported pair differs from the opened pair", name)
		}
	}
}

func TestOpenImagePairRejectsMismatch(t *testing.T) {
	s, _ := splitSession(t)
	disk1, _ := s.ExportImageAt(0)

	fresh := NewSession()
	if _, cerr := fresh.OpenImagePair(disk1, disk1); cerr == nil || cerr.Code != codePairMismatch {
		t.Fatalf("expected pair-mismatch for two disk 1s, got %v", cerr)
	}
	blank := NewSession()
	if _, cerr := blank.NewDisk("LONE"); cerr != nil {
		t.Fatal(cerr)
	}
	lone := mustExport(t, blank)
	if _, cerr := fresh.OpenImagePair(disk1, lone); cerr == nil || cerr.Code != codePairMismatch {
		t.Fatalf("expected pair-mismatch for a dumpless image, got %v", cerr)
	}
}

// Edits on a split document stitch, patch, and re-split; replacing the
// instrument with a small one collapses to one disk, and undo restores
// the pair.
func TestSplitDocumentEditsAndCollapse(t *testing.T) {
	s, _ := splitSession(t)

	if _, cerr := s.SetAreaField(0, 0, "keyHigh", 40); cerr != nil {
		t.Fatalf("SetAreaField on a split document: %v", cerr)
	}
	if s.Snapshot().Disk.Disks != 2 {
		t.Fatal("the document should stay split after an edit")
	}
	if got := instrument(t, s).Banks[0].Areas[0].KeyHigh; got != 40 {
		t.Fatalf("keyHigh = %d, want 40", got)
	}

	if _, cerr := s.LoadFZF(testFZF(t, "TINY")); cerr != nil {
		t.Fatalf("LoadFZF small: %v", cerr)
	}
	if got := s.Snapshot().Disk.Disks; got != 1 {
		t.Fatalf("after a small replace, disks = %d, want 1", got)
	}
	if _, cerr := s.ExportImageAt(1); cerr == nil {
		t.Error("a one disk document must not export a disk 2")
	}

	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if got := s.Snapshot().Disk.Disks; got != 2 {
		t.Fatalf("undo should restore the pair, disks = %d", got)
	}
	if _, cerr := s.Redo(); cerr != nil {
		t.Fatalf("Redo: %v", cerr)
	}
	if got := s.Snapshot().Disk.Disks; got != 1 {
		t.Fatalf("redo should collapse again, disks = %d", got)
	}
}

// R5 flow control: one half of a pair opened alone names its missing
// twin so the UI can ask for it.
func TestOpenLoneSplitDiskFlagsMissing(t *testing.T) {
	s, _ := splitSession(t)
	disk1, _ := s.ExportImageAt(0)
	disk2, _ := s.ExportImageAt(1)

	fresh := NewSession()
	snap, cerr := fresh.OpenImage(disk1)
	if cerr != nil {
		t.Fatalf("OpenImage disk 1: %v", cerr)
	}
	if snap.Disk.MissingDisk != 2 {
		t.Errorf("disk 1 alone: missingDisk = %d, want 2", snap.Disk.MissingDisk)
	}

	snap, cerr = fresh.OpenImage(disk2)
	if cerr != nil {
		t.Fatalf("OpenImage disk 2: %v", cerr)
	}
	if snap.Disk.MissingDisk != 1 {
		t.Errorf("disk 2 alone: missingDisk = %d, want 1", snap.Disk.MissingDisk)
	}
}

// R5 plus E3: a lone half of a split pair carries a truncated dump.
// A size growing edit would re-split that truncation and format a
// fresh disk 2 over the real one; a header only edit would rewrite
// disk 1's wave count from the pair's total down to what this half
// holds, so the sampler stops asking for the other disk. Every
// mutating operation refuses, names the disk to fetch, and changes
// nothing.
func TestEditsOnALoneHalfRefuse(t *testing.T) {
	s, _ := splitSession(t)
	disk1, _ := s.ExportImageAt(0)

	lone := NewSession()
	if _, cerr := lone.OpenImage(disk1); cerr != nil {
		t.Fatalf("OpenImage disk 1: %v", cerr)
	}
	if got := lone.Snapshot().Disk.MissingDisk; got != 2 {
		t.Fatalf("missingDisk = %d, want 2", got)
	}
	before := mustExport(t, lone)

	edits := map[string]func() *Error{
		"area edit":         func() *Error { _, cerr := lone.SetAreaField(0, 0, "keyHigh", 40); return cerr },
		"voice rename":      func() *Error { _, cerr := lone.RenameVoiceSlot(0, "RENAMED"); return cerr },
		"voice param":       func() *Error { _, cerr := lone.SetSlotParamNumber(0, fieldCutoff, 40); return cerr },
		"size growing join": func() *Error { _, cerr := lone.AddVoice(testFZV(t, "EXTRA", 120000)); return cerr },
		"instrument swap":   func() *Error { _, cerr := lone.LoadFZF(testFZF(t, "TINY")); return cerr },
		"disk rename":       func() *Error { _, cerr := lone.RenameDisk("OTHER"); return cerr },
		"file delete":       func() *Error { _, cerr := lone.DeleteFile(disk.FullDumpName); return cerr },
		"wav folder import": func() *Error {
			_, cerr := lone.ImportWAVFolder(map[string][]byte{"a.wav": wavBytes(t, 500)}, 18000, false, ChannelMix)
			return cerr
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			cerr := edit()
			if cerr == nil || cerr.Code != codeMissingDisk {
				t.Fatalf("cerr = %v, want %s", cerr, codeMissingDisk)
			}
			if !strings.Contains(cerr.Message, "disk 2") {
				t.Errorf("message %q does not name the absent disk", cerr.Message)
			}
			if !bytes.Equal(mustExport(t, lone), before) {
				t.Error("the refused edit changed disk 1")
			}
			snap := lone.Snapshot().Disk
			if snap.MissingDisk != 2 || snap.Disks != 1 {
				t.Errorf("after the refusal: missingDisk = %d, disks = %d, want 2 and 1", snap.MissingDisk, snap.Disks)
			}
		})
	}

}

// The refusal is on the mutations only: the shell opens a lone half
// deliberately, so the user can still look at what they opened, hear
// it, and export it.
func TestReadPathsWorkOnALoneHalf(t *testing.T) {
	s, _ := splitSession(t)
	disk1, _ := s.ExportImageAt(0)

	lone := NewSession()
	if _, cerr := lone.OpenImage(disk1); cerr != nil {
		t.Fatalf("OpenImage disk 1: %v", cerr)
	}
	if snap := lone.Snapshot(); snap.Disk == nil || snap.Disk.Instrument == nil {
		t.Fatal("a lone half still reads as a document")
	}
	if _, cerr := lone.AuditionSlot(0); cerr != nil {
		t.Errorf("AuditionSlot on a lone half: %v", cerr)
	}
	if _, cerr := lone.SlotPeaks(0, 0, 1000, 16); cerr != nil {
		t.Errorf("SlotPeaks on a lone half: %v", cerr)
	}
	if _, _, cerr := lone.ExtractVoiceSlot(0, ExtractFZV); cerr != nil {
		t.Errorf("ExtractVoiceSlot on a lone half: %v", cerr)
	}
	if _, cerr := lone.ExportImage(); cerr != nil {
		t.Errorf("ExportImage on a lone half: %v", cerr)
	}
	if _, cerr := lone.ExtractFile(disk.FullDumpName); cerr != nil {
		t.Errorf("ExtractFile on a lone half: %v", cerr)
	}
}

// The remedy the refusal names works from where it leaves the user:
// opening the pair from a lone half clears the banner and the edits
// go through.
func TestOpeningThePairFromALoneHalfRestoresEditing(t *testing.T) {
	s, _ := splitSession(t)
	disk1, _ := s.ExportImageAt(0)
	disk2, _ := s.ExportImageAt(1)

	lone := NewSession()
	if _, cerr := lone.OpenImage(disk1); cerr != nil {
		t.Fatalf("OpenImage disk 1: %v", cerr)
	}
	if _, cerr := lone.SetAreaField(0, 0, "keyHigh", 40); cerr == nil {
		t.Fatal("the lone half should refuse the edit")
	}

	snap, cerr := lone.OpenImagePair(disk1, disk2)
	if cerr != nil {
		t.Fatalf("OpenImagePair: %v", cerr)
	}
	if snap.Disk.MissingDisk != 0 || snap.Disk.Disks != 2 {
		t.Fatalf("disk = %+v, want a whole 2 disk document", snap.Disk)
	}
	if _, cerr := lone.SetAreaField(0, 0, "keyHigh", 40); cerr != nil {
		t.Fatalf("SetAreaField on the reunited pair: %v", cerr)
	}
	if got := instrument(t, lone).Banks[0].Areas[0].KeyHigh; got != 40 {
		t.Fatalf("keyHigh = %d, want 40", got)
	}
}

// The continuation half refuses the same way, naming disk 1.
func TestEditsOnALoneContinuationRefuse(t *testing.T) {
	s, _ := splitSession(t)
	disk2, _ := s.ExportImageAt(1)

	lone := NewSession()
	if _, cerr := lone.OpenImage(disk2); cerr != nil {
		t.Fatalf("OpenImage disk 2: %v", cerr)
	}
	if got := lone.Snapshot().Disk.MissingDisk; got != 1 {
		t.Fatalf("missingDisk = %d, want 1", got)
	}
	before := mustExport(t, lone)

	_, cerr := lone.RenameDisk("OTHER")
	if cerr == nil || cerr.Code != codeMissingDisk {
		t.Fatalf("cerr = %v, want %s", cerr, codeMissingDisk)
	}
	if !strings.Contains(cerr.Message, "disk 1") {
		t.Errorf("message %q does not name the absent disk", cerr.Message)
	}
	if !bytes.Equal(mustExport(t, lone), before) {
		t.Error("the refused edit changed the continuation disk")
	}
}

// A complete one disk instrument needs no continuation, so pairing it
// with a stray disk 2 is nonsense whatever that disk holds. The stitch
// runs an unrelated instrument's audio onto the end of a whole dump,
// and every read of the document afterwards sees a file of the wrong
// length: the reviewer's ExtractFile returned 503,808 bytes where the
// real dump is 9,216.
func TestOpenImagePairRefusesACompleteDiskOne(t *testing.T) {
	whole := twoVoiceSession(t)
	wholeImage := mustExport(t, whole)
	wholeDump := dumpBytes(t, whole)

	split, _ := splitSession(t)
	stray, _ := split.ExportImageAt(1)

	fresh := NewSession()
	_, cerr := fresh.OpenImagePair(wholeImage, stray)
	if cerr == nil || cerr.Code != codePairMismatch {
		t.Fatalf("expected pair-mismatch for a complete disk 1, got %v", cerr)
	}
	if fresh.Snapshot().Disk != nil {
		t.Error("the refused pair must not open a document")
	}

	// The same image opened alone is a whole document, and its dump is
	// the length it was written at.
	lone := NewSession()
	if _, cerr := lone.OpenImage(wholeImage); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	if got := lone.Snapshot().Disk.MissingDisk; got != 0 {
		t.Errorf("missingDisk = %d, want 0: this image needs no continuation", got)
	}
	got, cerr := lone.ExtractFile(disk.FullDumpName)
	if cerr != nil {
		t.Fatalf("ExtractFile: %v", cerr)
	}
	if len(got) != len(wholeDump) {
		t.Errorf("the extracted dump is %d bytes, want %d", len(got), len(wholeDump))
	}
}

// A one disk document reports no missing half and one disk capacity.
func TestSingleDiskSnapshotShape(t *testing.T) {
	s := twoVoiceSession(t)
	snap := s.Snapshot()
	if snap.Disk.Disks != 1 || snap.Disk.MissingDisk != 0 {
		t.Fatalf("disk = %+v, want a whole 1 disk document", snap.Disk)
	}
	if snap.Disk.CapacityBytes != disk.ImageSize {
		t.Errorf("capacity = %d, want %d", snap.Disk.CapacityBytes, disk.ImageSize)
	}
}

// R10: a split refuses to drop the disk's other files silently.
func TestSplitRefusesWithLooseFiles(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("BUSY"); cerr != nil {
		t.Fatal(cerr)
	}
	if _, cerr := s.ImportWAV("loose.wav", wavBytes(t, 500), 18000, ChannelMix); cerr != nil {
		t.Fatal(cerr)
	}
	voices, groups := bigVoices(t)
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	_, cerr := s.LoadFZF(fzf)
	if cerr == nil || cerr.Code != "no-space" {
		t.Fatalf("expected no-space with a loose file present, got %v", cerr)
	}
}

// The FZ-1 does not always stamp the total wave marker. Without it the
// pair check falls back to the furthest voice's wave end, which still
// catches a continuation that is too short to hold this instrument's
// audio.
func TestCheckContinuationCatchesShortAudioWithoutTheMarker(t *testing.T) {
	voices := make([][]byte, 3)
	groups := make([]voicebuild.Keygroup, 3)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("MRK%02d", i+1), 40000)
		lo := uint8(50 + i) // #nosec G115 -- small test values
		groups[i] = voicebuild.NewKeygroup(lo, lo, lo)
	}
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := fzutil.ParseFZFHeader(fzf)
	if err != nil {
		t.Fatal(err)
	}

	if cerr := checkContinuation(fzf, hdr); cerr != nil {
		t.Fatalf("a whole dump must pass: %v", cerr)
	}

	// Strike the marker, as a stored image can carry it.
	blank := bytes.Clone(fzf)
	binary.LittleEndian.PutUint32(blank[disk.BankTotalWaveOffset:], 0)
	if cerr := checkContinuation(blank, hdr); cerr != nil {
		t.Fatalf("a whole dump with no marker must still pass: %v", cerr)
	}

	// A continuation short of the last voice's audio, with no marker to
	// measure it against.
	short := blank[:len(blank)-4*disk.SectorSize]
	cerr := checkContinuation(short, hdr)
	if cerr == nil || cerr.Code != codePairMismatch {
		t.Fatalf("expected pair-mismatch for short audio with no marker, got %v", cerr)
	}
}

// Two disks from unrelated split instruments must not stitch: disk 1
// says how much audio the instrument needs, and a stranger's disk 2
// does not carry it.
func TestOpenImagePairRejectsAForeignContinuation(t *testing.T) {
	a, _ := splitSession(t)
	aDisk1, _ := a.ExportImageAt(0)

	// A second split instrument with more audio: its disk 2 is a
	// different continuation of a different length.
	other := make([][]byte, 4)
	otherGroups := make([]voicebuild.Keygroup, 4)
	for i := range other {
		other[i] = testutil.MakeTestVoice(fmt.Sprintf("OTH%02d", i+1), 240000)
		lo := uint8(40 + i) // #nosec G115 -- small test values
		otherGroups[i] = voicebuild.NewKeygroup(lo, lo, lo)
	}
	fzf, err := voicebuild.AssembleWithKeygroups(other, otherGroups)
	if err != nil {
		t.Fatal(err)
	}
	b := NewSession()
	if _, cerr := b.LoadFZF(fzf); cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	if b.Snapshot().Disk.Disks != 2 {
		t.Fatal("the second instrument did not split")
	}
	bDisk2, _ := b.ExportImageAt(1)

	fresh := NewSession()
	_, cerr := fresh.OpenImagePair(aDisk1, bDisk2)
	if cerr == nil || cerr.Code != codePairMismatch {
		t.Fatalf("expected pair-mismatch for a foreign continuation, got %v", cerr)
	}
}
