package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/pool"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// testFZV builds a valid single-voice FZV (header + audio) large enough
// to pass assign's size validation.
const amenName = "AMEN"

func testFZV(name string) []byte {
	return voiceimport.Encode(make([]int16, 1024), 0, name, 0, voiceimport.NoLoop())
}

// TestRoundTrip_NewDisk_ImportPersists pins UXF (new-disk case): a voice
// imported into a brand-new disk must survive save -> reload. Before the
// fix the save skipped embedding the FULL-DATA-FZ (cached VoiceCount was
// never bumped on a header-less new container) and reloaded empty.
func TestRoundTrip_NewDisk_ImportPersists(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = a.assignPoolEntryToArea(&pool.Entry{Name: "TESTVOX", Bytes: testFZV("TESTVOX")}, 0, 0)

	target := filepath.Join(t.TempDir(), "MYDISK.img")
	saved, _ := a.doSaveTo(target)
	if got := saved.status.View(); strings.Contains(got, "Save failed") {
		t.Fatalf("save reported failure: %q", got)
	}

	_, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("reload after save lost the disk entirely: %v", err)
	}
	if info.VoiceCount < 1 {
		t.Errorf("new-disk import lost on save: reloaded VoiceCount=%d, want >=1", info.VoiceCount)
	}
}

// TestRoundTrip_SingleVoiceDisk_ImportPersists pins UXF (single-voice
// case): importing a second voice into a wrapped single-voice .img must
// survive save -> reload. Before the fix the save unwrapped to a bare
// FZV (one voice) and dropped the import.
func TestRoundTrip_SingleVoiceDisk_ImportPersists(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/HOOVER.img")
	if !st.a.containerInfo.WrappedVoice {
		t.Fatalf("HOOVER.img must be a wrapped single-voice disk (WrappedVoice=true); the fixture changed and this regression guard would otherwise pass vacuously")
	}
	st.a = st.a.assignPoolEntryToArea(&pool.Entry{Name: amenName, Bytes: testFZV(amenName)}, 0, 1)

	if err := st.a.saveContainerToImage(st.target); err != nil {
		t.Fatalf("save: %v", err)
	}
	_, info, err := loader.LoadContainer(st.target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if info.VoiceCount < 2 {
		t.Errorf("single-voice import lost on save: reloaded VoiceCount=%d, want >=2", info.VoiceCount)
	}
}

// TestRoundTrip_SingleVoiceDisk_BankRenamePersists pins UXD: a bank
// rename on a single-voice disk must survive save -> reload. Before the
// fix the FZV unwrap dropped the bank sector (and its name).
func TestRoundTrip_SingleVoiceDisk_BankRenamePersists(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/HOOVER.img")
	if !st.a.containerInfo.WrappedVoice {
		t.Fatalf("HOOVER.img must be a wrapped single-voice disk (WrappedVoice=true); the fixture changed and this regression guard would otherwise pass vacuously")
	}
	// Rename Bank 1 to "RENAMED" by patching the bank-name field, the
	// same bytes the rename modal writes.
	const want = "RENAMED"
	nameBytes := make([]byte, disk.VoiceNameFieldSize)
	for i := range nameBytes {
		nameBytes[i] = ' '
	}
	copy(nameBytes, want)
	old := append([]byte(nil), st.a.containerModel.Bytes()[disk.BankNameOffset:disk.BankNameOffset+disk.VoiceNameFieldSize]...)
	if err := st.a.containerModel.Apply(model.Patch{Offset: disk.BankNameOffset, Old: old, New: nameBytes}); err != nil {
		t.Fatalf("seed bank name: %v", err)
	}

	if err := st.a.saveContainerToImage(st.target); err != nil {
		t.Fatalf("save: %v", err)
	}
	m, _, err := loader.LoadContainer(st.target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := string(m.Bytes()[disk.BankNameOffset : disk.BankNameOffset+len(want)])
	if got != want {
		t.Errorf("bank rename lost on save: reloaded name = %q, want %q", got, want)
	}
}

// TestRoundTrip_FullDump_ImportPersists locks the already-working path:
// importing into a full-dump disk survives save -> reload.
func TestRoundTrip_FullDump_ImportPersists(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/TECHNO.img")
	if st.a.containerInfo.WrappedVoice {
		t.Fatalf("TECHNO.img must be a full dump (WrappedVoice=false); the fixture changed and this guard would otherwise pass vacuously")
	}
	before := st.a.containerInfo.VoiceCount
	st.a = st.a.assignPoolEntryToArea(&pool.Entry{Name: amenName, Bytes: testFZV(amenName)}, 0, 40)

	if err := st.a.saveContainerToImage(st.target); err != nil {
		t.Fatalf("save: %v", err)
	}
	_, info, err := loader.LoadContainer(st.target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if info.VoiceCount <= before {
		t.Errorf("full-dump import not persisted: before=%d after=%d", before, info.VoiceCount)
	}
}

// TestRoundTrip_SingleVoice_FaithfulWhenUnchanged pins the kept faithful
// round-trip: a wrapped single-voice disk with no added voices or bank
// names saves back as a single-voice .img (not promoted to a full dump).
func TestRoundTrip_SingleVoice_FaithfulWhenUnchanged(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/HOOVER.img")
	if !st.a.containerInfo.WrappedVoice {
		t.Fatalf("HOOVER.img must be a wrapped single-voice disk; the fixture changed and this regression guard would otherwise pass vacuously")
	}
	if err := st.a.saveContainerToImage(st.target); err != nil {
		t.Fatalf("save: %v", err)
	}
	_, info, err := loader.LoadContainer(st.target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !info.WrappedVoice {
		t.Errorf("unchanged single-voice disk was promoted to a full dump; faithful round-trip lost")
	}
}

// TestRoundTrip_SingleVoice_SaveTwiceAfterImport pins that a second save
// after a promoting import still works: reflectPromotion must flip the
// in-memory format so the next in-place save targets FULL-DATA-FZ, not
// the gone FZV entry.
func TestRoundTrip_SingleVoice_SaveTwiceAfterImport(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/HOOVER.img")
	if !st.a.containerInfo.WrappedVoice {
		t.Fatalf("HOOVER.img must be a wrapped single-voice disk; the fixture changed and this regression guard would otherwise pass vacuously")
	}
	st.a = st.a.assignPoolEntryToArea(&pool.Entry{Name: amenName, Bytes: testFZV(amenName)}, 0, 1)

	m, _ := st.a.handleSave() // first save promotes to full dump
	st.a, _ = m.(App)
	m, _ = st.a.handleSave() // second save must not fail on the gone FZV entry
	st.a, _ = m.(App)
	if got := st.a.status.View(); strings.Contains(got, "Save failed") {
		t.Fatalf("second save failed: %q", got)
	}

	_, info, err := loader.LoadContainer(st.target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if info.VoiceCount < 2 {
		t.Errorf("after two saves, reloaded VoiceCount=%d, want >=2", info.VoiceCount)
	}
}
