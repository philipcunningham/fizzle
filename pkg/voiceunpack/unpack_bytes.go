package voiceunpack

import (
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzf"
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
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		return nil, nil, fmt.Errorf("voiceunpack: %w", err)
	}
	return unpack(doc.Bytes(), doc.Layout())
}

// UnpackDataFromBytesWithVoiceCount is UnpackDataFromBytes with a
// known voice count.
func UnpackDataFromBytesWithVoiceCount(data []byte, vn int) ([][]byte, []int, error) {
	hdr, err := fzutil.ParseFZFHeaderWithVoiceCount(data, vn)
	if err != nil {
		return nil, nil, fmt.Errorf("voiceunpack: %w", err)
	}
	audioStart := hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize
	return unpackAt(data, hdr.NVoice, hdr.VoiceAreaStart, audioStart)
}
