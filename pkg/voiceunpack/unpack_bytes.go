package voiceunpack

import (
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/fzutil"
)

// UnpackDataFromBytes is the in-memory twin of UnpackData. It accepts a raw
// FZF byte slice (already loaded into memory by the caller) and returns
// one FZV byte slice per voice plus the parallel file-level slot indices.
//
// Studio v2's audition path uses this to render the currently-edited voice
// to FZV without round-tripping through disk: edits land in
// model.(*Model).bytes, the bytes are unpacked here, the resulting FZV is
// handed to voiceextract.ExtractPlayback, and the WAV is auditioned.
//
// The address-rewrite transform applied here matches what UnpackData does
// for files on disk (see subtractSampleOffsets): wavst/waved/genst/gened
// are rewritten to be relative to the extracted voice's audio bytes rather
// than the combined wave area's.
func UnpackDataFromBytes(data []byte) ([][]byte, []int, error) {
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		return nil, nil, fmt.Errorf("voiceunpack: %w", err)
	}
	return unpack(data, hdr)
}
