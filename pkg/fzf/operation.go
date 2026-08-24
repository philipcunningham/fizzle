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
	voiceCount  int
	audioStart  int
	hasGeometry bool
}

// IsStructural reports whether applying the result replaces document
// structure rather than changing fixed-size fields.
func (r OperationResult) IsStructural() bool { return r.kind == operationStructural }

// VoiceGeometry returns updated voice geometry when a structural operation
// changes it.
func (r OperationResult) VoiceGeometry() (voiceCount, audioStart int, ok bool) {
	return r.voiceCount, r.audioStart, r.hasGeometry
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

func structuralAreaOperation(preimage, replacement []byte, voiceCount, audioStart int) OperationResult {
	result := structuralOperation(preimage, replacement)
	result.voiceCount = voiceCount
	result.audioStart = audioStart
	result.hasGeometry = true
	return result
}

// Apply validates the operation against current and returns new owned bytes.
// current is unchanged on success and failure.
func (r OperationResult) Apply(current []byte) ([]byte, error) {
	return r.apply(bytes.Clone(current))
}

// ApplyOwned applies the operation to a buffer whose ownership the caller
// transfers. Fixed-size operations reuse that buffer. Structural operations
// return fresh replacement bytes because the result retains its own copy.
func (r OperationResult) ApplyOwned(current []byte) ([]byte, error) {
	return r.apply(current)
}

func (r OperationResult) apply(current []byte) ([]byte, error) {
	switch r.kind {
	case operationFixed:
		if err := model.Apply(current, r.patches); err != nil {
			return nil, fmt.Errorf("fzf: applying fixed-size operation: %w", err)
		}
		return current, nil
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
