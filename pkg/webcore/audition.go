package webcore

import (
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

// Audition carries one voice's PCM for the preview path (R20 to R22):
// the declared sample rate, the root key the pitch maths centres on,
// and the decoded samples. The PCM crosses the boundary as a
// transferable; it stays out of JSON.
type Audition struct {
	SampleRate int     `json:"sampleRate"`
	Root       int     `json:"root"`
	PCM        []int16 `json:"-"`
}

func auditionFromFZV(fzv []byte) (*Audition, *Error) {
	rate, samples, err := voiceextract.Decode(fzv)
	if err != nil {
		return nil, errf("not-a-voice", "%v", err)
	}
	root := 60
	if vp, err := fzvinfo.ParseBytes(fzv); err == nil {
		root = int(vp.KeyCentre)
	}
	return &Audition{SampleRate: int(rate), Root: root, PCM: samples}, nil
}

// AuditionPCM decodes a voice file's audio for preview, the same code
// path as the CLI's fzv extract.
func (s *Session) AuditionPCM(fileName string) (*Audition, *Error) {
	img, cerr := s.openedImage()
	if cerr != nil {
		return nil, cerr
	}
	fzv, gerr := diskfs.Extract(img, fileName)
	if gerr != nil {
		return nil, errf(codeNotFound, "%v", gerr)
	}
	return auditionFromFZV(fzv)
}

// AuditionSlot decodes an instrument voice slot's audio for preview,
// through the same unpack path as the CLI's fzf unpack.
func (s *Session) AuditionSlot(slot int) (*Audition, *Error) {
	fzv, cerr := s.slotFZV(slot)
	if cerr != nil {
		return nil, cerr
	}
	return auditionFromFZV(fzv)
}

// slotFZV extracts one instrument voice slot as a standalone .fzv
// (header rewritten as if the voice had always been alone), through
// the same unpack path as the CLI's fzf unpack. Shared by audition,
// slot peaks, and slot extract.
func (s *Session) slotFZV(slot int) ([]byte, *Error) {
	if s.image == nil {
		return nil, errf(codeNoDisk, "no disk is open")
	}
	if s.instrument == nil {
		return nil, errf("no-instrument", "the disk has no full dump")
	}
	img, ierr := s.openedImage()
	if ierr != nil {
		return nil, ierr
	}
	fzf, cerr := s.stitchedDump(img)
	if cerr != nil {
		return nil, cerr
	}
	var voices [][]byte
	var slots []int
	var uerr error
	vn := 0
	if s.usesDIS() {
		vn = disVoiceCount(img)
	}
	if vn > 0 {
		voices, slots, uerr = voiceunpack.UnpackDataFromBytesWithVoiceCount(fzf, vn)
	} else {
		voices, slots, uerr = voiceunpack.UnpackDataFromBytes(fzf)
	}
	if uerr != nil {
		return nil, errf("invalid-image", "%v", uerr)
	}
	for i, fzvSlot := range slots {
		if fzvSlot == slot {
			return voices[i], nil
		}
	}
	return nil, errf(codeInvalidValue, "voice slot %d not found in the instrument", slot)
}
