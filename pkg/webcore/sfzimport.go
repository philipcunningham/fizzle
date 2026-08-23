package webcore

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing/fstest"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// SFZResult reports an import that ran the conversion pipeline: the
// new document snapshot plus the sample rate the core actually used,
// which fit to disk may have stepped down from the request (R9). The
// UI warns when Rate is below the asked rate.
type SFZResult struct {
	Snapshot Snapshot `json:"snapshot"`
	Rate     int      `json:"rate"`
}

// ImportSFZ converts an SFZ instrument (its folder crossing the
// boundary as path to bytes pairs) through the CLI's pipeline and
// places it as the document's instrument, replacing any existing one
// (R7, R9). sfzPath names the .sfz inside files; empty means the one
// .sfz the folder holds. With split, the CLI's two disk build runs
// and the document becomes an image pair; otherwise fitToDisk steps
// the rate down to fit one disk, and a dump that still outgrows one
// disk splits automatically (R10 rejects only past two).
//
// channel is ChannelLeft, ChannelRight, or ChannelMix, as it is for
// ImportWAVFolder: the UI asks once and the answer covers every stereo
// sample the instrument references.
func (s *Session) ImportSFZ(files map[string][]byte, sfzPath string, rate int, fitToDisk, split bool, channel string) (SFZResult, *Error) {
	fail := func(cerr *Error) (SFZResult, *Error) {
		return SFZResult{Snapshot: s.Snapshot()}, cerr
	}
	if rate < 0 {
		return fail(errf("invalid-rate", "rate must be positive, got %d", rate))
	}
	if fitToDisk && split {
		return fail(errf(codeInvalidValue, "fit to disk and the two disk split are alternatives; choose one"))
	}
	ch, cerr := parseChannel(channel)
	if cerr != nil {
		return fail(cerr)
	}
	fsys := mapFS(files)
	if sfzPath == "" {
		found, cerr := loneSFZ(files)
		if cerr != nil {
			return fail(cerr)
		}
		sfzPath = found
	}

	ctx := context.Background()
	if split {
		result, err := sfzconvert.ConvertMultiDiskFS(ctx, fsys, sfzPath, uint32(rate), ch) // #nosec G115 -- validated by the pipeline
		if err != nil {
			return fail(convertError(err))
		}
		img, cerr := s.imageOrNew()
		if cerr != nil {
			return fail(cerr)
		}
		snap, cerr := s.placeSplitResult(img, result)
		if cerr != nil {
			return fail(cerr)
		}
		return SFZResult{Snapshot: snap, Rate: rate}, nil
	}

	res, err := sfzconvert.ConvertFS(ctx, fsys, sfzPath, uint32(rate), fitToDisk, ch) // #nosec G115 -- validated by the pipeline
	if err != nil {
		return fail(convertError(err))
	}
	img, cerr := s.imageOrNew()
	if cerr != nil {
		return fail(cerr)
	}
	snap, cerr := s.replaceDump(img, res.FZF, 0)
	if cerr != nil {
		return fail(cerr)
	}
	s.refreshDISMode()
	return SFZResult{Snapshot: snap, Rate: int(res.Rate)}, nil
}

// ImportWAVFolder converts a folder of WAVs through the CLI's zero SFZ
// pipeline (sorted names, sequential keys up the keyboard, R8) and
// places the result as the document's instrument. channel is
// ChannelLeft, ChannelRight, or ChannelMix; the UI asks once and the
// answer covers every file in the batch.
func (s *Session) ImportWAVFolder(files map[string][]byte, rate int, fitToDisk bool, channel string) (SFZResult, *Error) {
	fail := func(cerr *Error) (SFZResult, *Error) {
		return SFZResult{Snapshot: s.Snapshot()}, cerr
	}
	if rate < 0 {
		return fail(errf("invalid-rate", "rate must be positive, got %d", rate))
	}
	ch, cerr := parseChannel(channel)
	if cerr != nil {
		return fail(cerr)
	}
	res, err := sfzconvert.ConvertDirFS(context.Background(), mapFS(files), uint32(rate), fitToDisk, ch) // #nosec G115 -- validated by the pipeline
	if err != nil {
		return fail(convertError(err))
	}
	img, cerr := s.imageOrNew()
	if cerr != nil {
		return fail(cerr)
	}
	snap, cerr := s.replaceDump(img, res.FZF, 0)
	if cerr != nil {
		return fail(cerr)
	}
	s.refreshDISMode()
	return SFZResult{Snapshot: snap, Rate: int(res.Rate)}, nil
}

// placeSplitResult writes an already split build onto the document as
// an image pair, with the split's DIS tail counts on both disks.
// Disk 1 of a split holds the maximum payload one disk carries, so
// nothing else fits beside it: it is rebuilt fresh (real hardware
// images can have odd sector maps that run one sector short), and the
// placement refuses rather than silently drop the disk's other files
// (R10).
func (s *Session) placeSplitResult(img *disk.Image, result voicebuild.MultiDiskResult) (Snapshot, *Error) {
	if others := looseFileCount(img); others > 0 {
		return s.Snapshot(), errf("no-space",
			"a two disk instrument fills disk 1 completely; extract the disk's %d other files first", others)
	}
	img1, cerr := freshImage(img.Label())
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	if cerr := putDump(img1, result.Disks[0], 0, &result, 0); cerr != nil {
		return s.Snapshot(), cerr
	}
	img2, cerr := s.disk2Image()
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	if cerr := putDump(img2, result.Disks[1], 1, &result, 0); cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.adoptPair(img1, img2.Bytes())
}

// codeInvalidChannel is the boundary code for an answer that is not
// one of left, right, or mix.
const codeInvalidChannel = "invalid-channel"

// parseChannel maps the boundary's channel name onto the conversion
// pipeline's enum, for the single file path and the batch alike. A
// name the boundary doesn't know is refused rather than quietly
// standing in for one of the three.
func parseChannel(channel string) (voiceimport.Channel, *Error) {
	switch channel {
	case ChannelLeft:
		return voiceimport.ChannelLeft, nil
	case ChannelRight:
		return voiceimport.ChannelRight, nil
	case ChannelMix:
		return voiceimport.ChannelMix, nil
	default:
		return voiceimport.ChannelMonoOnly, errf(codeInvalidChannel, "channel %q is not left, right, or mix", channel)
	}
}

// mapFS wraps boundary files as a filesystem for the conversion
// pipeline. Paths are slash separated and relative, io/fs shape; the
// UI normalises dropped folder paths before the call.
func mapFS(files map[string][]byte) fs.FS {
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: data}
	}
	return fsys
}

// loneSFZ finds the folder's single .sfz file.
func loneSFZ(files map[string][]byte) (string, *Error) {
	var found []string
	for name := range files {
		if strings.HasSuffix(strings.ToLower(name), ".sfz") {
			found = append(found, name)
		}
	}
	switch len(found) {
	case 0:
		return "", errf("no-sfz", "the folder holds no .sfz file")
	case 1:
		return found[0], nil
	default:
		return "", errf(codeInvalidValue, "the folder holds %d .sfz files; name one", len(found))
	}
}

// convertError maps pipeline failures onto boundary codes. A missing
// referenced sample keeps the pipeline's message, which names it (R9).
func convertError(err error) *Error {
	if errors.Is(err, fs.ErrNotExist) {
		return errf("missing-samples", "%v", err)
	}
	var tmd *voicebuild.ErrTooManyDisks
	if errors.As(err, &tmd) {
		return errf("too-large", "%v", tmd)
	}
	var ram *voicebuild.ErrSampleRAMExceeded
	if errors.As(err, &ram) {
		return errf("ram-exceeded", "%v", ram)
	}
	return errf("invalid-sfz", "%v", err)
}
