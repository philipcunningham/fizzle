package diskfs_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	"github.com/philipcunningham/fizzle/pkg/fzf"
)

func image(t *testing.T) *disk.Image {
	t.Helper()
	data, err := diskformat.BuildImage("TEST")
	if err != nil {
		t.Fatal(err)
	}
	img, err := disk.ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestAddExtractAndList(t *testing.T) {
	img := image(t)
	data := bytes.Repeat([]byte{0x5a}, disk.SectorSize+17)
	file := diskfs.File{Name: disk.PadLabel("PROGRAM"), Type: disk.TypeProgram}
	if err := diskfs.Add(img, data, file); err != nil {
		t.Fatal(err)
	}
	got, err := diskfs.Extract(img, "program")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:len(data)], data) {
		t.Fatal("extracted payload differs")
	}
	listing, err := diskfs.List(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "PROGRAM" || listing.Entries[0].Type != disk.TypeProgram {
		t.Fatalf("listing = %+v", listing.Entries)
	}
}

func TestReplaceRollsBackOnFailure(t *testing.T) {
	img := image(t)
	file := diskfs.File{Name: disk.PadLabel("PROGRAM"), Type: disk.TypeProgram}
	if err := diskfs.Add(img, []byte{1}, file); err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(img.Bytes())
	err := diskfs.Replace(img, "PROGRAM", make([]byte, disk.ImageSize), file)
	if !errors.Is(err, disk.ErrNoSpace) {
		t.Fatalf("error = %v, want ErrNoSpace", err)
	}
	if !bytes.Equal(img.Bytes(), before) {
		t.Fatal("failed replacement changed the image")
	}
}

func TestFullDumpStoresWalkedVoiceCount(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "corpus",
		"casio-fz1-soundwaves", "accompaniment", "harpsichord",
		"Harpsichord.fzf")
	dump, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("full hardware corpus is not installed")
	}
	if err != nil {
		t.Fatal(err)
	}
	file, err := diskfs.FullDump(dump, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if file.Voices != 32 {
		t.Fatalf("walked voice count = %d, want 32", file.Voices)
	}
	bstepSum := 0
	doc, err := fzf.NewStandalone(dump)
	if err != nil {
		t.Fatal(err)
	}
	for bank := range doc.Layout().BankCount() {
		off := bank*disk.SectorSize + disk.BankVoiceCountOffset
		bstepSum += int(binary.LittleEndian.Uint16(dump[off : off+2]))
	}
	if bstepSum != 177 {
		t.Fatalf("fixture bstep sum = %d, want the misleading count 177", bstepSum)
	}
	img := image(t)
	if err := diskfs.Add(img, dump, file); err != nil {
		t.Fatal(err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory entries = %d, want 1", len(entries))
	}
	disBytes, err := img.SectorRef(int(entries[0].DisSector))
	if err != nil {
		t.Fatal(err)
	}
	dis, err := disk.DecodeDisSector(disBytes)
	if err != nil {
		t.Fatal(err)
	}
	if dis.VoiceCount != 32 {
		t.Fatalf("stored DIS voice count = %d, want walked count 32", dis.VoiceCount)
	}
	wantWaves := disk.SectorsNeeded(len(dump)) - file.Banks - disk.VoiceAreaSectors(32)
	if int(dis.WaveCount) != wantWaves {
		t.Fatalf("stored DIS wave count = %d, want %d from the walked voice boundary", dis.WaveCount, wantWaves)
	}
}
