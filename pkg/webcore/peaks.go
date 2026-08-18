package webcore

import (
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
)

// maxPeakBuckets bounds one peaks request; a 4k display column per
// pixel stays far below it, and it keeps a hostile request from
// allocating without limit.
const maxPeakBuckets = 16384

// Peaks returns interleaved min/max sample pairs for the requested
// frame window of a voice, bucketed for display (R17). The window
// clamps to the voice's frame count; buckets must be positive. The
// PCM decode is the same code path as the CLI's fzv extract.
func (s *Session) Peaks(fileName string, startFrame, endFrame, buckets int) ([]int16, *Error) {
	if s.image == nil {
		return nil, errf(codeNoDisk, "no disk is open")
	}
	if buckets <= 0 || buckets > maxPeakBuckets {
		return nil, errf(codeInvalidValue, "buckets must be 1 to %d, got %d", maxPeakBuckets, buckets)
	}
	img, ierr := s.openedImage()
	if ierr != nil {
		return nil, ierr
	}
	voiceBytes, gerr := diskget.FromImage(img, fileName)
	if gerr != nil {
		return nil, errf(codeNotFound, "%v", gerr)
	}
	_, samples, derr := voiceextract.Decode(voiceBytes)
	if derr != nil {
		return nil, errf("not-a-voice", "%v", derr)
	}

	return bucketPeaks(samples, startFrame, endFrame, buckets), nil
}

// SlotPeaks returns interleaved min/max sample pairs for a frame
// window of an instrument voice slot, the slot-addressed sibling of
// Peaks (R17).
func (s *Session) SlotPeaks(slot, startFrame, endFrame, buckets int) ([]int16, *Error) {
	if buckets <= 0 || buckets > maxPeakBuckets {
		return nil, errf(codeInvalidValue, "buckets must be 1 to %d, got %d", maxPeakBuckets, buckets)
	}
	fzv, cerr := s.slotFZV(slot)
	if cerr != nil {
		return nil, cerr
	}
	_, samples, derr := voiceextract.Decode(fzv)
	if derr != nil {
		return nil, errf("not-a-voice", "%v", derr)
	}
	return bucketPeaks(samples, startFrame, endFrame, buckets), nil
}

// bucketPeaks reduces a frame window to interleaved min/max pairs;
// shared by the file and slot peak paths.
func bucketPeaks(samples []int16, startFrame, endFrame, buckets int) []int16 {
	if startFrame < 0 {
		startFrame = 0
	}
	if endFrame > len(samples) {
		endFrame = len(samples)
	}
	if startFrame >= endFrame {
		return make([]int16, 2*buckets)
	}
	window := samples[startFrame:endFrame]

	out := make([]int16, 0, 2*buckets)
	for b := 0; b < buckets; b++ {
		lo := b * len(window) / buckets
		hi := (b + 1) * len(window) / buckets
		if hi <= lo {
			hi = lo + 1
		}
		if hi > len(window) {
			hi = len(window)
		}
		if lo >= len(window) {
			out = append(out, 0, 0)
			continue
		}
		minV, maxV := window[lo], window[lo]
		for _, v := range window[lo:hi] {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
		out = append(out, minV, maxV)
	}
	return out
}
