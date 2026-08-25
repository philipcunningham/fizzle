package webcore

import (
	"bytes"
	"errors"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	fzfmodel "github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/model"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// defaultLabel names disks the placement matrix creates implicitly
// (dropping material with no disk open). It matches the UI's default.
const defaultLabel = "FIZZLE"

// LoadFZF places a full dump per the placement matrix (R7): with no
// disk open it becomes the instrument of a fresh disk; otherwise it
// replaces the open disk's instrument. A dump too large for one disk
// splits into a two disk document (R25). The UI owns the replace
// prompt; the core just places.
func (s *Session) LoadFZF(data []byte) (Snapshot, *Error) {
	if _, err := fzutil.ParseFZFHeader(data); err != nil {
		return s.Snapshot(), errf("invalid-fzf", "%v", err)
	}
	img, cerr := s.imageOrNew()
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	// A stamped export reloads under its carried count; the DIS the
	// write-back produces is what the adopted mode derives from.
	vn := 0
	if hdr, src, err := fzutil.ResolveStandaloneFZF(data); err == nil && src == fzutil.VoiceCountMarker {
		vn = hdr.NVoice
	}
	return s.replaceDump(img, data, vn, modeDerive)
}

// AddVoice places an .fzv per the placement matrix (R7): with no disk
// open it becomes a one voice instrument on a fresh disk; with no
// instrument it becomes a one voice instrument on the open disk;
// otherwise it joins the instrument's voice list. The join lands with
// a fresh area carrying the voice's own key range: the FZ format sizes
// a dump's voice area from bank references, so a voice no bank
// references would vanish on the next parse.
func (s *Session) AddVoice(fzvData []byte) (Snapshot, *Error) {
	vp, err := fzvinfo.ParseBytes(fzvData)
	if err != nil {
		return s.Snapshot(), errf("not-a-voice", "%v", err)
	}
	if s.image != nil && s.instrument != nil {
		return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
			result, err := d.doc.AddVoice(fzvData)
			if err != nil {
				return nil, addVoiceDocumentError(err)
			}
			return nil, applyDocumentOperation(d, result)
		})
	}
	// No instrument yet: assemble one from this voice, keyed by the
	// voice's own range, through the CLI's builder.
	fzf, berr := voicebuild.AssembleWithKeygroups(
		[][]byte{fzvData},
		[]voicebuild.Keygroup{voicebuild.NewKeygroup(vp.KeyLow, vp.KeyHigh, vp.KeyCentre)},
	)
	if berr != nil {
		return s.Snapshot(), errf("invalid-fzv", "%v", berr)
	}
	img, cerr := s.imageOrNew()
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	// A dump this session could not parse still belongs to the user:
	// replaceDump would overwrite it, so an unreadable instrument is
	// refused rather than silently replaced.
	if hasFile(img, disk.FullDumpName) {
		return s.Snapshot(), errItemf("unreadable-instrument", disk.FullDumpName,
			"this disk's instrument cannot be read, so fizzle will not replace it; delete it first to import over it")
	}
	// Through replaceDump rather than diskadd directly: a first voice
	// too large for one disk then splits across a pair, the same way
	// a join or an SFZ conversion does.
	return s.replaceDump(img, fzf, 0, modeDerive)
}

func addVoiceDocumentError(err error) *Error {
	switch {
	case errors.Is(err, fzfmodel.ErrVoiceFileTooShort):
		return errf("invalid-fzv", "voice file is shorter than one sector")
	case errors.Is(err, fzfmodel.ErrVoicePCMMisaligned):
		return errf("invalid-fzv", "voice PCM is misaligned")
	case errors.Is(err, fzfmodel.ErrAllBanksFull):
		return errf("bank-full", "every bank already holds %d areas", disk.MaxVoices)
	default:
		return voiceAreaBoundaryError(err)
	}
}

// ImportWAVToInstrument converts a WAV through the CLI's importer and
// places the voice with AddVoice's matrix behaviour: the instrument
// grows by one voice, or comes into being when absent.
func (s *Session) ImportWAVToInstrument(filename string, wavData []byte, rate uint32, channel string) (Snapshot, *Error) {
	voice, cerr := convertWAV(filename, wavData, rate, channel)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.AddVoice(voice)
}

// AddBank places a .fzb per the placement matrix (R7). With an
// instrument open, the dump's bank sector lands at bank index slot
// (replacing that bank's mapping, or appending when slot is the first
// unused index); voice slots keep their meaning by index, the hardware
// bank load semantics. Without an instrument the .fzb itself becomes
// the instrument: bank plus voice headers, audio to be imported later.
func (s *Session) AddBank(data []byte, slot int) (Snapshot, *Error) {
	if len(data) < disk.SectorSize {
		return s.Snapshot(), errf("invalid-fzb", "a bank dump is at least one %d byte sector", disk.SectorSize)
	}
	if _, err := fzutil.ParseFZFHeader(data); err != nil {
		return s.Snapshot(), errf("invalid-fzb", "%v", err)
	}
	if s.image != nil && s.instrument != nil {
		return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
			return addBankDocumentOperation(d, data, slot)
		})
	}
	img, cerr := s.imageOrNew()
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	file, err := diskfs.FullDump(data, 0, 0)
	if err != nil {
		return s.Snapshot(), addError(err)
	}
	if err := diskfs.Add(img, data, file); err != nil {
		return s.Snapshot(), addError(err)
	}
	return s.adoptPair(img, s.image2, modeDerive)
}

func addBankDocumentOperation(d *dumpState, data []byte, slot int) ([]model.Patch, *Error) {
	result, err := d.doc.AddBank(data, slot)
	if err != nil {
		return nil, addBankDocumentError(err, slot, d.doc.Layout().BankCount())
	}
	return nil, applyDocumentOperation(d, result)
}

func addBankDocumentError(err error, slot, bankCount int) *Error {
	if errors.Is(err, fzfmodel.ErrBankIndexOutOfRange) {
		if slot < 0 || slot >= disk.MaxBanks {
			return errf(codeInvalidValue, "bank slot must be 0 to %d, got %d", disk.MaxBanks-1, slot)
		}
		return errf(codeInvalidValue, "bank slot %d skips past the %d existing banks", slot, bankCount)
	}
	var areaErr *fzfmodel.AreaVoiceError
	if errors.As(err, &areaErr) {
		return errf(codeInvalidValue,
			"the bank's area %d plays voice slot %d, and this instrument holds %d slots",
			areaErr.Area+1, areaErr.Voice, areaErr.VoiceCount)
	}
	return voiceAreaBoundaryError(err)
}

// wavRefusal turns a WAV read failure into words a musician reads:
// the file that failed, what fizzle wanted, and the way out where one
// exists. The Go chain rides along in the envelope's detail for a bug
// report, never in the message (the spec's contract section).
func wavRefusal(filename string, err error) *Error {
	say := func(format string, args ...any) *Error {
		e := errItemf("invalid-wav", filename, format, args...)
		e.Detail = err.Error()
		return e
	}
	switch {
	case errors.Is(err, wav.ErrNotRIFF), errors.Is(err, wav.ErrNotWAVE),
		errors.Is(err, wav.ErrMissingFmt), errors.Is(err, wav.ErrDataBeforeFmt):
		return say("%s is not a WAV file fizzle can read; export it as a WAV and try again", filename)
	case errors.Is(err, wav.ErrMissingData), errors.Is(err, wav.ErrNoSamples):
		return say("%s holds no audio", filename)
	case errors.Is(err, wav.ErrUnsupportedPCM), errors.Is(err, wav.ErrBitDepth):
		return say("%s is not 16 bit PCM; export it as a 16 bit WAV and try again", filename)
	case errors.Is(err, wav.ErrChannelCount):
		return say("%s carries more than two channels; export it as mono or stereo", filename)
	case errors.Is(err, wav.ErrSampleRate):
		return say("%s declares a sample rate fizzle cannot read", filename)
	case errors.Is(err, wav.ErrTooManySamples), errors.Is(err, wav.ErrDataTooLarge),
		errors.Is(err, fzutil.ErrTooLong):
		return say("%s is longer than the sampler's memory holds at this rate; choose a lower rate", filename)
	case errors.Is(err, fzutil.ErrSourceRateTooLow):
		return say("%s was recorded at too low a sample rate to convert", filename)
	default:
		return say("%s could not be read as a WAV", filename)
	}
}

// convertWAV maps the boundary channel name and runs the CLI's WAV to
// voice conversion; shared by ImportWAV and ImportWAVToInstrument.
func convertWAV(filename string, wavData []byte, rate uint32, channel string) ([]byte, *Error) {
	if err := disk.ValidateRate(rate); err != nil {
		return nil, errf("invalid-rate", "%v", err)
	}
	ch, cerr := parseChannel(channel)
	if cerr != nil {
		return nil, cerr
	}
	voice, err := voiceimport.ImportBytes(wavData, fzutil.VoiceName(filename), rate, ch)
	if err != nil {
		return nil, wavRefusal(filename, err)
	}
	return voice, nil
}

// imageOrNew parses the open image, or formats a fresh one when no
// disk is open: the matrix's implicit new disk column.
func (s *Session) imageOrNew() (*disk.Image, *Error) {
	if s.image == nil {
		data, err := diskformat.BuildImage(defaultLabel)
		if err != nil {
			return nil, errf("invalid-label", "%v", err)
		}
		img, rerr := disk.ReadImage(bytes.NewReader(data))
		if rerr != nil {
			return nil, errf("invalid-image", "%v", rerr)
		}
		return img, nil
	}
	img, rerr := disk.ReadImage(bytes.NewReader(s.image))
	if rerr != nil {
		return nil, errf("invalid-image", "not a readable FZ image: %v", rerr)
	}
	return img, nil
}

// hasFile reports whether the image's directory holds name.
func hasFile(img *disk.Image, name string) bool {
	entries, err := img.Directory()
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.NameString() == name {
			return true
		}
	}
	return false
}

// addError maps diskadd failures onto the boundary's stable codes.
func addError(err error) *Error {
	switch {
	case errors.Is(err, disk.ErrNoSpace):
		return errf("no-space", "%v", err)
	case errors.Is(err, disk.ErrDirFull):
		return errf("dir-full", "%v", err)
	default:
		return errf("add-failed", "%v", err)
	}
}
