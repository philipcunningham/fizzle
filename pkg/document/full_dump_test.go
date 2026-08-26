package document_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	"github.com/philipcunningham/fizzle/pkg/document"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

func TestReplaceFullDumpReturnsNewStateAndLeavesReceiverUntouched(t *testing.T) {
	t.Parallel()
	state, err := document.NewState(image(t, "ONE"), nil, document.AuthorityWalk)
	if err != nil {
		t.Fatal(err)
	}
	before := state.Image1()
	dump, _ := fzfbuilder.MakeTestFZF(t, []string{"VOICE"})

	next, err := state.ReplaceFullDump(dump, 0, document.AuthorityDIS)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(state.Image1(), before) {
		t.Fatal("ReplaceFullDump mutated its receiver")
	}
	if !next.UsesDIS() || next.HasSecondDisk() {
		t.Fatalf("replacement state authority/pair = DIS %v, second %v", next.UsesDIS(), next.HasSecondDisk())
	}
}

func TestReplaceFullDumpFailureLeavesPairByteIdentical(t *testing.T) {
	t.Parallel()
	state, err := document.NewState(image(t, "ONE"), image(t, "TWO"), document.AuthorityDIS)
	if err != nil {
		t.Fatal(err)
	}
	one, two := state.Image1(), state.Image2()
	_, err = state.ReplaceFullDump(make([]byte, voicebuild.MaxDiskFileBytes+1), 0, document.AuthorityDIS)
	if err == nil {
		t.Fatal("ReplaceFullDump accepted malformed oversized bytes")
	}
	if !bytes.Equal(state.Image1(), one) || !bytes.Equal(state.Image2(), two) {
		t.Fatal("failed replacement changed the pair")
	}
}

func TestPlaceSplitRejectsLooseFilesWithoutMutation(t *testing.T) {
	t.Parallel()
	base := image(t, "ONE")
	img, err := disk.ReadImage(bytes.NewReader(base))
	if err != nil {
		t.Fatal(err)
	}
	file := diskfs.File{Name: disk.PadLabel("LOOSE"), Type: disk.TypeProgram}
	if err := diskfs.Add(img, []byte{1}, file); err != nil {
		t.Fatal(err)
	}
	state, err := document.NewState(img.Bytes(), nil, document.AuthorityDIS)
	if err != nil {
		t.Fatal(err)
	}
	before := state.Image1()
	result := voicebuild.MultiDiskResult{Disks: [][]byte{{1}, {2}}, BankCount: 1, VoiceCount: 1, WaveCount: 1}
	_, err = state.PlaceSplit(result, document.AuthorityDIS)
	var occupied *document.ErrSplitNeedsEmptyDisk
	if !errors.As(err, &occupied) || occupied.Files != 1 {
		t.Fatalf("error = %v, want one loose-file refusal", err)
	}
	if !bytes.Equal(state.Image1(), before) {
		t.Fatal("failed split placement changed disk 1")
	}
}
