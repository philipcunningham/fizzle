package integration_test

// Regression tests over PREY.img, a full dump a real FZ-1 saved: a
// deleted entry blanks directory slot 0 ahead of the live FULL-DATA-FZ,
// the fifth voice (CHEMICAL) belongs to no bank, and three stale voice
// slots sit past the DIS tail's vn=5. See
// llm-wiki/findings/directory-blank-slots.md.

import (
	"bytes"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/disklist"
	"github.com/philipcunningham/fizzle/pkg/webcore"
)

const preyImg = "../../testdata/synthetic/PREY.img"

func TestFirmwareDiskListsPastDeletedEntry(t *testing.T) {
	skipShort(t)
	listing, err := disklist.Parse(preyImg)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 {
		t.Fatalf("entries = %d, want 1 (the deleted slot must be skipped, not treated as the end)", len(listing.Entries))
	}
	e := listing.Entries[0]
	if e.Name != disk.FullDumpName || e.TypeName != disk.TypeFullDumpLabel {
		t.Errorf("entry = %q (%s), want %s (%s)", e.Name, e.TypeName, disk.FullDumpName, disk.TypeFullDumpLabel)
	}
}

func TestFirmwareDiskOpensWithAllVoices(t *testing.T) {
	skipShort(t)
	data, err := os.ReadFile(preyImg)
	if err != nil {
		t.Fatal(err)
	}
	s := webcore.NewSession()
	snap, cerr := s.OpenImage(data)
	if cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	inst := snap.Disk.Instrument
	if inst == nil {
		t.Fatal("no instrument parsed")
	}
	names := make([]string, len(inst.Voices))
	for i, v := range inst.Voices {
		names[i] = v.Name
	}
	want := []string{"NOT LISTENG", "ADDICTED", "REGRET", "NOISE 1", "CHEMICAL"}
	if len(names) != len(want) {
		t.Fatalf("voices = %v, want %v (DIS vn=5 must beat the bstep walk's 4)", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Errorf("voice %d = %q, want %q", i, names[i], name)
		}
	}
	// wn=62 wave sectors of audio.
	if got := snap.Disk.AudioBytes; got != 62*disk.SectorSize {
		t.Errorf("AudioBytes = %d, want %d", got, 62*disk.SectorSize)
	}

	// Opening then exporting is byte identical.
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	if len(out) != len(data) {
		t.Fatalf("export length %d, want %d", len(out), len(data))
	}
	for i := range out {
		if out[i] != data[i] {
			t.Fatalf("export differs from source at byte %d", i)
		}
	}
}

func TestFirmwareDiskEditPreservesDISCounts(t *testing.T) {
	skipShort(t)
	data, err := os.ReadFile(preyImg)
	if err != nil {
		t.Fatal(err)
	}
	s := webcore.NewSession()
	if _, cerr := s.OpenImage(data); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	if _, cerr := s.RenameBank(0, "EDITED"); cerr != nil {
		t.Fatalf("RenameBank: %v", cerr)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}

	img, err := disk.ReadImage(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries after edit = %d, want 1", len(entries))
	}
	sec, err := img.SectorRef(int(entries[0].DisSector))
	if err != nil {
		t.Fatal(err)
	}
	dis, err := disk.DecodeDisSector(sec)
	if err != nil {
		t.Fatal(err)
	}
	if dis.BankCount != 2 || dis.VoiceCount != 5 || dis.WaveCount != 62 {
		t.Errorf("DIS tail after edit = bn %d vn %d wn %d, want bn 2 vn 5 wn 62 (the sampler reads these)",
			dis.BankCount, dis.VoiceCount, dis.WaveCount)
	}
}
