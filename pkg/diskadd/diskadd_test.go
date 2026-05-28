package diskadd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
)

func TestBuildDISContiguous(t *testing.T) {
	t.Parallel()
	// DIS at sector 2, data sectors 3-6 (contiguous after DIS).
	// Expected: one extent [2,6]. DIS is prepended as ss0.
	dis := buildDIS(2, []int{3, 4, 5, 6}, 0, 1, 3)
	if len(dis.Extents) != 1 {
		t.Fatalf("expected 1 extent for contiguous sectors, got %d", len(dis.Extents))
	}
	if dis.Extents[0][0] != 2 || dis.Extents[0][1] != 6 {
		t.Errorf("extent: got [%d,%d], want [2,6]", dis.Extents[0][0], dis.Extents[0][1])
	}
	if dis.VoiceCount != 1 || dis.WaveCount != 3 {
		t.Errorf("counts: voice=%d wave=%d", dis.VoiceCount, dis.WaveCount)
	}
}

func TestBuildDISNonContiguous(t *testing.T) {
	t.Parallel()
	// DIS at sector 2, data at 3-4 and 10-11 (non-contiguous).
	// Expected: two extents [2,4] and [10,11].
	dis := buildDIS(2, []int{3, 4, 10, 11}, 0, 1, 3)
	if len(dis.Extents) != 2 {
		t.Fatalf("expected 2 extents for non-contiguous sectors, got %d", len(dis.Extents))
	}
	if dis.Extents[0] != ([2]uint16{2, 4}) {
		t.Errorf("extent[0]: got %v, want [2,4]", dis.Extents[0])
	}
	if dis.Extents[1] != ([2]uint16{10, 11}) {
		t.Errorf("extent[1]: got %v, want [10,11]", dis.Extents[1])
	}
}

func TestDetectFileVoice(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize*2)
	copy(data[disk.VoiceNameOffset:], "KICK        ")

	fi, err := detectFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if fi.fileType != disk.TypeVoice {
		t.Errorf("file type: got %d, want %d (Voice)", fi.fileType, disk.TypeVoice)
	}
	if fi.nvoice != 1 {
		t.Errorf("nvoice: got %d, want 1", fi.nvoice)
	}
}

func TestDetectFileTooSmall(t *testing.T) {
	t.Parallel()
	_, err := detectFile([]byte{0x01})
	if err == nil {
		t.Error("expected error for 1-byte file")
	}
}

// TestAddDiskFullError is a regression test for the disk-full error message.
// Previously it reported only internal sector counts with no context. It should
// now report the file size and disk capacity in MB so the user understands why.
func TestAddDiskFullError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "tiny.img")
	if err := diskformat.Format(imgPath, "TINY"); err != nil {
		t.Fatal(err)
	}

	// Build a file larger than the disk (1.25 MB). We write raw bytes with a
	// printable name at disk.VoiceNameOffset so it's detected as a voice file.
	oversized := make([]byte, disk.ImageSize+disk.SectorSize)
	copy(oversized[disk.VoiceNameOffset:], "TOOBIG      ")
	filePath := filepath.Join(dir, "big.fzv")
	if err := os.WriteFile(filePath, oversized, 0644); err != nil {
		t.Fatal(err)
	}

	err := Add(imgPath, filePath, 0)
	if err == nil {
		t.Fatal("expected error when file exceeds disk capacity")
	}
	msg := err.Error()
	if !strings.Contains(msg, "MB") {
		t.Errorf("error should mention size in MB, got: %q", msg)
	}
	if !strings.Contains(msg, "not enough space") {
		t.Errorf("error should say 'not enough space', got: %q", msg)
	}
	doubled := strings.Repeat("diskadd: ", 2)
	if strings.Contains(msg, doubled) {
		t.Errorf("error should not have a doubled package prefix, got: %q", msg)
	}
}

// TestFullDumpUsagesMagicName is a regression test for the "Next disk?" bug.
// The FZ-1 firmware identifies full dump files by the magic directory name
// "FULL-DATA-FZ". Any other name causes the sampler to mis-identify the file
// and prompt for a second disk. This test verifies that name is always used.
func TestFullDumpUsesMagicName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	// Build a minimal FZF: bank sector with nvoice=1, one voice sector,
	// one wave sector. No printable name at disk.VoiceNameOffset so it's detected as full dump.
	fzfData := make([]byte, 3*disk.SectorSize)
	// bank sector: nvoice = 1 at bytes 0-1
	fzfData[0] = 1
	fzfData[1] = 0
	// bank name at 0x282
	copy(fzfData[disk.BankNameOffset:], "All Voices  ")
	fzfPath := filepath.Join(dir, "MYKIT.fzf")
	if err := os.WriteFile(fzfPath, fzfData, 0644); err != nil {
		t.Fatal(err)
	}

	if err := Add(imgPath, fzfPath, 0); err != nil {
		t.Fatal(err)
	}

	// Read back the directory entry and check the name.
	imgBytes, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	dirEntry := string(imgBytes[disk.SectorSize : disk.SectorSize+disk.LabelSize])
	if dirEntry != "FULL-DATA-FZ" {
		t.Errorf("full dump directory name: got %q, want %q (sampler will prompt for next disk)", dirEntry, "FULL-DATA-FZ")
	}
}

// TestAddMultiDiskDisk1WnIsTotal verifies that when a disk 1 FZF has the
// multi-disk total wave marker set AND at least one voice slot whose wavst
// points past the local audio area, diskadd writes that larger value as wn
// in the DIS tail. The sampler uses wn to know more data exists on disk 2.
//
// The boundary voice is required because the FZ-1 firmware does not always
// initialise BankTotalWaveOffset, so a high value alone is not sufficient
// evidence of a real split (see TestAddIgnoresGarbageTotalWaveMarker).
func TestAddMultiDiskDisk1WnIsTotal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	fzf := make([]byte, 5*disk.SectorSize)
	binary.LittleEndian.PutUint16(fzf[0:], 3) // nvoice = 3
	copy(fzf[disk.BankNameOffset:], "All Voices  ")
	binary.LittleEndian.PutUint32(fzf[disk.BankTotalWaveOffset:], 900)

	// Local audio area = 3 sectors = 3072 bytes => 1536 samples at 2 bytes/sample.
	// Plant a plausible voice in slot 0 whose wavst (in samples) lies past the
	// local audio so it corroborates the multi-disk marker.
	voiceArea := disk.SectorSize
	slot0 := voiceArea + disk.VoiceSlotOffset(0, 0)
	binary.LittleEndian.PutUint16(fzf[slot0+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	// wavst = 4000 samples * 2 bytes = 8000 bytes >= 3072 (localAudioBytes).
	binary.LittleEndian.PutUint32(fzf[slot0+disk.VoiceWaveStartOffset:], 4000)
	binary.LittleEndian.PutUint32(fzf[slot0+disk.VoiceWaveEndOffset:], 5000)

	fzfPath := filepath.Join(dir, "disk1.fzf")
	if err := os.WriteFile(fzfPath, fzf, 0644); err != nil {
		t.Fatal(err)
	}

	if err := Add(imgPath, fzfPath, 0); err != nil {
		t.Fatal(err)
	}

	imgData, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	de, err := disk.DecodeDirEntry(imgData[disk.SectorSize : disk.SectorSize+disk.DirEntrySize])
	if err != nil {
		t.Fatal(err)
	}
	dissec := int(de.DisSector)
	dis := imgData[dissec*disk.SectorSize : (dissec+1)*disk.SectorSize]
	wn := int(binary.LittleEndian.Uint16(dis[disk.DisWaveCountOffset : disk.DisWaveCountOffset+2]))
	if wn != 900 {
		t.Errorf("DIS wn=%d, want 900 (total from marker); sampler won't prompt for disk 2", wn)
	}
}

// TestAddIgnoresGarbageTotalWaveMarker verifies that detectFile does NOT
// honour a high BankTotalWaveOffset value when no voice slot corroborates
// it. Real-world FZFs frequently carry uninitialised garbage in this field
// (the FZ-1 firmware doesn't always write it), and inflating wn from
// garbage causes the sampler to ask for a phantom disk 2.
//
// This mirrors the corroboration heuristic in fzfinfo.Parse: a high marker
// is only adopted when at least one plausible voice slot has wavst pointing
// past the local audio area.
func TestAddIgnoresGarbageTotalWaveMarker(t *testing.T) {
	t.Parallel()

	// 5 sectors = 1 bank + 1 voice area + 3 audio sectors.
	// localAudioBytes = 3072, so localWaveSectors = 3.
	fzf := make([]byte, 5*disk.SectorSize)
	binary.LittleEndian.PutUint16(fzf[0:], 3) // nvoice = 3
	copy(fzf[disk.BankNameOffset:], "Garbage Mkr ")
	binary.LittleEndian.PutUint32(fzf[disk.BankTotalWaveOffset:], 0xCAFEBABE)

	// Plant three plausible voice slots, all with wavst safely inside the
	// local audio area so none of them corroborates the garbage marker.
	voiceArea := disk.SectorSize
	for i := range 3 {
		slot := voiceArea + disk.VoiceSlotOffset(0, i)
		binary.LittleEndian.PutUint16(fzf[slot+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		// wavst = 100 samples * 2 = 200 bytes, well inside 3072.
		binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceWaveStartOffset:], 100)
		binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceWaveEndOffset:], 200)
	}

	fi, err := detectFile(fzf)
	if err != nil {
		t.Fatalf("detectFile: %v", err)
	}
	// Expected nwave = totalSectors - nbank - voiceSectors = 5 - 1 - 1 = 3.
	if fi.nwave != 3 {
		t.Errorf("nwave = %d, want 3 (local sectors only; garbage marker 0xCAFEBABE must be ignored without a corroborating boundary voice)", fi.nwave)
	}
}

// TestAddHonoursMarkerWithBoundaryVoice is the positive twin of
// TestAddIgnoresGarbageTotalWaveMarker: the same high BankTotalWaveOffset
// value IS adopted when at least one plausible voice slot has wavst past
// the local audio area, signalling a real disk-1-of-2 split.
func TestAddHonoursMarkerWithBoundaryVoice(t *testing.T) {
	t.Parallel()

	const totalWave = 0x00000384 // 900 sectors
	fzf := make([]byte, 5*disk.SectorSize)
	binary.LittleEndian.PutUint16(fzf[0:], 3) // nvoice = 3
	copy(fzf[disk.BankNameOffset:], "Split Disk1 ")
	binary.LittleEndian.PutUint32(fzf[disk.BankTotalWaveOffset:], totalWave)

	// Plant two slots inside local audio, one boundary slot past it.
	voiceArea := disk.SectorSize
	for i := range 2 {
		slot := voiceArea + disk.VoiceSlotOffset(0, i)
		binary.LittleEndian.PutUint16(fzf[slot+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceWaveStartOffset:], 100)
		binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceWaveEndOffset:], 200)
	}
	boundary := voiceArea + disk.VoiceSlotOffset(0, 2)
	binary.LittleEndian.PutUint16(fzf[boundary+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	// wavst = 4000 samples * 2 = 8000 bytes >= 3072 localAudioBytes.
	binary.LittleEndian.PutUint32(fzf[boundary+disk.VoiceWaveStartOffset:], 4000)
	binary.LittleEndian.PutUint32(fzf[boundary+disk.VoiceWaveEndOffset:], 5000)

	fi, err := detectFile(fzf)
	if err != nil {
		t.Fatalf("detectFile: %v", err)
	}
	if fi.nwave != totalWave {
		t.Errorf("nwave = %d, want %d (corroborating boundary voice present, marker must be honoured)", fi.nwave, totalWave)
	}
}

// TestHeuristicPathHonoursMultiDiskMarker verifies that when bank-0 bstep is
// zero (forcing detectFile to fall through to the heuristic path), the
// multi-disk total-wave marker at BankTotalWaveOffset is still honoured if
// a plausible voice slot corroborates it. Without this, disk-1 FZFs whose
// bstep is corrupt or unset would get nwave = local sectors only, breaking
// the sampler's "Next disk?" prompt.
func TestHeuristicPathHonoursMultiDiskMarker(t *testing.T) {
	t.Parallel()

	const totalWave = 900
	// 5 sectors = 1 bank + 1 voice area + 3 audio sectors.
	fzf := make([]byte, 5*disk.SectorSize)
	// bstep = 0 so detectFile takes the heuristic path. A printable bank
	// name at BankNameOffset is what isBankSector matches on.
	copy(fzf[disk.BankNameOffset:], "Heur Disk1  ")
	binary.LittleEndian.PutUint32(fzf[disk.BankTotalWaveOffset:], totalWave)

	// Plant a plausible voice in slot 0 of the voice sector with wavst past
	// the local audio area (3 sectors = 3072 bytes; 4000 samples * 2 = 8000).
	voiceArea := disk.SectorSize
	slot0 := voiceArea + disk.VoiceSlotOffset(0, 0)
	copy(fzf[slot0+disk.VoiceNameOffset:], "BOUNDARY    ")
	fzf[slot0+disk.VoiceSampOffset] = 0
	binary.LittleEndian.PutUint16(fzf[slot0+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	binary.LittleEndian.PutUint32(fzf[slot0+disk.VoiceWaveStartOffset:], 4000)
	binary.LittleEndian.PutUint32(fzf[slot0+disk.VoiceWaveEndOffset:], 5000)

	fi, err := detectFile(fzf)
	if err != nil {
		t.Fatalf("detectFile: %v", err)
	}
	if fi.fileType != disk.TypeFullDump {
		t.Errorf("fileType = %d, want %d (full dump)", fi.fileType, disk.TypeFullDump)
	}
	if fi.nwave != totalWave {
		t.Errorf("nwave = %d, want %d (heuristic path must honour multi-disk marker when corroborated)", fi.nwave, totalWave)
	}
}

// TestHeuristicPathIgnoresGarbageMarker mirrors TestAddIgnoresGarbageTotalWaveMarker
// but forces the heuristic path (bstep=0). A high BankTotalWaveOffset value
// must be ignored when no voice slot corroborates it.
func TestHeuristicPathIgnoresGarbageMarker(t *testing.T) {
	t.Parallel()

	// 5 sectors = 1 bank + 1 voice area + 3 audio sectors.
	fzf := make([]byte, 5*disk.SectorSize)
	// bstep = 0 to force the heuristic path.
	copy(fzf[disk.BankNameOffset:], "Heur Garb   ")
	binary.LittleEndian.PutUint32(fzf[disk.BankTotalWaveOffset:], 0xCAFEBABE)

	// Plant plausible voice slots inside the local audio area so none of
	// them corroborates the garbage marker.
	voiceArea := disk.SectorSize
	for i := range 3 {
		slot := voiceArea + disk.VoiceSlotOffset(0, i)
		copy(fzf[slot+disk.VoiceNameOffset:], "INSIDEVOICE ")
		fzf[slot+disk.VoiceSampOffset] = 0
		binary.LittleEndian.PutUint16(fzf[slot+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceWaveStartOffset:], 100)
		binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceWaveEndOffset:], 200)
	}

	fi, err := detectFile(fzf)
	if err != nil {
		t.Fatalf("detectFile: %v", err)
	}
	// totalSectors - nbank - voiceSectors. Heuristic counts 1 bank + 1
	// voice sector + 3 audio sectors.
	if fi.nbank != 1 {
		t.Fatalf("nbank = %d, want 1 (precondition: heuristic must detect one bank)", fi.nbank)
	}
	if fi.nwave != 3 {
		t.Errorf("nwave = %d, want 3 (heuristic path must ignore garbage marker without a corroborating boundary voice)", fi.nwave)
	}
}

// TestAddPreservesImageOnFailure verifies the disk image is not corrupted when
// an add operation fails (e.g. disk full). The image must remain intact.
func TestAddPreservesImageOnFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "disk.img")
	if err := diskformat.Format(imgPath, "SAFE"); err != nil {
		t.Fatal(err)
	}

	statBefore, err := os.Stat(imgPath)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt to add an oversized file.
	oversized := make([]byte, disk.ImageSize+disk.SectorSize)
	copy(oversized[disk.VoiceNameOffset:], "TOOBIG      ")
	filePath := filepath.Join(dir, "big.fzv")
	if err := os.WriteFile(filePath, oversized, 0644); err != nil {
		t.Fatal(err)
	}
	_ = Add(imgPath, filePath, 0)

	statAfter, err := os.Stat(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if statBefore.Size() != statAfter.Size() {
		t.Errorf("image size changed after failed add: before=%d, after=%d", statBefore.Size(), statAfter.Size())
	}
}

// TestAddDisSectorIsss0 is a regression test for the "Next disk?" bug.
// The DIS sector must be written as ss of the first extent (ss0 == disSector)
// so that the FZ-1 firmware can locate and parse the file correctly.
// When ss0 != disSector, the sampler reads garbage as the file header and
// may prompt for a second disk even when loading a single-disk instrument.
func TestAddDisSectorIsss0(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	fzv := make([]byte, disk.SectorSize*3)
	copy(fzv[disk.VoiceNameOffset:], "KICK        ")
	fzvPath := filepath.Join(dir, "kick.fzv")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzvPath, 0); err != nil {
		t.Fatal(err)
	}

	imgData, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}

	de, err := disk.DecodeDirEntry(imgData[disk.SectorSize : disk.SectorSize+disk.DirEntrySize])
	if err != nil {
		t.Fatal(err)
	}
	disSector := int(de.DisSector)

	// Read DIS sector and check ss0 == disSector.
	dis := imgData[disSector*disk.SectorSize : (disSector+1)*disk.SectorSize]
	ss0 := int(binary.LittleEndian.Uint16(dis[0:2]))

	if ss0 != disSector {
		t.Errorf("ss0=%d != disSector=%d: sampler will not find file correctly (Next disk? bug)", ss0, disSector)
	}
}

func TestAddSecondFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	fzv1 := make([]byte, disk.SectorSize*3)
	copy(fzv1[disk.VoiceNameOffset:], "KICK        ")
	fzv1Path := filepath.Join(dir, "kick.fzv")
	if err := os.WriteFile(fzv1Path, fzv1, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzv1Path, 0); err != nil {
		t.Fatalf("Add first file: %v", err)
	}

	fzv2 := make([]byte, disk.SectorSize*3)
	copy(fzv2[disk.VoiceNameOffset:], "SNARE       ")
	fzv2Path := filepath.Join(dir, "snare.fzv")
	if err := os.WriteFile(fzv2Path, fzv2, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzv2Path, 0); err != nil {
		t.Fatalf("Add second file: %v", err)
	}

	imgData, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}

	var found []string
	for i := range disk.MaxDirEntries {
		off := disk.SectorSize + i*disk.DirEntrySize
		name := strings.TrimRight(string(imgData[off:off+disk.LabelSize]), "\x00 ")
		if name != "" {
			found = append(found, name)
		}
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 directory entries, got %d: %v", len(found), found)
	}
	if found[0] != "KICK" {
		t.Errorf("entry 0: got %q, want %q", found[0], "KICK")
	}
	if found[1] != "SNARE" {
		t.Errorf("entry 1: got %q, want %q", found[1], "SNARE")
	}
}

func TestAddBytesHappyPath(t *testing.T) {
	t.Parallel()
	imgPath := filepath.Join(t.TempDir(), "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatalf("Format: %v", err)
	}

	fileData := make([]byte, 2*disk.SectorSize)
	for i := range fileData {
		fileData[i] = byte(i % 256)
	}
	name := disk.PadLabel("TESTFILE")

	err := AddBytes(imgPath, fileData, name, disk.TypeVoice, 0, 0, 1, 1)
	if err != nil {
		t.Fatalf("AddBytes: %v", err)
	}

	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatalf("opening image: %v", err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatalf("reading directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 directory entry, got %d", len(entries))
	}
	if entries[0].NameString() != "TESTFILE" {
		t.Errorf("name: got %q, want %q", entries[0].NameString(), "TESTFILE")
	}
}

// makeProgramFile builds a 1024-byte FZ-1 Type-5 Program payload whose
// first 14 bytes match the standard preamble shared by every program on
// the factory OPT_SOFTWARE diagnostic disk. The body after the preamble
// is left zero. Real programs put main() there, but the byte content
// after the preamble does not affect detection or directory wiring.
func makeProgramFile() []byte {
	p := make([]byte, disk.SectorSize)
	copy(p, []byte{
		0xE8, 0x0B, 0x00, 0xCB, 0x8F, 0x06, 0xF6, 0x55,
		0xCC, 0xFF, 0x36, 0xF6, 0x55, 0xC3,
	})
	return p
}

func TestDetectFileProgram(t *testing.T) {
	t.Parallel()
	data := makeProgramFile()
	fi, err := detectFile(data)
	if err != nil {
		t.Fatalf("detectFile: %v", err)
	}
	if fi.fileType != disk.TypeProgram {
		t.Errorf("file type: got %d, want %d (Program)", fi.fileType, disk.TypeProgram)
	}
	// detectFile leaves the name field zero for Program files. Add()
	// fills it from the host filepath basename, since programs carry no
	// name field in the file itself.
	var zero [disk.LabelSize]byte
	if fi.name != zero {
		t.Errorf("name: got %q, want zero (Add fills it from filepath)", fi.name)
	}
}

func TestAddProgramFromBin(t *testing.T) {
	t.Parallel()
	// Parameterise on the input filename to verify the basename->disk-name
	// derivation: strip extension, uppercase, truncate to LabelSize.
	cases := []struct {
		filename string
		wantName string
	}{
		{"DEMO.bin", "DEMO"},
		{"lowercase.bin", "LOWERCASE"},
		{"verylongname.bin", "VERYLONGNAME"},   // exactly 12 chars
		{"evenlongername.bin", "EVENLONGERNA"}, // truncated to 12
		{"NoExtension", "NOEXTENSION"},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			binPath := filepath.Join(dir, tc.filename)
			if err := os.WriteFile(binPath, makeProgramFile(), 0o644); err != nil {
				t.Fatal(err)
			}
			imgPath := filepath.Join(dir, "test.img")
			if err := diskformat.Format(imgPath, "TEST"); err != nil {
				t.Fatal(err)
			}

			if err := Add(imgPath, binPath, 0); err != nil {
				t.Fatalf("Add: %v", err)
			}

			img, err := disk.OpenImage(imgPath)
			if err != nil {
				t.Fatalf("OpenImage: %v", err)
			}
			entries, err := img.Directory()
			if err != nil {
				t.Fatalf("Directory: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			if entries[0].FileType != disk.TypeProgram {
				t.Errorf("FileType: got %d, want %d (Program)", entries[0].FileType, disk.TypeProgram)
			}
			if got := entries[0].NameString(); got != tc.wantName {
				t.Errorf("NameString: got %q, want %q", got, tc.wantName)
			}
		})
	}
}

func TestAddBytesProgramRoundTrip(t *testing.T) {
	t.Parallel()
	imgPath := filepath.Join(t.TempDir(), "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	payload := makeProgramFile()
	name := disk.PadLabel("DEMO")

	if err := AddBytes(imgPath, payload, name, disk.TypeProgram, 0, 0, 0, 0); err != nil {
		t.Fatalf("AddBytes: %v", err)
	}

	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatalf("OpenImage: %v", err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}
	if len(entries) != 1 || entries[0].FileType != disk.TypeProgram {
		t.Fatalf("expected one Program entry, got entries=%+v", entries)
	}
	if entries[0].NameString() != "DEMO" {
		t.Errorf("name: got %q, want DEMO", entries[0].NameString())
	}
}

func TestBuildDISEmptySectors(t *testing.T) {
	t.Parallel()
	dis := buildDIS(0, nil, 0, 0, 0)
	encoded := disk.EncodeDisSector(dis)
	decoded, err := disk.DecodeDisSector(encoded)
	if err != nil {
		t.Fatalf("decoding DIS: %v", err)
	}
	if len(decoded.Extents) != 0 {
		t.Errorf("expected 0 extents, got %d", len(decoded.Extents))
	}
}

func TestDetectFileHeuristicFallback(t *testing.T) {
	t.Parallel()
	data := make([]byte, 5*disk.SectorSize)
	copy(data[disk.BankNameOffset:], "HEURISTIC   ")
	fi, err := detectFile(data)
	if err != nil {
		t.Fatalf("detectFile: %v", err)
	}
	if fi.fileType != disk.TypeFullDump {
		t.Errorf("expected FullDump type, got %d", fi.fileType)
	}
}

func TestAddNonexistentImage(t *testing.T) {
	t.Parallel()
	err := Add("/nonexistent/path/image.img", "/nonexistent/file.fzv", 0)
	if err == nil {
		t.Fatal("expected error for nonexistent image")
	}
}

func TestAddNonexistentFile(t *testing.T) {
	t.Parallel()
	imgPath := filepath.Join(t.TempDir(), "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatalf("Format: %v", err)
	}
	err := Add(imgPath, "/nonexistent/file.fzv", 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestDetectFileMinimalVoice(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize)
	copy(data[disk.VoiceNameOffset:], "MINIMAL TEST")
	fi, err := detectFile(data)
	if err != nil {
		t.Fatalf("detectFile: %v", err)
	}
	if fi.fileType != disk.TypeVoice {
		t.Errorf("expected Voice type, got %d", fi.fileType)
	}
	if fi.nwave != 0 {
		t.Errorf("expected nwave=0 for 1-sector voice, got %d", fi.nwave)
	}
}

func TestDetectFileRejectsTextWithPrintableName(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize*2)
	copy(data[disk.VoiceNameOffset:], "from the lat")
	data[disk.VoiceSampOffset] = 'e'
	_, err := detectFile(data)
	if err == nil {
		t.Fatal("expected error for text-like data with invalid rate index")
	}
}

func TestDetectFileAcceptsValidVoiceWithEachRate(t *testing.T) {
	t.Parallel()
	for rate := range byte(3) {
		data := make([]byte, disk.SectorSize*2)
		copy(data[disk.VoiceNameOffset:], "KICK        ")
		data[disk.VoiceSampOffset] = rate
		fi, err := detectFile(data)
		if err != nil {
			t.Fatalf("rate %d: unexpected error: %v", rate, err)
		}
		if fi.fileType != disk.TypeVoice {
			t.Errorf("rate %d: expected Voice, got %d", rate, fi.fileType)
		}
	}
}

// TestDetectFileMultiBank guards a regression where detectFile hardcoded
// nbank=1, so multi-bank FZFs (e.g. the factory Clarinet.fzf with 4 banks)
// got the wrong bn count in their DIS tail and the firmware would only
// see bank 0. detectFile must now walk the bank-sector chain.
func TestDetectFileMultiBank(t *testing.T) {
	t.Parallel()

	// Build a synthetic 3-bank FZF: three valid bank sectors at offsets
	// 0x000, 0x400, 0x800, then one voice slot at 0xC00 with a
	// playable header, then a single 1024-byte audio sector.
	const banks = 3
	const voicesPerBank = 4
	const totalVoices = banks * voicesPerBank
	voiceAreaSectors := (totalVoices + 3) / 4
	totalSectors := banks + voiceAreaSectors + 1 // +1 audio sector
	data := make([]byte, totalSectors*disk.SectorSize)

	for b := range banks {
		off := b * disk.SectorSize
		binary.LittleEndian.PutUint16(data[off+disk.BankVoiceCountOffset:], uint16(voicesPerBank)) //nolint:gosec // test constant
		copy(data[off+disk.BankNameOffset:], "Multi Bank   ")
	}

	// One plausible voice slot at the start of the voice area is enough
	// for InferVoiceCount to accept (the other 11 stay zero/NoSound which
	// is also accepted as a placeholder).
	voiceArea := banks * disk.SectorSize
	binary.LittleEndian.PutUint16(data[voiceArea+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)

	fi, err := detectFile(data)
	if err != nil {
		t.Fatalf("detectFile: %v", err)
	}
	if fi.fileType != disk.TypeFullDump {
		t.Errorf("fileType = %d, want %d (full dump)", fi.fileType, disk.TypeFullDump)
	}
	if fi.nbank != banks {
		t.Errorf("nbank = %d, want %d (must count bank sectors, not hardcode 1)", fi.nbank, banks)
	}
	wantNwave := totalSectors - banks - voiceAreaSectors
	if fi.nwave != wantNwave {
		t.Errorf("nwave = %d, want %d (= total %d - banks %d - voice %d)",
			fi.nwave, wantNwave, totalSectors, banks, voiceAreaSectors)
	}
}

// TestHeuristicDetectFileVoiceCountCap is a regression test for an
// off-by-up-to-3 bug in heuristicDetectFile. The phaseVoices guard
// checked `fi.nvoice < disk.MaxVoices` *before* adding, but
// countVoicesInSector adds up to 4 voices per call, so nvoice could
// climb from 63 to 67 (exceeding MaxVoices=64). The DIS tail's vn field
// would then be out of spec and could mislead the firmware.
//
// We construct a synthetic FZF where the heuristic walks: 15 voice
// sectors of 4 voices each (60), then one sector with only 3 voices
// (63), then one sector with 4 voices. The pre-add guard would let the
// last sector add 4 -> 67. With the fix, the 64th cap is respected.
func TestHeuristicDetectFileVoiceCountCap(t *testing.T) {
	t.Parallel()

	const banks = 1
	const fullSectors = 15    // each contributes 4 voices -> 60
	const partialSlots = 3    // one sector contributes 3 voices -> 63
	const overflowSectors = 1 // one sector would contribute 4 voices -> 67
	voiceSectors := fullSectors + 1 + overflowSectors
	totalSectors := banks + voiceSectors
	data := make([]byte, totalSectors*disk.SectorSize)

	// Bank sector: leave bstep (BankVoiceCountOffset) zero so detectFile
	// falls through to the heuristic. Provide a printable bank name so
	// isBankSector accepts it.
	copy(data[disk.BankNameOffset:], "BankHeurist ")

	// Helper: populate `slots` plausible voice slots in the sector at
	// sectorIdx (relative to the start of the file).
	fillVoiceSector := func(sectorIdx, slots int) {
		secOff := sectorIdx * disk.SectorSize
		for slot := range slots {
			slotOff := secOff + slot*disk.VoicePackSize
			copy(data[slotOff+disk.VoiceNameOffset:], "VOICEABCDEFG")
			data[slotOff+disk.VoiceSampOffset] = 0
		}
	}

	// 15 sectors of 4 voices each.
	for i := range fullSectors {
		fillVoiceSector(banks+i, disk.VoicesPerSector)
	}
	// One sector of 3 voices (so isVoiceSector still accepts it because
	// slot 0 has a printable name + valid sample rate).
	fillVoiceSector(banks+fullSectors, partialSlots)
	// One more sector of 4 voices; pre-fix this would push nvoice from
	// 63 to 67.
	fillVoiceSector(banks+fullSectors+1, disk.VoicesPerSector)

	fi, err := detectFile(data)
	if err != nil {
		t.Fatalf("detectFile: %v", err)
	}
	if fi.fileType != disk.TypeFullDump {
		t.Errorf("fileType = %d, want %d (full dump)", fi.fileType, disk.TypeFullDump)
	}
	if fi.nvoice > disk.MaxVoices {
		t.Errorf("nvoice = %d, want <= %d (MaxVoices); guard let count overshoot", fi.nvoice, disk.MaxVoices)
	}
}

func TestReplaceOnImage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	fzv1 := make([]byte, disk.SectorSize*3)
	copy(fzv1[disk.VoiceNameOffset:], "OLDVOICE    ")
	fzv1Path := filepath.Join(dir, "old.fzv")
	if err := os.WriteFile(fzv1Path, fzv1, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzv1Path, 0); err != nil {
		t.Fatal(err)
	}

	fzv2 := make([]byte, disk.SectorSize*3)
	copy(fzv2[disk.VoiceNameOffset:], "NEWVOICE    ")

	if err := ReplaceOnImage(imgPath, "OLDVOICE", fzv2, 0); err != nil {
		t.Fatal(err)
	}

	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].NameString() != "NEWVOICE" {
		t.Errorf("expected NEWVOICE, got %q", entries[0].NameString())
	}
}

func TestReplaceOnImagePreservesOtherFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	fzv1 := make([]byte, disk.SectorSize*3)
	copy(fzv1[disk.VoiceNameOffset:], "KEEP        ")
	fzv1Path := filepath.Join(dir, "keep.fzv")
	if err := os.WriteFile(fzv1Path, fzv1, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzv1Path, 0); err != nil {
		t.Fatal(err)
	}

	fzv2 := make([]byte, disk.SectorSize*3)
	copy(fzv2[disk.VoiceNameOffset:], "REPLACE     ")
	fzv2Path := filepath.Join(dir, "replace.fzv")
	if err := os.WriteFile(fzv2Path, fzv2, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzv2Path, 0); err != nil {
		t.Fatal(err)
	}

	keepBefore := extractFileData(t, imgPath, "KEEP")

	fzv3 := make([]byte, disk.SectorSize*3)
	copy(fzv3[disk.VoiceNameOffset:], "REPLACED    ")
	if err := ReplaceOnImage(imgPath, "REPLACE", fzv3, 0); err != nil {
		t.Fatal(err)
	}

	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.NameString())
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(entries), names)
	}

	keepAfter := extractFileData(t, imgPath, "KEEP")
	if !bytes.Equal(keepBefore, keepAfter) {
		t.Error("KEEP file data changed after replacing another file")
	}

	foundReplaced := false
	for _, e := range entries {
		if e.NameString() == "REPLACED" {
			foundReplaced = true
		}
	}
	if !foundReplaced {
		t.Errorf("REPLACED not found in directory: %v", names)
	}
}

func extractFileData(t *testing.T, imgPath, name string) []byte {
	t.Helper()
	dir := t.TempDir()
	outPath := filepath.Join(dir, name+".fzv")

	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	var match *disk.DirEntry
	for i := range entries {
		if strings.EqualFold(entries[i].NameString(), name) {
			match = &entries[i]
			break
		}
	}
	if match == nil {
		t.Fatalf("file %q not found", name)
	}

	disSec, err := img.Sector(int(match.DisSector))
	if err != nil {
		t.Fatal(err)
	}
	dis, err := disk.DecodeDisSector(disSec)
	if err != nil {
		t.Fatal(err)
	}

	var raw []byte
	for i, ext := range dis.Extents {
		start := int(ext[0])
		end := int(ext[1])
		if i == 0 && start == int(match.DisSector) {
			start++
		}
		for sec := start; sec <= end; sec++ {
			b, err := img.Sector(sec)
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, b...)
		}
	}

	if err := os.WriteFile(outPath, raw, 0644); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAddRejectsNonFZFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	textPath := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(textPath, []byte("This is a plain text file, not an FZ file."), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, textPath, 0); err == nil {
		t.Error("expected error when adding a text file")
	}

	binPath := filepath.Join(dir, "random.bin")
	randomData := make([]byte, 4096)
	for i := range randomData {
		randomData[i] = byte((i*7 + 13) % 256)
	}
	if err := os.WriteFile(binPath, randomData, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, binPath, 0); err == nil {
		t.Error("expected error when adding a random binary file")
	}
}

func TestAddDuplicateNameRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	fzv := make([]byte, disk.SectorSize*3)
	copy(fzv[disk.VoiceNameOffset:], "KICK        ")
	fzvPath := filepath.Join(dir, "kick.fzv")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzvPath, 0); err != nil {
		t.Fatalf("first add: %v", err)
	}

	fzv2 := make([]byte, disk.SectorSize*3)
	copy(fzv2[disk.VoiceNameOffset:], "KICK        ")
	fzv2Path := filepath.Join(dir, "kick2.fzv")
	if err := os.WriteFile(fzv2Path, fzv2, 0644); err != nil {
		t.Fatal(err)
	}
	err := Add(imgPath, fzv2Path, 0)
	if err == nil {
		t.Fatal("expected error when adding duplicate name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %q", err.Error())
	}
}

// TestReplaceInMemoryRollsBackOnFailure verifies ReplaceInMemory is
// transactional on the *disk.Image value itself: when addToImage fails
// (here, the replacement is too large to fit), the image bytes must be
// byte-identical to their pre-call state. Callers like studio.editsave
// rely on this so they can safely abort a save without writing a
// half-modified image.
func TestReplaceInMemoryRollsBackOnFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	fzv := make([]byte, disk.SectorSize*2)
	copy(fzv[disk.VoiceNameOffset:], "SMALL       ")
	fzvPath := filepath.Join(dir, "small.fzv")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzvPath, 0); err != nil {
		t.Fatal(err)
	}

	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), img.Bytes()...)

	oversized := make([]byte, disk.ImageSize+disk.SectorSize)
	copy(oversized[disk.VoiceNameOffset:], "TOOBIG      ")

	if err := ReplaceInMemory(img, "SMALL", oversized, 0); err == nil {
		t.Fatal("expected error when replacement file exceeds disk capacity")
	}

	if !bytes.Equal(before, img.Bytes()) {
		t.Error("image bytes mutated after failed ReplaceInMemory (rollback did not restore state)")
	}

	// Sanity: the original file must still be loadable from the image.
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].NameString() != "SMALL" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.NameString()
		}
		t.Errorf("expected directory to still contain SMALL, got %v", names)
	}
}

// TestReplaceInMemoryRollsBackOnMissingFile verifies the early failure path
// (file not found) leaves the image untouched. This is the path studio.editsave
// could hit if the in-progress file is renamed/deleted between selection and
// save, and a partial mutation would corrupt the on-disk image when the caller
// proceeds to write a sibling image.
func TestReplaceInMemoryRollsBackOnMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	fzv := make([]byte, disk.SectorSize*2)
	copy(fzv[disk.VoiceNameOffset:], "PRESENT     ")
	fzvPath := filepath.Join(dir, "present.fzv")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzvPath, 0); err != nil {
		t.Fatal(err)
	}

	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), img.Bytes()...)

	replacement := make([]byte, disk.SectorSize*2)
	copy(replacement[disk.VoiceNameOffset:], "NEW         ")

	if err := ReplaceInMemory(img, "ABSENT", replacement, 0); err == nil {
		t.Fatal("expected error when replacing a name that doesn't exist")
	}

	if !bytes.Equal(before, img.Bytes()) {
		t.Error("image bytes mutated after failed ReplaceInMemory of missing file")
	}
}

func TestReplaceOnImageOverCapacity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	fzv1 := make([]byte, disk.SectorSize*2)
	copy(fzv1[disk.VoiceNameOffset:], "SMALL       ")
	fzv1Path := filepath.Join(dir, "small.fzv")
	if err := os.WriteFile(fzv1Path, fzv1, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzv1Path, 0); err != nil {
		t.Fatal(err)
	}

	oversized := make([]byte, disk.ImageSize+disk.SectorSize)
	copy(oversized[disk.VoiceNameOffset:], "HUGE        ")

	err := ReplaceOnImage(imgPath, "SMALL", oversized, 0)
	if err == nil {
		t.Fatal("expected error when replacement file exceeds disk capacity")
	}
}

func TestAddNearFullDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "full.img")
	if err := diskformat.Format(imgPath, "NEARFULL"); err != nil {
		t.Fatal(err)
	}

	for i := range disk.MaxDirEntries - 1 {
		fzvPath := filepath.Join(dir, fmt.Sprintf("voice%03d.fzv", i))
		fzv := make([]byte, disk.SectorSize+256)
		name := fmt.Sprintf("V%03d", i)
		padded := disk.PadLabel(name)
		copy(fzv[disk.VoiceNameOffset:], padded[:])
		if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
			t.Fatal(err)
		}
		if err := Add(imgPath, fzvPath, 0); err != nil {
			t.Fatalf("adding voice %d: %v", i, err)
		}
	}

	fzvPath := filepath.Join(dir, "final.fzv")
	fzv := make([]byte, disk.SectorSize+256)
	padded := disk.PadLabel("FINAL")
	copy(fzv[disk.VoiceNameOffset:], padded[:])
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Add(imgPath, fzvPath, 0); err != nil {
		t.Fatalf("adding 64th voice (should succeed): %v", err)
	}
}

// TestAddDirectoryFullLeavesImageClean verifies that when the directory is
// completely full, addToImage rejects the operation BEFORE allocating any
// sectors or mutating the CAT, leaving the in-memory image untouched.
// Previously the CAT and data sectors were written first, only to discover
// NextFreeDirSlot returned ErrDirFull, leaving the in-memory image dirty
// (the on-disk file was saved by WriteAtomic, but a caller holding the
// *Image now has a broken view of the disk).
func TestAddDirectoryFullLeavesImageClean(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "full.img")
	if err := diskformat.Format(imgPath, "DIRFULL"); err != nil {
		t.Fatal(err)
	}

	// Fill the directory completely (MaxDirEntries entries).
	for i := range disk.MaxDirEntries {
		fzvPath := filepath.Join(dir, fmt.Sprintf("voice%03d.fzv", i))
		fzv := make([]byte, disk.SectorSize+256)
		name := fmt.Sprintf("V%03d", i)
		padded := disk.PadLabel(name)
		copy(fzv[disk.VoiceNameOffset:], padded[:])
		if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
			t.Fatal(err)
		}
		if err := Add(imgPath, fzvPath, 0); err != nil {
			t.Fatalf("adding voice %d (directory should have space): %v", i, err)
		}
	}

	// Snapshot the image state after the directory is full.
	before, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}

	// One more add should fail with ErrDirFull, and the image bytes must
	// remain identical (no partial CAT/sector mutation).
	fzvPath := filepath.Join(dir, "overflow.fzv")
	fzv := make([]byte, disk.SectorSize+256)
	copy(fzv[disk.VoiceNameOffset:], "OVERFLOW    ")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	addErr := Add(imgPath, fzvPath, 0)
	if addErr == nil {
		t.Fatal("expected ErrDirFull when adding to a full directory")
	}
	if !errors.Is(addErr, disk.ErrDirFull) {
		t.Errorf("expected ErrDirFull, got %v", addErr)
	}

	after, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("image bytes changed after a failed-because-dir-full Add (partial mutation)")
	}
}

func TestConcurrentAddSafe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("V%03d", idx)
			fzv := make([]byte, disk.SectorSize*2)
			padded := disk.PadLabel(name)
			copy(fzv[disk.VoiceNameOffset:], padded[:])
			fzvPath := filepath.Join(dir, fmt.Sprintf("voice%d.fzv", idx))
			if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
				errs[idx] = err
				return
			}
			errs[idx] = Add(imgPath, fzvPath, 0)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != n {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.NameString()
		}
		t.Errorf("expected %d entries, got %d: %v", n, len(entries), names)
	}
}
