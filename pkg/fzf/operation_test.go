package fzf

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/model"
)

func TestFixedOperationAppliesAtomicallyToMatchingDocument(t *testing.T) {
	original := []byte{1, 2, 3}
	result := fixedOperation([]model.Patch{{Offset: 1, Old: []byte{2}, New: []byte{9}}})

	updated, err := result.Apply(original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(updated, []byte{1, 9, 3}) {
		t.Fatalf("updated = %v", updated)
	}
	if !bytes.Equal(original, []byte{1, 2, 3}) {
		t.Fatalf("operation changed its input: %v", original)
	}
}

func TestFixedOperationRejectsStaleDocumentWithoutMutation(t *testing.T) {
	current := []byte{1, 8, 3}
	result := fixedOperation([]model.Patch{{Offset: 1, Old: []byte{2}, New: []byte{9}}})

	if _, err := result.ApplyOwned(current); err == nil {
		t.Fatal("stale operation succeeded")
	}
	if !bytes.Equal(current, []byte{1, 8, 3}) {
		t.Fatalf("failed operation changed its input: %v", current)
	}
}

func TestFixedOperationReusesTransferredBuffer(t *testing.T) {
	owned := []byte{1, 2, 3}
	result := fixedOperation([]model.Patch{{Offset: 1, Old: []byte{2}, New: []byte{9}}})

	updated, err := result.ApplyOwned(owned)
	if err != nil {
		t.Fatal(err)
	}
	if &updated[0] != &owned[0] {
		t.Fatal("fixed operation copied the transferred buffer")
	}
	if !bytes.Equal(owned, []byte{1, 9, 3}) {
		t.Fatalf("owned = %v", owned)
	}
}

func TestStructuralOperationRejectsWrongPreimageAndOwnsReplacement(t *testing.T) {
	original := []byte{1, 2, 3}
	replacement := []byte{4, 5, 6, 7}
	result := structuralOperation(original, replacement)
	replacement[0] = 0

	updated, err := result.Apply(original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(updated, []byte{4, 5, 6, 7}) {
		t.Fatalf("updated = %v", updated)
	}
	updated[0] = 0
	again, err := result.Apply(original)
	if err != nil {
		t.Fatal(err)
	}
	if again[0] != 4 {
		t.Fatal("operation exposed its replacement buffer")
	}
	if _, err := result.Apply([]byte{1, 2, 9}); err == nil {
		t.Fatal("structural operation accepted the wrong preimage")
	}
}

func TestZeroOperationIsInvalid(t *testing.T) {
	if _, err := (OperationResult{}).Apply([]byte{1}); err == nil {
		t.Fatal("zero operation succeeded")
	}
}
