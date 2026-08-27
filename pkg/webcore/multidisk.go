package webcore

import (
	"bytes"
	"errors"

	"github.com/philipcunningham/fizzle/pkg/disk"
	documentmodel "github.com/philipcunningham/fizzle/pkg/document"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

// OpenImagePair opens a two disk split instrument as one document
// (R5). The images may arrive in either order: the directory entry's
// disk number byte says which is which.
func (s *Session) OpenImagePair(a, b []byte) (Snapshot, *Error) {
	state, err := documentmodel.OpenPair(a, b)
	if err != nil {
		return s.Snapshot(), errf(codePairMismatch, "%v", err)
	}
	img, err := disk.ReadImage(bytes.NewReader(state.Image1()))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "%v", err)
	}
	return s.adoptFresh(img, state.Image2())
}

func (s *Session) stitchedDump(img *disk.Image) ([]byte, *Error) {
	state, err := documentmodel.NewState(img.Bytes(), s.state.Image2(), authorityFromDIS(s.state.UsesDIS()))
	if err != nil {
		return nil, errf("invalid-image", "%v", err)
	}
	data, err := state.StitchedFullDump()
	if err != nil {
		return nil, errf(codeNotFound, "%v", err)
	}
	return data, nil
}

// replaceDump writes a full dump back into the document, splitting
// across two images when it outgrows one disk and collapsing back to
// one when it no longer does. img is the parsed disk 1 scratch image;
// on error the session state is untouched. vn is the voice count the
// DIS tail must carry, 0 to let content detection derive it.
func (s *Session) replaceDump(img *disk.Image, fzf []byte, vn int, mode parseMode) (Snapshot, *Error) {
	usesDIS := s.state.UsesDIS()
	if mode == modeDerive {
		usesDIS = documentDISMode(img)
	}
	working, err := documentmodel.NewState(img.Bytes(), s.state.Image2(), authorityFromDIS(usesDIS))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "%v", err)
	}
	next, err := working.ReplaceFullDump(fzf, vn, authorityFromDIS(usesDIS))
	if err != nil {
		return s.Snapshot(), documentMutationError(err)
	}
	nextImage, err := disk.ReadImage(bytes.NewReader(next.Image1()))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "%v", err)
	}
	return s.adoptPair(nextImage, next.Image2(), mode)
}

func documentMutationError(err error) *Error {
	var occupied *documentmodel.ErrSplitNeedsEmptyDisk
	if errors.As(err, &occupied) {
		return errf("no-space", "a two disk instrument fills disk 1 completely; extract the disk's %d other files first", occupied.Files)
	}
	if errors.Is(err, disk.ErrNoSpace) {
		return addError(err)
	}
	return splitError(err)
}

// splitError maps the split's failure modes onto boundary codes (R10:
// what cannot fit a two disk set is rejected with the core's error).
func splitError(err error) *Error {
	var tmd *voicebuild.ErrTooManyDisks
	if errors.As(err, &tmd) {
		return errf("too-large", "%v", tmd)
	}
	var ram *voicebuild.ErrSampleRAMExceeded
	if errors.As(err, &ram) {
		return errf("ram-exceeded", "%v", ram)
	}
	return errf(codeInvalidValue, "%v", err)
}
