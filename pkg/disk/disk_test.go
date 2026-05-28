package disk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDiskNum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      int
		want    uint8
		wantErr bool
	}{
		{1, 0, false},
		{2, 1, false},
		{0, 0, true},
		{3, 0, true},
		{-1, 0, true},
	}
	for _, tt := range tests {
		got, err := ValidateDiskNum(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ValidateDiskNum(%d): expected error, got nil", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ValidateDiskNum(%d): unexpected error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ValidateDiskNum(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestPadLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  [LabelSize]byte
	}{
		{"HOOVER", [LabelSize]byte{'H', 'O', 'O', 'V', 'E', 'R', ' ', ' ', ' ', ' ', ' ', ' '}},
		{"EXACTLY12CHR", [LabelSize]byte{'E', 'X', 'A', 'C', 'T', 'L', 'Y', '1', '2', 'C', 'H', 'R'}},
		{"TOOLONGSTRING", [LabelSize]byte{'T', 'O', 'O', 'L', 'O', 'N', 'G', 'S', 'T', 'R', 'I', 'N'}},
	}
	for _, tt := range tests {
		got := PadLabel(tt.input)
		if got != tt.want {
			t.Errorf("PadLabel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestTypeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code FileType
		want string
	}{
		{TypeFullDump, TypeFullDumpLabel},
		{TypeVoice, "Voice"},
		{TypeBank, "Bank"},
		{TypeEffect, "Effect"},
		{TypeSequence, "Sequence"},
		{TypeProgram, "Program"},
		{FileType(99), "Unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.code.String(); got != tt.want {
			t.Errorf("FileType(%d).String() = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestEncodeDirEntryRoundTrip(t *testing.T) {
	t.Parallel()
	e := DirEntry{
		Name:      PadLabel("HOOVER"),
		FileType:  TypeVoice,
		DiskNum:   0,
		DisSector: 2,
	}
	b := EncodeDirEntry(e)
	got, err := DecodeDirEntry(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != e {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, e)
	}
}

func TestEncodeDisSectorRoundTrip(t *testing.T) {
	t.Parallel()
	d := DisSector{
		Extents:    [][2]uint16{{2, 142}},
		BankCount:  0,
		VoiceCount: 1,
		WaveCount:  139,
	}
	b := EncodeDisSector(d)
	got, err := DecodeDisSector(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Extents) != 1 || got.Extents[0] != d.Extents[0] {
		t.Errorf("extents mismatch: got %v, want %v", got.Extents, d.Extents)
	}
	if got.BankCount != d.BankCount || got.VoiceCount != d.VoiceCount || got.WaveCount != d.WaveCount {
		t.Errorf("counts mismatch: got %+v, want %+v", got, d)
	}
}

// TestDBPAreaBoundedAtSpecLimit guards a regression where the DIS extent
// scan extended into the file head's work area (bytes 256..1017 per spec
// §1-4). The dBP area is 256 bytes (64 entries); encoded extents must not
// spill into the work area, and decoded extents must not scan past byte
// 256.
func TestDBPAreaBoundedAtSpecLimit(t *testing.T) {
	t.Parallel()

	// Encode: 64 contiguous (small) extents fit cleanly into the 256-byte
	// dBP area; a 65th must be dropped silently rather than written into
	// the work area where it would corrupt unrelated bytes.
	exts := make([][2]uint16, MaxDBPEntries+1)
	for i := range exts {
		exts[i] = [2]uint16{uint16(i*2 + 2), uint16(i*2 + 3)} //nolint:gosec // bounded by MaxDBPEntries+1 = 65
	}
	d := DisSector{Extents: exts}
	b := EncodeDisSector(d)

	// Byte 256 onwards (work area) must be zero: no overflowing extent
	// data, no leakage.
	for i := DBPAreaSize; i < DisTailOffset; i++ {
		if b[i] != 0 {
			t.Errorf("byte 0x%03x in work area = 0x%02x, expected 0 (extent leaked beyond dBP area)", i, b[i])
			break
		}
	}

	// Decode: only the first MaxDBPEntries (64) extents should be
	// recovered; the dropped 65th must not appear.
	got, err := DecodeDisSector(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Extents) > MaxDBPEntries {
		t.Errorf("decoded %d extents, want at most %d (work area must not be scanned)", len(got.Extents), MaxDBPEntries)
	}

	// Sanity check: with a single extent followed by all-zero dBP slots,
	// decode must stop at the first (0,0) terminator and not continue
	// reading work-area garbage.
	single := EncodeDisSector(DisSector{Extents: [][2]uint16{{5, 9}}})
	for i := 8; i < SectorSize; i++ {
		single[i] = 0xCC // poison everything past the first extent
	}
	// Restore (0,0) at slot 1 so the terminator is present.
	single[4], single[5], single[6], single[7] = 0, 0, 0, 0
	gotSingle, err := DecodeDisSector(single)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotSingle.Extents) != 1 {
		t.Errorf("expected 1 extent, got %d (terminator should stop the scan)", len(gotSingle.Extents))
	}
}

func TestDisSectorFileSize(t *testing.T) {
	t.Parallel()
	d := DisSector{Extents: [][2]uint16{{2, 142}}}
	// 142 - 2 + 1 = 141 sectors
	want := 141 * SectorSize
	if got := d.FileSize(); got != want {
		t.Errorf("FileSize() = %d, want %d", got, want)
	}
	// PayloadSize strips the DIS sector so disk ls and disk get agree.
	if got := d.PayloadSize(); got != want-SectorSize {
		t.Errorf("PayloadSize() = %d, want %d", got, want-SectorSize)
	}
}

func TestDisSectorPayloadSizeEmpty(t *testing.T) {
	t.Parallel()
	d := DisSector{}
	if got := d.PayloadSize(); got != 0 {
		t.Errorf("PayloadSize() on empty extents = %d, want 0", got)
	}
}

func TestOpenImageValid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := os.WriteFile(imgPath, make([]byte, ImageSize), 0644); err != nil {
		t.Fatal(err)
	}
	img, err := OpenImage(imgPath)
	if err != nil {
		t.Fatalf("OpenImage: %v", err)
	}
	if len(img.Bytes()) != ImageSize {
		t.Errorf("image size: got %d, want %d", len(img.Bytes()), ImageSize)
	}
}

func TestOpenImageMissing(t *testing.T) {
	t.Parallel()
	_, err := OpenImage(filepath.Join(t.TempDir(), "nope.img"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestOpenImageWrongSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "small.img")
	if err := os.WriteFile(imgPath, make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := OpenImage(imgPath)
	if err == nil {
		t.Error("expected error for undersized file")
	}
}

func TestReadImageSizeValidation(t *testing.T) {
	t.Parallel()
	_, err := ReadImage(bytes.NewReader(make([]byte, 100)))
	if err == nil {
		t.Error("expected error for undersized image")
	}
}

func TestReadImageOversized(t *testing.T) {
	t.Parallel()
	_, err := ReadImage(bytes.NewReader(make([]byte, ImageSize+100)))
	if err == nil {
		t.Fatal("expected error for oversized image")
	}
	if !strings.Contains(err.Error(), "file is larger") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestImageSectorRoundTrip(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	sector := make([]byte, SectorSize)
	sector[0] = 0xde
	sector[SectorSize-1] = 0xad
	if err := img.SetSector(5, sector); err != nil {
		t.Fatal(err)
	}
	got, err := img.Sector(5)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sector) {
		t.Error("sector round-trip mismatch")
	}
}

func TestCATBitmap(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if img.CATAllocated(5) {
		t.Error("sector 5 should be free initially")
	}
	if err := img.CATSetAllocated(5); err != nil {
		t.Fatal(err)
	}
	if !img.CATAllocated(5) {
		t.Error("sector 5 should be allocated after CATSetAllocated")
	}
	if img.CATAllocated(6) {
		t.Error("sector 6 should still be free")
	}
}

func TestAllocateSectors(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	sectors, err := img.AllocateSectors(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(sectors) != 3 {
		t.Fatalf("expected 3 sectors, got %d", len(sectors))
	}
	if sectors[0] != 2 || sectors[1] != 3 || sectors[2] != 4 {
		t.Errorf("expected sectors [2,3,4], got %v", sectors)
	}
	for _, s := range sectors {
		if !img.CATAllocated(s) {
			t.Errorf("sector %d should be marked allocated", s)
		}
	}
}

func TestFreeSectors(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	// Fresh unformatted image: all sectors free.
	initial := img.FreeSectors()
	if initial != SectorCount-2 {
		t.Errorf("initial free sectors: got %d, want %d", initial, SectorCount-2)
	}

	// Allocate 5 sectors; free count should drop by 5.
	if _, err := img.AllocateSectors(5); err != nil {
		t.Fatal(err)
	}
	after := img.FreeSectors()
	if after != initial-5 {
		t.Errorf("after allocating 5: got %d free, want %d", after, initial-5)
	}
}

func TestAllocateSectorsDiskFull(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// Allocate all available sectors.
	free := img.FreeSectors()
	_, err = img.AllocateSectors(free)
	if err != nil {
		t.Fatalf("allocating all %d sectors: %v", free, err)
	}
	// One more should fail.
	_, err = img.AllocateSectors(1)
	if err == nil {
		t.Error("expected error when disk is full")
	}
}

func TestDirectoryFull(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// Fill all 64 directory slots.
	for i := range MaxDirEntries {
		slot, err := img.NextFreeDirSlot()
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		name := PadLabel(fmt.Sprintf("VOICE%03d", i))
		entry := DirEntry{Name: name, FileType: TypeVoice, DisSector: uint16(i + 2)}
		copy(img.Bytes()[slot:], EncodeDirEntry(entry))
	}
	// 65th should fail.
	_, err = img.NextFreeDirSlot()
	if err == nil {
		t.Error("expected error when directory is full")
	}
}

func TestDirectory(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, ImageSize)
		img, err := ReadImage(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		entries, err := img.Directory()
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 entries, got %d", len(entries))
		}
	})

	t.Run("one entry", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, ImageSize)
		img, err := ReadImage(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		want := DirEntry{
			Name:      PadLabel("HOOVER"),
			FileType:  TypeVoice,
			DiskNum:   0,
			DisSector: 5,
		}
		copy(img.Bytes()[DirSector*SectorSize:], EncodeDirEntry(want))
		entries, err := img.Directory()
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		if entries[0] != want {
			t.Errorf("got %+v, want %+v", entries[0], want)
		}
	})

	t.Run("terminates at zero", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, ImageSize)
		img, err := ReadImage(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		e1 := DirEntry{Name: PadLabel("FIRST"), FileType: TypeVoice, DisSector: 2}
		e2 := DirEntry{Name: PadLabel("SECOND"), FileType: TypeBank, DisSector: 3}
		base := DirSector * SectorSize
		copy(img.Bytes()[base:], EncodeDirEntry(e1))
		copy(img.Bytes()[base+DirEntrySize:], EncodeDirEntry(e2))
		// Third slot is zero (default), so iteration should stop here.
		entries, err := img.Directory()
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
	})
}

func TestVoiceSlotOffset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		bankStart  int
		voiceIndex int
		want       int
	}{
		{1024, 0, 1024},
		{1024, 3, 1024 + 3*256},
		{1024, 4, 1024 + 1*1024 + 0*256},
		{1024, 63, 1024 + 15*1024 + 3*256},
	}
	for _, tt := range tests {
		got := VoiceSlotOffset(tt.bankStart, tt.voiceIndex)
		if got != tt.want {
			t.Errorf("VoiceSlotOffset(%d, %d) = %d, want %d",
				tt.bankStart, tt.voiceIndex, got, tt.want)
		}
	}
}

func TestFormatAudioOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		gchn uint8
		want string
	}{
		{0xff, "all"},
		{0x00, "none"},
		{0x01, "1"},
		{0x02, "2"},
		{0x04, "3"},
		{0x08, "4"},
		{0x10, "5"},
		{0x20, "6"},
		{0x40, "7"},
		{0x80, "8"},
		{0x05, "1,3"},
		{0x0f, "1,2,3,4"},
		{0x55, "1,3,5,7"},
		{0xaa, "2,4,6,8"},
	}
	for _, tt := range tests {
		got := FormatAudioOut(tt.gchn)
		if got != tt.want {
			t.Errorf("FormatAudioOut(0x%02x) = %q, want %q", tt.gchn, got, tt.want)
		}
	}
}

func TestSetSectorWrongSize(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	err = img.SetSector(0, make([]byte, 512))
	if err == nil {
		t.Error("expected error for wrong-size sector data")
	}
}

func TestCATAllocatedBounds(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if img.CATAllocated(-1) {
		t.Error("sector -1 should return false")
	}
	if img.CATAllocated(0) {
		t.Error("sector 0 should be free initially")
	}
	if img.CATAllocated(SectorCount - 1) {
		t.Error("sector SectorCount-1 should be free initially")
	}
	if img.CATAllocated(SectorCount) {
		t.Error("sector SectorCount should return false")
	}
}

func TestDecodeDisSectorMultipleExtents(t *testing.T) {
	t.Parallel()
	want := DisSector{
		Extents:    [][2]uint16{{2, 50}, {100, 200}, {500, 600}},
		BankCount:  1,
		VoiceCount: 3,
		WaveCount:  150,
	}
	b := EncodeDisSector(want)
	got, err := DecodeDisSector(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Extents) != len(want.Extents) {
		t.Fatalf("extent count: got %d, want %d", len(got.Extents), len(want.Extents))
	}
	for i, ext := range got.Extents {
		if ext != want.Extents[i] {
			t.Errorf("extent %d: got %v, want %v", i, ext, want.Extents[i])
		}
	}
	if got.BankCount != want.BankCount || got.VoiceCount != want.VoiceCount || got.WaveCount != want.WaveCount {
		t.Errorf("counts: got {%d,%d,%d}, want {%d,%d,%d}",
			got.BankCount, got.VoiceCount, got.WaveCount,
			want.BankCount, want.VoiceCount, want.WaveCount)
	}
}

func TestDecodeDisSectorTooSmall(t *testing.T) {
	t.Parallel()
	_, err := DecodeDisSector(make([]byte, SectorSize-1))
	if err == nil {
		t.Error("expected error for buffer smaller than SectorSize")
	}
}

// TestDecodeDisSectorRejectsCorruptExtents guards against a corrupt DIS
// silently routing reads at sector 0 or 1 (reserved for the disk label and
// directory) or past the end of the disk. Such a DIS cannot be used safely:
// following its extents would mis-interpret reserved bytes as file data.
func TestDecodeDisSectorRejectsCorruptExtents(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		start uint16
		end   uint16
	}{
		{"start in reserved label sector", 0, 5},
		{"start in reserved directory sector", 1, 5},
		{"end in reserved sector", 2, 1},
		{"start past end of disk", SectorCount, SectorCount + 1},
		{"end past end of disk", 2, SectorCount},
		{"end before start", 10, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := make([]byte, SectorSize)
			binary.LittleEndian.PutUint16(b[0:2], c.start)
			binary.LittleEndian.PutUint16(b[2:4], c.end)
			_, err := DecodeDisSector(b)
			if err == nil {
				t.Fatalf("expected error for extent [%d,%d], got nil", c.start, c.end)
			}
			if !errors.Is(err, ErrCorruptDIS) {
				t.Errorf("expected ErrCorruptDIS, got %v", err)
			}
		})
	}
}

func TestDecodeDirEntryTooSmall(t *testing.T) {
	t.Parallel()
	_, err := DecodeDirEntry(make([]byte, 4))
	if err == nil {
		t.Error("expected error for 4-byte buffer")
	}
}

func TestPadLabelEmpty(t *testing.T) {
	t.Parallel()
	got := PadLabel("")
	want := [LabelSize]byte{' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' '}
	if got != want {
		t.Fatalf("PadLabel empty: got %v, want %v", got, want)
	}
}

func TestDisSectorFileSizeZeroExtents(t *testing.T) {
	t.Parallel()
	d := DisSector{}
	if got := d.FileSize(); got != 0 {
		t.Fatalf("FileSize zero extents: got %d, want 0", got)
	}
}

func TestImageLabel(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	padded := PadLabel("TEST LABEL")
	copy(img.Bytes()[0:LabelSize], padded[:])
	got := img.Label()
	if got != "TEST LABEL" {
		t.Fatalf("Label: got %q, want %q", got, "TEST LABEL")
	}
}

func TestIsPrintableName(t *testing.T) {
	t.Parallel()
	if !IsPrintableName([]byte("HOOVER      ")) {
		t.Error("HOOVER should be printable")
	}
	if IsPrintableName([]byte{0x00, 0x01}) {
		t.Error("control bytes should not be printable")
	}
}

// TestLoopPointerMasks verifies that the loopst/looped flag-bit helpers
// match the spec section 2-1 byte layout: upper 8 bits of loopst hold a
// 0-255 loop-fine value, the MSB of looped holds the loop-pattern (skip)
// flag, and the remaining bits carry the sample address.
func TestLoopPointerMasks(t *testing.T) {
	t.Parallel()
	// loopst: loop-fine = 0x37, address = 0x123456 -> 0x37123456.
	if got := LoopStartAddress(0x37123456); got != 0x123456 {
		t.Errorf("LoopStartAddress(0x37123456) = 0x%x, want 0x123456", got)
	}
	if got := LoopFineBits(0x37123456); got != 0x37 {
		t.Errorf("LoopFineBits(0x37123456) = 0x%x, want 0x37", got)
	}
	// loopst with no loop-fine.
	if got := LoopStartAddress(0x00123456); got != 0x123456 {
		t.Errorf("LoopStartAddress(0x00123456) = 0x%x, want 0x123456", got)
	}
	if got := LoopFineBits(0x00123456); got != 0 {
		t.Errorf("LoopFineBits(0x00123456) = 0x%x, want 0", got)
	}

	// looped: skip flag = 1, address = 0x12345 -> 0x80012345.
	if got := LoopEndAddress(0x80012345); got != 0x12345 {
		t.Errorf("LoopEndAddress(0x80012345) = 0x%x, want 0x12345", got)
	}
	if !LoopSkipFlag(0x80012345) {
		t.Errorf("LoopSkipFlag(0x80012345) = false, want true")
	}
	// looped with no skip flag.
	if got := LoopEndAddress(0x00012345); got != 0x12345 {
		t.Errorf("LoopEndAddress(0x00012345) = 0x%x, want 0x12345", got)
	}
	if LoopSkipFlag(0x00012345) {
		t.Errorf("LoopSkipFlag(0x00012345) = true, want false")
	}
}

func TestIsPlausibleVoiceHeader(t *testing.T) {
	t.Parallel()
	makeHeader := func(rate byte, name string) []byte {
		data := make([]byte, SectorSize)
		data[VoiceSampOffset] = rate
		copy(data[VoiceNameOffset:], name)
		return data
	}

	t.Run("valid rate 0", func(t *testing.T) {
		t.Parallel()
		if !IsPlausibleVoiceHeader(makeHeader(0, "KICK        ")) {
			t.Error("rate index 0 with printable name should be plausible")
		}
	})
	t.Run("valid rate 1", func(t *testing.T) {
		t.Parallel()
		if !IsPlausibleVoiceHeader(makeHeader(1, "SNARE       ")) {
			t.Error("rate index 1 with printable name should be plausible")
		}
	})
	t.Run("valid rate 2", func(t *testing.T) {
		t.Parallel()
		if !IsPlausibleVoiceHeader(makeHeader(2, "HIHAT       ")) {
			t.Error("rate index 2 with printable name should be plausible")
		}
	})
	t.Run("invalid rate index", func(t *testing.T) {
		t.Parallel()
		if IsPlausibleVoiceHeader(makeHeader(3, "KICK        ")) {
			t.Error("rate index 3 should not be plausible")
		}
	})
	t.Run("text-like data", func(t *testing.T) {
		t.Parallel()
		if IsPlausibleVoiceHeader(makeHeader('e', "from the lat")) {
			t.Error("text file content should not be plausible")
		}
	})
	t.Run("unprintable name", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, SectorSize)
		data[VoiceSampOffset] = 0
		data[VoiceNameOffset] = 0x00
		if IsPlausibleVoiceHeader(data) {
			t.Error("unprintable name should not be plausible")
		}
	})
	t.Run("too small", func(t *testing.T) {
		t.Parallel()
		if IsPlausibleVoiceHeader(make([]byte, 100)) {
			t.Error("data smaller than SectorSize should not be plausible")
		}
	})
}

func TestIsPlausibleProgramHeader(t *testing.T) {
	t.Parallel()
	// Standard FZ-1 program preamble shared by CKMEMORY, CKMIDI, CKPORT et al
	// on the factory OPT_SOFTWARE diagnostic disk. 14 bytes covering the
	// CALL near to main, RETF back to firmware, and the ROM-call trampoline.
	standard := []byte{
		0xE8, 0x0B, 0x00, 0xCB, 0x8F, 0x06, 0xF6, 0x55,
		0xCC, 0xFF, 0x36, 0xF6, 0x55, 0xC3,
	}
	// ONBOARDKEY's variant: an STI is inserted between the CALL and RETF,
	// pushing the RETF to offset 4 instead of 3.
	withSTI := []byte{
		0xE8, 0xEE, 0x01, 0xFB, 0xCB, 0x8F, 0x06, 0x0F,
		0x60, 0xCC, 0xFF, 0x36, 0x0F, 0x60,
	}

	t.Run("standard preamble", func(t *testing.T) {
		t.Parallel()
		if !IsPlausibleProgramHeader(standard) {
			t.Error("standard CKMIDI-style preamble should be plausible")
		}
	})
	t.Run("variant with STI", func(t *testing.T) {
		t.Parallel()
		if !IsPlausibleProgramHeader(withSTI) {
			t.Error("ONBOARDKEY-style preamble (STI before RETF) should be plausible")
		}
	})
	t.Run("too short", func(t *testing.T) {
		t.Parallel()
		if IsPlausibleProgramHeader(standard[:13]) {
			t.Error("13 bytes should be too short")
		}
	})
	t.Run("all zeros", func(t *testing.T) {
		t.Parallel()
		if IsPlausibleProgramHeader(make([]byte, 64)) {
			t.Error("zero buffer should not be plausible")
		}
	})
	t.Run("starts with long jump", func(t *testing.T) {
		t.Parallel()
		bad := append([]byte{}, standard...)
		bad[0] = 0xE9 // JMP near, not CALL
		if IsPlausibleProgramHeader(bad) {
			t.Error("E9 (long jump) should not be plausible")
		}
	})
	t.Run("no RETF at offset 3 or 4", func(t *testing.T) {
		t.Parallel()
		bad := append([]byte{}, standard...)
		bad[3] = 0x90 // NOP instead of RETF
		bad[4] = 0x90
		if IsPlausibleProgramHeader(bad) {
			t.Error("E8 without CB at offset 3 or 4 should not be plausible")
		}
	})
	t.Run("voice header bytes", func(t *testing.T) {
		t.Parallel()
		// Construct a plausible voice header. Its first byte is the low
		// byte of wavst (typically 0x00), which is not 0xE8.
		voice := make([]byte, SectorSize)
		voice[VoiceSampOffset] = 0
		copy(voice[VoiceNameOffset:], "KICK        ")
		if IsPlausibleProgramHeader(voice) {
			t.Error("voice header should not look like a program")
		}
	})
}

func TestCATSetAllocatedBounds(t *testing.T) {
	t.Parallel()
	img := &Image{}
	tests := []struct {
		name   string
		sector int
	}{
		{"negative", -1},
		{"at count", SectorCount},
		{"above count", SectorCount + 100},
	}
	for _, tt := range tests {
		if err := img.CATSetAllocated(tt.sector); err == nil {
			t.Errorf("CATSetAllocated(%d): expected error for %s", tt.sector, tt.name)
		}
	}
}

func TestSectorOutOfBounds(t *testing.T) {
	t.Parallel()
	img := &Image{}
	tests := []struct {
		name   string
		sector int
	}{
		{"negative", -1},
		{"at count", SectorCount},
	}
	for _, tt := range tests {
		_, err := img.Sector(tt.sector)
		if err == nil {
			t.Errorf("Sector(%d): expected error for %s", tt.sector, tt.name)
		}
	}
}

func TestAllocateSectorsZero(t *testing.T) {
	t.Parallel()
	img := &Image{}
	sectors, err := img.AllocateSectors(0)
	if err != nil {
		t.Fatalf("AllocateSectors(0): unexpected error: %v", err)
	}
	if len(sectors) != 0 {
		t.Errorf("expected empty slice, got %d sectors", len(sectors))
	}
}

func TestSectorsNeeded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		byteLen int
		want    int
	}{
		{0, 0},
		{1, 1},
		{SectorSize, 1},
		{SectorSize + 1, 2},
		{SectorSize * 3, 3},
		{SectorSize*3 + 500, 4},
	}
	for _, tt := range tests {
		if got := SectorsNeeded(tt.byteLen); got != tt.want {
			t.Errorf("SectorsNeeded(%d) = %d, want %d", tt.byteLen, got, tt.want)
		}
	}
}

func TestPadToSector(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int
		want int
	}{
		{0, 0},
		{1, SectorSize},
		{SectorSize, SectorSize},
		{SectorSize + 1, SectorSize * 2},
		{SectorSize * 3, SectorSize * 3},
		{SectorSize*3 + 1, SectorSize * 4},
	}
	for _, tt := range tests {
		if got := PadToSector(tt.n); got != tt.want {
			t.Errorf("PadToSector(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

func TestFileTypeString(t *testing.T) {
	t.Parallel()
	if got := TypeFullDump.String(); got != TypeFullDumpLabel {
		t.Errorf("TypeFullDump.String() = %q, want %q", got, TypeFullDumpLabel)
	}
	if got := FileType(99).String(); got != "Unknown(99)" {
		t.Errorf("FileType(99).String() = %q, want %q", got, "Unknown(99)")
	}
}

func TestForEachSamplePointer(t *testing.T) {
	t.Parallel()
	voice := make([]byte, LoopPointerRangeEnd)
	var count int
	var waveCount, loopStartCount, loopEndCount int
	ForEachSamplePointer(voice, func(_ []byte, kind SamplePointerKind) {
		count++
		switch kind {
		case WavePointer:
			waveCount++
		case LoopStartPointer:
			loopStartCount++
		case LoopEndPointer:
			loopEndCount++
		}
	})
	waveFields := (WavePointerRangeEnd - WavePointerRangeStart) / 4
	loopStartFields := (VoiceLoopEd0Offset - LoopPointerRangeStart) / 4
	loopEndFields := (LoopPointerRangeEnd - VoiceLoopEd0Offset) / 4
	want := waveFields + loopStartFields + loopEndFields
	if count != want {
		t.Errorf("ForEachSamplePointer called fn %d times, want %d", count, want)
	}
	if waveCount != waveFields {
		t.Errorf("wave-pointer kind count: got %d, want %d", waveCount, waveFields)
	}
	if loopStartCount != loopStartFields {
		t.Errorf("loop-start kind count: got %d, want %d", loopStartCount, loopStartFields)
	}
	if loopEndCount != loopEndFields {
		t.Errorf("loop-end kind count: got %d, want %d", loopEndCount, loopEndFields)
	}
}

func TestForEachSamplePointerShortVoice(t *testing.T) {
	t.Parallel()
	voice := make([]byte, 10)
	var count int
	ForEachSamplePointer(voice, func(_ []byte, _ SamplePointerKind) {
		count++
	})
	if count != 0 {
		t.Errorf("ForEachSamplePointer should not call fn on short voice, called %d times", count)
	}
}

func TestTrimPadded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input []byte
		want  string
	}{
		{[]byte("            "), ""},
		{[]byte("HELLO"), "HELLO"},
		{[]byte("HELLO       "), "HELLO"},
		{[]byte{}, ""},
		{[]byte("A"), "A"},
	}
	for _, tt := range tests {
		if got := TrimPadded(tt.input); got != tt.want {
			t.Errorf("TrimPadded(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDirEntryNameString(t *testing.T) {
	t.Parallel()
	e := DirEntry{Name: PadLabel("HOOVER")}
	if got := e.NameString(); got != "HOOVER" {
		t.Errorf("NameString() = %q, want %q", got, "HOOVER")
	}
}

func TestVoiceAreaSectors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		nvoice int
		want   int
	}{
		{0, 0},
		{1, 1},
		{4, 1},
		{5, 2},
		{64, 16},
	}
	for _, tt := range tests {
		if got := VoiceAreaSectors(tt.nvoice); got != tt.want {
			t.Errorf("VoiceAreaSectors(%d) = %d, want %d", tt.nvoice, got, tt.want)
		}
	}
}

func TestCATClearAllocated(t *testing.T) {
	t.Parallel()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := img.CATSetAllocated(5); err != nil {
		t.Fatal(err)
	}
	if !img.CATAllocated(5) {
		t.Fatal("sector 5 should be allocated after CATSetAllocated")
	}
	if err := img.CATClearAllocated(5); err != nil {
		t.Fatal(err)
	}
	if img.CATAllocated(5) {
		t.Error("sector 5 should be free after CATClearAllocated")
	}
}

func TestCATClearAllocatedOutOfRange(t *testing.T) {
	t.Parallel()
	img := &Image{}
	if err := img.CATClearAllocated(-1); err == nil {
		t.Error("expected error for sector -1")
	}
	if err := img.CATClearAllocated(SectorCount); err == nil {
		t.Error("expected error for sector SectorCount")
	}
}

func TestRemoveFile(t *testing.T) {
	t.Parallel()
	img := buildFormattedImage(t)
	freeBefore := img.FreeSectors()

	addFakeVoice(t, img, "TESTVOX")

	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].NameString() != "TESTVOX" {
		t.Fatalf("expected TESTVOX, got %q", entries[0].NameString())
	}

	if err := img.RemoveFile("TESTVOX"); err != nil {
		t.Fatal(err)
	}

	entries, err = img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after remove, got %d", len(entries))
	}
	if got := img.FreeSectors(); got != freeBefore {
		t.Errorf("free sectors: got %d, want %d (sectors not freed)", got, freeBefore)
	}
}

func TestRemoveFileNotFound(t *testing.T) {
	t.Parallel()
	img := buildFormattedImage(t)
	if err := img.RemoveFile("NONEXISTENT"); err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestRemoveFileCompactsDirectory(t *testing.T) {
	t.Parallel()
	img := buildFormattedImage(t)

	addFakeVoice(t, img, "VOICEA")
	addFakeVoice(t, img, "VOICEB")
	addFakeVoice(t, img, "VOICEC")

	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	if err := img.RemoveFile("VOICEB"); err != nil {
		t.Fatal(err)
	}

	entries, err = img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after removing B, got %d", len(entries))
	}
	if entries[0].NameString() != "VOICEA" {
		t.Errorf("entry 0: got %q, want VOICEA", entries[0].NameString())
	}
	if entries[1].NameString() != "VOICEC" {
		t.Errorf("entry 1: got %q, want VOICEC", entries[1].NameString())
	}
}

func TestRemoveFileFreesDisSector(t *testing.T) {
	t.Parallel()
	img := buildFormattedImage(t)

	addFakeVoice(t, img, "TESTVOX")

	freeBefore := img.FreeSectors()

	if err := img.RemoveFile("TESTVOX"); err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}

	freeAfter := img.FreeSectors()
	if freeAfter <= freeBefore {
		t.Errorf("free sectors did not increase: before=%d after=%d", freeBefore, freeAfter)
	}

	formatted := buildFormattedImage(t)
	if freeAfter != formatted.FreeSectors() {
		t.Errorf("free sectors after remove (%d) != freshly formatted (%d)", freeAfter, formatted.FreeSectors())
	}
}

// TestRemoveFileRejectsCorruptDIS mirrors TestGetRejectsCorruptDIS in
// pkg/diskget. A corrupt directory entry whose DisSector points at sector 0
// (label/CAT, spec §1-1) or sector 1 (directory, spec §1-2) must not cause
// RemoveFile to mis-parse those bytes as a DIS and free arbitrary CAT bits.
// RemoveFile must surface ErrCorruptDIS without mutating the CAT bitmap so
// the diskadd.ReplaceInMemory snapshot/restore can roll the image back.
func TestRemoveFileRejectsCorruptDIS(t *testing.T) {
	t.Parallel()

	for _, badSector := range []uint16{0, 1} {
		t.Run(fmt.Sprintf("DisSector=%d", badSector), func(t *testing.T) {
			t.Parallel()
			img := buildFormattedImage(t)
			addFakeVoice(t, img, "TESTVOX")

			// Overwrite the entry's DisSector field with the reserved value.
			dirOff := DirSector * SectorSize
			binary.LittleEndian.PutUint16(img.Bytes()[dirOff+LabelSize+2:dirOff+LabelSize+4], badSector)

			// Snapshot the full CAT region so we can confirm the guard
			// returned without touching any allocation bit. In particular
			// the low two bits of CAT byte 0 (sectors 0 and 1, reserved
			// per spec §1-1/§1-2) must remain set.
			catBefore := append([]byte(nil), img.Bytes()[CATOffset:CATPhysicalEnd]...)
			if catBefore[0]&0x03 != 0x03 {
				t.Fatalf("precondition: CAT byte 0 low bits = 0x%02x, want 0x03", catBefore[0]&0x03)
			}

			err := img.RemoveFile("TESTVOX")
			if err == nil {
				t.Fatal("expected error for directory entry pointing at reserved sector")
			}
			if !errors.Is(err, ErrCorruptDIS) {
				t.Errorf("expected ErrCorruptDIS, got %v", err)
			}

			// Sectors 0 and 1 must still be marked allocated.
			catAfter := img.Bytes()[CATOffset:CATPhysicalEnd]
			if catAfter[0]&0x03 != 0x03 {
				t.Errorf("CAT byte 0 low bits: got 0x%02x, want 0x03 (reserved sectors must remain allocated)", catAfter[0]&0x03)
			}
			// Whole CAT must be untouched: the guard runs before any
			// CATClearAllocated call, so no bit anywhere should change.
			if !bytes.Equal(catBefore, catAfter) {
				t.Errorf("CAT bitmap mutated by RemoveFile despite ErrCorruptDIS refusal")
			}
		})
	}
}

func TestRateByteToDisplay(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input uint8
		want  int
	}{
		{0, 0}, {1, 0}, {32, 25}, {63, 49}, {64, 50}, {96, 75}, {126, 98}, {127, 99},
	}
	for _, tc := range tests {
		if got := RateByteToDisplay(tc.input); got != tc.want {
			t.Errorf("RateByteToDisplay(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestStopByteToDisplay(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input uint8
		want  int
	}{
		{0, 0}, {1, 1}, {3, 2}, {5, 2}, {26, 11}, {51, 20}, {128, 50}, {218, 85}, {255, 99},
	}
	for _, tc := range tests {
		if got := StopByteToDisplay(tc.input); got != tc.want {
			t.Errorf("StopByteToDisplay(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func buildFormattedImage(t *testing.T) *Image {
	t.Helper()
	data := make([]byte, ImageSize)
	img, err := ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	label := PadLabel("TESTDISK")
	copy(img.Bytes()[LabelOffset:LabelOffset+LabelSize], label[:])
	img.Bytes()[DiskNameTagOffset] = DiskNameTag
	copy(img.Bytes()[PasswordOffset:PasswordOffset+LabelSize], label[:])
	img.Bytes()[CATOffset] = 0x03
	for i := CATPhysicalEnd; i < SectorSize; i++ {
		img.Bytes()[i] = 0xff
	}
	return img
}

func TestRateIndexFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rate uint32
		idx  uint8
		ok   bool
	}{
		{36000, 0, true},
		{18000, 1, true},
		{9000, 2, true},
		{44100, 0, false},
	}
	for _, tt := range tests {
		idx, ok := RateIndexFor(tt.rate)
		if ok != tt.ok {
			t.Errorf("RateIndexFor(%d): ok=%v, want %v", tt.rate, ok, tt.ok)
		}
		if ok && idx != tt.idx {
			t.Errorf("RateIndexFor(%d): idx=%d, want %d", tt.rate, idx, tt.idx)
		}
	}
}

func TestSampleRate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		idx  uint8
		want uint32
	}{
		{0, 36000},
		{1, 18000},
		{2, 9000},
		{3, 0},
	}
	for _, tt := range tests {
		if got := SampleRate(tt.idx); got != tt.want {
			t.Errorf("SampleRate(%d): got %d, want %d", tt.idx, got, tt.want)
		}
	}
}

func TestSampleRatesLength(t *testing.T) {
	t.Parallel()
	if len(SampleRates) != 3 {
		t.Errorf("SampleRates length: got %d, want 3", len(SampleRates))
	}
}

func TestValidateRate(t *testing.T) {
	t.Parallel()
	for _, rate := range []uint32{36000, 18000, 9000} {
		if err := ValidateRate(rate); err != nil {
			t.Errorf("ValidateRate(%d) returned unexpected error: %v", rate, err)
		}
	}
	for _, rate := range []uint32{0, 44100, 48000, 12345} {
		if err := ValidateRate(rate); err == nil {
			t.Errorf("ValidateRate(%d) expected error, got nil", rate)
		}
	}
}

func TestNumSampleRates(t *testing.T) {
	t.Parallel()
	if got := NumSampleRates(); got != 3 {
		t.Errorf("NumSampleRates() = %d, want 3", got)
	}
}

func TestSampleRatesSlice(t *testing.T) {
	t.Parallel()
	got := SampleRatesSlice()
	want := []uint32{36000, 18000, 9000}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, v := range got {
		if v != want[i] {
			t.Errorf("index %d: got %d, want %d", i, v, want[i])
		}
	}
	got[0] = 99999
	fresh := SampleRatesSlice()
	if fresh[0] != 36000 {
		t.Errorf("mutation leaked: got %d, want 36000", fresh[0])
	}
}

func TestFileSizeCorruptExtent(t *testing.T) {
	t.Parallel()
	d := DisSector{
		Extents: [][2]uint16{{10, 5}, {20, 25}},
	}
	got := d.FileSize()
	want := (25 - 20 + 1) * SectorSize
	if got != want {
		t.Fatalf("FileSize with corrupt extent: got %d, want %d", got, want)
	}
}

func addFakeVoice(t *testing.T, img *Image, name string) {
	t.Helper()
	fileData := make([]byte, 2*SectorSize)
	nameLabel := PadLabel(name)
	copy(fileData[VoiceNameOffset:], nameLabel[:])

	dataSectorCount := SectorsNeeded(len(fileData))
	allocated, err := img.AllocateSectors(1 + dataSectorCount)
	if err != nil {
		t.Fatal(err)
	}
	disSectorIdx := allocated[0]
	dataSectors := allocated[1:]

	var dis DisSector
	allSectors := append([]int{disSectorIdx}, dataSectors...)
	start := allSectors[0]
	end := allSectors[0]
	for _, s := range allSectors[1:] {
		if s == end+1 {
			end = s
		} else {
			dis.Extents = append(dis.Extents, [2]uint16{uint16(start), uint16(end)}) //nolint:gosec // G115: test values fit uint16
			start = s
			end = s
		}
	}
	dis.Extents = append(dis.Extents, [2]uint16{uint16(start), uint16(end)}) //nolint:gosec // G115: test values fit uint16
	dis.VoiceCount = 1
	dis.WaveCount = uint16(dataSectorCount - 1) //nolint:gosec // G115: test value fits uint16

	if err := img.SetSector(disSectorIdx, EncodeDisSector(dis)); err != nil {
		t.Fatal(err)
	}
	padded := make([]byte, dataSectorCount*SectorSize)
	copy(padded, fileData)
	for i, sec := range dataSectors {
		b := padded[i*SectorSize : (i+1)*SectorSize]
		if err := img.SetSector(sec, b); err != nil {
			t.Fatal(err)
		}
	}

	dirOff, err := img.NextFreeDirSlot()
	if err != nil {
		t.Fatal(err)
	}
	entry := DirEntry{
		Name:      PadLabel(name),
		FileType:  TypeVoice,
		DiskNum:   0,
		DisSector: uint16(disSectorIdx), //nolint:gosec // G115: test value fits uint16
	}
	copy(img.Bytes()[dirOff:dirOff+DirEntrySize], EncodeDirEntry(entry))
}
