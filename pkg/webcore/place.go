package webcore

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/philipcunningham/fizzle/pkg/container"
	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
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
			return addBankPatches(d, data, slot)
		})
	}
	img, cerr := s.imageOrNew()
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	if err := diskadd.AddToImage(img, data, 0); err != nil {
		return s.Snapshot(), addError(err)
	}
	return s.adoptPair(img, s.image2, modeDerive)
}

// instrumentOwnedRanges names the bytes of bank sector 0 that belong to
// the instrument rather than to the bank that shares the sector, each
// as an offset and a length. The effect block is R19's whole surface
// (pitch bend range plus all 21 controller modulation cells) and the
// firmware mirrors it to and from its live RAM copy, so it is the
// instrument's; the total wave marker says how much audio the
// instrument spans across a two disk set, which no single bank knows.
// A .fzb carries a bank's mapping and has a claim on neither, so
// dropping one on slot 0 keeps what it found there.
var instrumentOwnedRanges = [][2]int{
	{disk.BankTotalWaveOffset, 4},
	{disk.BankEffectOffset, disk.EffectDataSize},
}

// keepInstrumentFields copies the instrument's own fields out of the
// sector being replaced and into the arriving one.
func keepInstrumentFields(sector, replaced []byte) {
	for _, field := range instrumentOwnedRanges {
		off, size := field[0], field[1]
		if off+size <= len(sector) && off+size <= len(replaced) {
			copy(sector[off:off+size], replaced[off:off+size])
		}
	}
}

func addBankPatches(d *dumpState, fzb []byte, slot int) ([]model.Patch, *Error) {
	if slot < 0 || slot >= disk.MaxBanks {
		return nil, errf(codeInvalidValue, "bank slot must be 0 to %d, got %d", disk.MaxBanks-1, slot)
	}
	if slot > d.header.NBankSectors {
		return nil, errf(codeInvalidValue, "bank slot %d skips past the %d existing banks", slot, d.header.NBankSectors)
	}
	// The incoming sector brings its own areas, and the summed bstep
	// values are what bound a reader's walk of the voice area. A bank
	// that carries more areas than the one it replaces therefore needs a
	// voice slot per extra area, or the walk runs past the last slot
	// into the audio; a bank that carries fewer gives those slots back,
	// or the walk stops short of the last one and the audio moves up
	// under every voice.
	incoming := int(binary.LittleEndian.Uint16(fzb[disk.BankVoiceCountOffset:]))
	replaced := 0
	if slot < d.header.NBankSectors {
		replaced = bankBstep(d.fzf, slot)
	}
	// An area that plays a slot this instrument does not have is a
	// broken area, so a bank lifted from a larger instrument is refused
	// rather than spliced in half working. In DIS mode the voice area
	// never resizes with bsteps, so the count itself is the bound.
	slots := d.header.NVoice + incoming - replaced
	if d.disVN > 0 {
		slots = d.header.NVoice
	}
	for a := range incoming {
		vp := int(binary.LittleEndian.Uint16(fzb[disk.BankVoiceNumOffset+a*disk.VPEntrySize:]))
		if vp >= slots {
			return nil, errf(codeInvalidValue,
				"the bank's area %d plays voice slot %d, and this instrument holds %d slots",
				a+1, vp, slots)
		}
	}
	if slot == d.header.NBankSectors {
		grown, growBytes := container.GrowBanks(d.fzf, d.header.NBankSectors, slot+1)
		d.fzf = grown
		// The banks took a sector, so everything past them moved with it.
		d.header.NBankSectors = slot + 1
		d.header.VoiceAreaStart += growBytes
		d.audioStart += growBytes
	}
	off := slot * disk.SectorSize
	old := make([]byte, disk.SectorSize)
	copy(old, d.fzf[off:off+disk.SectorSize])
	sector := make([]byte, disk.SectorSize)
	copy(sector, fzb[:disk.SectorSize])
	if slot == 0 {
		keepInstrumentFields(sector, old)
	}
	// The new mapping goes in first, so the voice area is brought in
	// step against the areas that end up playing rather than the ones
	// the replaced bank held.
	patch := []model.Patch{{Offset: off, Old: old, New: sector}}
	if err := model.Apply(d.fzf, patch); err != nil {
		return nil, errf("patch-failed", "%v", err)
	}
	if cerr := ensureVoiceSlots(d, 0, noFreedSlot); cerr != nil {
		return nil, cerr
	}
	return nil, nil
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
