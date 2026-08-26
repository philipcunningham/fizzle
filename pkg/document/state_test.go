package document_test

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/document"
)

func image(t *testing.T, label string) []byte {
	t.Helper()
	data, err := diskformat.BuildImage(label)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStateOwnsPairAndAuthority(t *testing.T) {
	t.Parallel()
	one := image(t, "ONE")
	two := image(t, "TWO")
	state, err := document.NewState(one, two, document.AuthorityDIS)
	if err != nil {
		t.Fatal(err)
	}
	one[0], two[0] = 0xff, 0xff
	gotOne, _ := state.Image(0)
	gotTwo, _ := state.Image(1)
	if gotOne[0] == 0xff || gotTwo[0] == 0xff || !state.UsesDIS() || state.SizeBytes() != len(gotOne)+len(gotTwo) {
		t.Fatalf("state did not retain owned pair and authority")
	}
	gotOne[0] = 0xee
	again, _ := state.Image(0)
	if bytes.Equal(gotOne, again) {
		t.Fatal("Image returned mutable canonical bytes")
	}
}

func TestStateRejectsInvalidOrOrphanImages(t *testing.T) {
	t.Parallel()
	if _, err := document.NewState([]byte("bad"), nil, document.AuthorityWalk); err == nil {
		t.Fatal("accepted invalid disk 1")
	}
	if _, err := document.NewState(nil, image(t, "TWO"), document.AuthorityWalk); err == nil {
		t.Fatal("accepted disk 2 without disk 1")
	}
}
