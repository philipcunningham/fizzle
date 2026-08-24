package fzf_test

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/model"
)

func TestStandaloneDocumentOwnsBytesAndLayout(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Layout().VoiceCountSource() != fzutil.VoiceCountMarker ||
		doc.Layout().VoiceCount() != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("layout source/voices = %v/%d, want marker/%d",
			doc.Layout().VoiceCountSource(), doc.Layout().VoiceCount(), fzfbuilder.BanklessDumpVoices)
	}

	want := doc.Bytes()[0]
	data[0] ^= 0xff
	if got := doc.Bytes()[0]; got != want {
		t.Fatalf("document changed with constructor input: got %#x, want %#x", got, want)
	}
	returned := doc.Bytes()
	returned[0] ^= 0xff
	if got := doc.Bytes()[0]; got != want {
		t.Fatalf("document changed through returned bytes: got %#x, want %#x", got, want)
	}
}

func TestDiskFileDocumentRetainsDISAuthority(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	doc, err := fzf.NewDiskFile(data, fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Layout().VoiceCountSource() != fzutil.VoiceCountDIS ||
		doc.Layout().VoiceCount() != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("layout source/voices = %v/%d, want DIS/%d",
			doc.Layout().VoiceCountSource(), doc.Layout().VoiceCount(), fzfbuilder.BanklessDumpVoices)
	}

	want := doc.Bytes()[0]
	data[0] ^= 0xff
	if got := doc.Bytes()[0]; got != want {
		t.Fatalf("document changed with constructor input: got %#x, want %#x", got, want)
	}
}

func TestDiskFileDocumentFallsBackFromUnsupportedDISCount(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	doc, err := fzf.NewDiskFile(data, 60)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Layout().VoiceCountSource() != fzutil.VoiceCountWalk {
		t.Fatalf("source = %v, want walk", doc.Layout().VoiceCountSource())
	}
}

func TestDocumentRejectsUnreadableBytes(t *testing.T) {
	if _, err := fzf.NewStandalone([]byte("not an FZF")); err == nil {
		t.Fatal("standalone document accepted unreadable bytes")
	}
	if _, err := fzf.NewDiskFile([]byte("not an FZF"), 1); err == nil {
		t.Fatal("disk-file document accepted unreadable bytes")
	}
}

func TestDiskFileDocumentRejectsInvalidDISCount(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	if _, err := fzf.NewDiskFile(data, 0); err == nil {
		t.Fatal("disk-file document accepted a zero DIS voice count")
	}
}

func TestRenameVoiceReturnsAtomicPatchesAndRetainsMarkerAuthority(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Bytes()

	patches, err := doc.RenameVoice(0, "NEW NAME")
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("patch count = %d, want name and marker patches", len(patches))
	}
	stale := doc.Bytes()
	stale[patches[0].Offset] ^= 0xff
	staleBefore := bytes.Clone(stale)
	if err := model.Apply(stale, patches); err == nil {
		t.Fatal("expected stale voice pre-image to reject the name and marker batch")
	}
	if !bytes.Equal(stale, staleBefore) {
		t.Fatal("rejected rename mutated the stale document")
	}
	updated := doc.Bytes()
	if err := model.Apply(updated, patches); err != nil {
		t.Fatal(err)
	}
	reopened, err := fzf.NewStandalone(updated)
	if err != nil {
		t.Fatal(err)
	}
	voice, err := reopened.Voice(0)
	if err != nil {
		t.Fatal(err)
	}
	if voice.Name() != "NEW NAME" {
		t.Fatalf("renamed voice = %q, want NEW NAME", voice.Name())
	}
	if !bytes.Equal(doc.Bytes(), before) {
		t.Fatal("rename mutated the original document")
	}
	if reopened.Layout().VoiceCountSource() != fzutil.VoiceCountMarker ||
		reopened.Layout().VoiceCount() != doc.Layout().VoiceCount() {
		t.Fatalf("reopened layout source/voices = %v/%d, want marker/%d",
			reopened.Layout().VoiceCountSource(), reopened.Layout().VoiceCount(), doc.Layout().VoiceCount())
	}
}

func TestRenameVoiceRejectsInvalidInputWithoutMutation(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	doc, err := fzf.NewDiskFile(data, fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Bytes()
	for _, test := range []struct {
		name  string
		slot  int
		value string
	}{
		{name: "empty name", slot: 0, value: ""},
		{name: "non-ASCII name", slot: 0, value: "BAD\u2603"},
		{name: "long name", slot: 0, value: "THIS NAME IS TOO LONG"},
		{name: "missing slot", slot: doc.Layout().VoiceCount(), value: "NAME"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := doc.RenameVoice(test.slot, test.value); err == nil {
				t.Fatal("RenameVoice accepted invalid input")
			}
			if !bytes.Equal(doc.Bytes(), before) {
				t.Fatal("failed rename mutated the document")
			}
		})
	}
}
