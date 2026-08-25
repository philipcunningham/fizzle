package diskfs_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
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
