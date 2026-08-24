package fzf

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/model"
)

type operationKind uint8

const (
	operationInvalid operationKind = iota
	operationFixed
	operationStructural
)

// OperationResult is the atomic output of one document operation. It owns
// either a fixed-size patch batch or a whole-document replacement.
type OperationResult struct {
	kind        operationKind
	patches     []model.Patch
	preimage    []byte
	replacement []byte
}

func fixedOperation(patches []model.Patch) OperationResult {
	owned := make([]model.Patch, len(patches))
	for i, patch := range patches {
		owned[i] = model.Patch{
			Offset: patch.Offset,
			Old:    bytes.Clone(patch.Old),
			New:    bytes.Clone(patch.New),
		}
	}
	return OperationResult{kind: operationFixed, patches: owned}
}

func structuralOperation(preimage, replacement []byte) OperationResult {
	return OperationResult{
		kind:        operationStructural,
		preimage:    bytes.Clone(preimage),
		replacement: bytes.Clone(replacement),
	}
}

// Apply validates the operation against current and returns new owned bytes.
// current is unchanged on success and failure.
func (r OperationResult) Apply(current []byte) ([]byte, error) {
	switch r.kind {
	case operationFixed:
		updated := bytes.Clone(current)
		if err := model.Apply(updated, r.patches); err != nil {
			return nil, fmt.Errorf("fzf: applying fixed-size operation: %w", err)
		}
		return updated, nil
	case operationStructural:
		if !bytes.Equal(current, r.preimage) {
			return nil, errors.New("fzf: structural operation pre-image mismatch")
		}
		return bytes.Clone(r.replacement), nil
	case operationInvalid:
		return nil, errors.New("fzf: invalid operation result")
	default:
		return nil, errors.New("fzf: invalid operation result")
	}
}
