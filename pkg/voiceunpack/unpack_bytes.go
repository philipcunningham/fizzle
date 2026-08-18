package voiceunpack

import (
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/fzutil"
)

// UnpackDataFromBytes is the in-memory twin of UnpackData. It takes a raw FZF
// byte slice and returns one FZV byte slice per voice plus the parallel
// file-level slot indices.
//
// Audition paths use it to render the voice being edited without a disk round
// trip: edits land in model.(*Model).bytes, those bytes unpack here, and the
// resulting FZV goes to voiceextract.ExtractPlayback.
//
// The address rewrite matches UnpackData's (see subtractSampleOffsets):
// wavst/waved/genst/gened become relative to the extracted voice's audio
// bytes rather than the combined wave area's.
func UnpackDataFromBytes(data []byte) ([][]byte, []int, error) {
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		return nil, nil, fmt.Errorf("voiceunpack: %w", err)
	}
	return unpack(data, hdr)
}
