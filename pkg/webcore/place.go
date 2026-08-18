package webcore

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/philipcunningham/fizzle/pkg/container"
	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/model"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
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
	return s.replaceDump(img, data)
}

// AddVoice places an .fzv per the placement matrix (R7): with no disk
// open it becomes a one voice instrument on a fresh disk; with no
// instrument it becomes a one voice instrument on the open disk;
// otherwise it joins the instrument's voice list. The join lands with
// a fresh area carrying the voice's own key range: the FZ format sizes
// a dump's voice area from bank references, so a voice no bank
// references would vanish on the next parse. Membership is reference.
func (s *Session) AddVoice(fzvData []byte) (Snapshot, *Error) {
	vp, err := fzvinfo.ParseBytes(fzvData)
	if err != nil {
		return s.Snapshot(), errf("not-a-voice", "%v", err)
	}
	if s.image != nil && s.instrument != nil {
		return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
			newSlot, cerr := appendVoiceToDump(d, fzvData)
			if cerr != nil {
				return nil, cerr
			}
			// An empty instrument's placeholder area already references
			// the filled slot. Joining again would double-map it, so the
			// existing area is retuned to the voice it now holds instead.
			if sites := fzutil.FindBankSitesForVoice(d.fzf, d.header, newSlot); len(sites) > 0 {
				return retuneAreaPatches(d, sites[0], newSlot, vp.KeyLow, vp.KeyHigh, vp.KeyCentre), nil
			}
			return joinAreaPatches(d, newSlot, vp.KeyLow, vp.KeyHigh, vp.KeyCentre)
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
	// Through replaceDump rather than diskadd directly: a first voice
	// too large for one disk then splits across a pair, the same way
	// a join or an SFZ conversion does.
	return s.replaceDump(img, fzf)
}

// ImportWAVToInstrument converts a WAV through the CLI's importer and
// places the voice with AddVoice's matrix behaviour: the instrument
// grows by one voice (the slice 5 deferral), or comes into being when
// absent.
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
	return s.adopt(img)
}

// noSkipArea marks a key-range choice with no area of its own to
// discount: only a retune has one, because an area is not an obstacle
// to itself.
const noSkipArea = -1

// carriesOwnKeyRange reports whether a voice header says where on the
// keyboard it belongs. The WAV importer writes one fixed range into
// every voice it produces (disk.DefaultKeyLow to disk.DefaultKeyHigh),
// so that exact pair is the one value that carries no opinion; any
// other range is one an .fzv from a real instrument brought with it.
func carriesOwnKeyRange(keyLow, keyHigh uint8) bool {
	return keyLow != disk.DefaultKeyLow || keyHigh != disk.DefaultKeyHigh
}

// nextFreeKey returns the first MIDI note the bank's areas leave free,
// counting from disk.FirstMIDINote (C2, where the core's own folder
// import starts). skipArea is the area being retuned, or noSkipArea.
// Where the bank already reaches the top of the keyboard there is no
// free key left, and the join lands on the top note rather than
// refusing: a voice that arrives has to land somewhere, and the user
// can move it.
func nextFreeKey(d *dumpState, bank, skipArea int) uint8 {
	base := bank * disk.SectorSize
	next := disk.FirstMIDINote
	for i := 0; i < min(bankBstep(d.fzf, bank), disk.MaxVoices); i++ {
		off := base + disk.BankKeyHighOffset + i
		if i == skipArea || off >= len(d.fzf) {
			continue
		}
		if high := int(d.fzf[off]); high >= next {
			next = high + 1
		}
	}
	return clampByte(next, disk.MaxMIDINote)
}

// joinKeyRange decides where a joining voice lands on the keyboard.
//
// The rule: a voice that carries a key range of its own keeps it, and a
// voice that carries the WAV importer's fixed default does not. Every
// voice that importer produces reads 36 to 96 root 60, so honouring the
// incoming header stacked eight dropped WAVs on the same two octaves
// and one key sounded all eight. R7's .wav and .fzv cells promise
// sequential mapping, and J5 promises the next free key range.
//
// A voice with no range of its own takes one key, the first the bank
// leaves free, with the area's root on that key so the samples sound at
// the pitch they were recorded at. That is the convention the core's
// own folder import already uses (sfzconvert.sequentialRegions keys
// each WAV to one ascending note from disk.FirstMIDINote with
// pitch_keycenter on it), so dropping a folder and dropping its files
// one at a time land on the same notes.
func joinKeyRange(d *dumpState, bank, skipArea int, keyLow, keyHigh, keyCentre uint8) (uint8, uint8, uint8) {
	if carriesOwnKeyRange(keyLow, keyHigh) {
		return keyLow, keyHigh, keyCentre
	}
	key := nextFreeKey(d, bank, skipArea)
	return key, key, key
}

// voiceHeaderKeyPatches mirrors an area's key range into the voice
// slot's own header. The CLI builder writes both from one keygroup, and
// fizzle reads the header back: voiceRootKey pitches a later area from
// it, and unpacking the slot as an .fzv carries it out. Leaving the two
// to disagree would give the same voice one range on hardware and
// another everywhere fizzle looks.
func voiceHeaderKeyPatches(d *dumpState, slot int, keyLow, keyHigh, keyCentre uint8) []model.Patch {
	off := disk.VoiceSlotOffset(d.header.VoiceAreaStart, slot)
	if off+disk.VoiceHeaderUsed > len(d.fzf) {
		return nil
	}
	return []model.Patch{
		bytePatch(d.fzf, off+disk.VoiceKeyLowOffset, keyLow),
		bytePatch(d.fzf, off+disk.VoiceKeyHighOffset, keyHigh),
		bytePatch(d.fzf, off+disk.VoiceKeyCentOffset, keyCentre),
	}
}

// retuneAreaPatches gives an existing area the range the voice that
// just landed in the slot it references should play on. The empty
// instrument's placeholder area spans the whole keyboard; once a real
// voice fills the slot, the area speaks the voice's own range, or the
// first free key where the voice brought no range of its own.
func retuneAreaPatches(d *dumpState, site fzutil.BankSite, slot int, keyLow, keyHigh, keyCentre uint8) []model.Patch {
	keyLow, keyHigh, keyCentre = joinKeyRange(d, site.BankIdx, site.SplitIdx, keyLow, keyHigh, keyCentre)
	base := site.BankIdx * disk.SectorSize
	i := site.SplitIdx
	header := voiceHeaderKeyPatches(d, slot, keyLow, keyHigh, keyCentre)
	patches := make([]model.Patch, 0, 3+len(header))
	patches = append(patches,
		bytePatch(d.fzf, base+disk.BankKeyLowOffset+i, keyLow),
		bytePatch(d.fzf, base+disk.BankKeyHighOffset+i, keyHigh),
		bytePatch(d.fzf, base+disk.BankKeyCentOffset+i, keyCentre),
	)
	return append(patches, header...)
}

// joinAreaPatches maps a freshly appended voice slot into the first
// bank with room: vp entry, the key range joinKeyRange chose, an
// audible velocity range, and the bstep bump. Without this reference
// the slot would sit past every parser's voice walk. The slot itself
// already exists, appendVoiceToDump having grown the voice area for it,
// so this raises the bstep with a slot to match.
func joinAreaPatches(d *dumpState, newSlot int, keyLow, keyHigh, keyCentre uint8) ([]model.Patch, *Error) {
	bank := -1
	for b := 0; b < d.header.NBankSectors; b++ {
		if bankBstep(d.fzf, b) < disk.MaxVoices {
			bank = b
			break
		}
	}
	if bank < 0 {
		return nil, errf("bank-full", "every bank already holds %d areas", disk.MaxVoices)
	}
	keyLow, keyHigh, keyCentre = joinKeyRange(d, bank, noSkipArea, keyLow, keyHigh, keyCentre)
	bstep := bankBstep(d.fzf, bank)
	base := bank * disk.SectorSize

	vpOff := base + disk.BankVoiceNumOffset + bstep*disk.VPEntrySize
	oldVP := make([]byte, disk.VPEntrySize)
	copy(oldVP, d.fzf[vpOff:vpOff+disk.VPEntrySize])
	newVP := make([]byte, disk.VPEntrySize)
	binary.LittleEndian.PutUint16(newVP, uint16(newSlot)) // #nosec G115 -- bounded by disk.MaxVoices

	patches := []model.Patch{
		{Offset: vpOff, Old: oldVP, New: newVP},
		bytePatch(d.fzf, base+disk.BankKeyLowOffset+bstep, keyLow),
		bytePatch(d.fzf, base+disk.BankKeyHighOffset+bstep, keyHigh),
		bytePatch(d.fzf, base+disk.BankKeyCentOffset+bstep, keyCentre),
		bytePatch(d.fzf, base+disk.BankVelLowOffset+bstep, 1),
		bytePatch(d.fzf, base+disk.BankVelHighOffset+bstep, 127),
		// gchn: every generator, the CLI builder's default. A zero byte
		// routes the area to no output, which the sampler plays silently.
		bytePatch(d.fzf, base+disk.BankAudioOutOffset+bstep, disk.PolyphonicAudioOut),
	}
	patches = append(patches, voiceHeaderKeyPatches(d, newSlot, keyLow, keyHigh, keyCentre)...)
	if bump, ok := container.BankBstepBumpPatch(d.fzf, bank, bstep); ok {
		patches = append(patches, bump)
	}
	return patches, nil
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
	// rather than spliced in half working.
	slots := d.header.NVoice + incoming - replaced
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
	if err := applyModelPatches(d.fzf, patch); err != nil {
		return nil, errf("patch-failed", "%v", err)
	}
	if cerr := ensureVoiceSlots(d, 0, noFreedSlot); cerr != nil {
		return nil, cerr
	}
	return nil, nil
}

// appendVoiceToDump appends an .fzv (header sector plus PCM) to a full
// dump as a fresh voice slot, matching the retired studio TUI's pool
// assign byte for byte: grow the voice area at the boundary when the slot
// needs it, land the PCM at the end of the audio area, and rewrite the
// header's wave pointers to absolute sample offsets. An empty
// instrument's silent placeholder slot is filled rather than appended
// past. Returns the grown dump and the slot the voice landed in.
func appendVoiceToDump(d *dumpState, fzv []byte) (int, *Error) {
	if len(fzv) < disk.SectorSize {
		return 0, errf("invalid-fzv", "voice file is %d bytes, shorter than one sector", len(fzv))
	}
	pcm := fzv[disk.SectorSize:]
	if len(pcm)%disk.BytesPerSample != 0 {
		return 0, errf("invalid-fzv", "voice PCM is misaligned")
	}

	// An empty instrument's silent placeholder is filled rather than
	// appended past, so the join costs no slot and moves no bstep.
	newSlot := 0
	if !placeholderSlot(d) {
		var cerr *Error
		if newSlot, cerr = allocVoiceSlot(d); cerr != nil {
			return 0, cerr
		}
	}

	// The PCM lands at the end of the audio area, so the header's wave
	// pointers are rewritten to absolute sample offsets there.
	audioUsedBytes := len(d.fzf) - d.audioStart
	header := make([]byte, disk.VoicePackSize)
	copy(header, fzv[:disk.VoicePackSize])
	container.RewriteWavePointers(header, bitconv.NarrowU32(audioUsedBytes/disk.BytesPerSample))

	d.fzf = append(d.fzf, pcm...)
	copy(d.fzf[disk.VoiceSlotOffset(d.header.VoiceAreaStart, newSlot):], header)
	return newSlot, nil
}

// placeholderSlot reports whether the dump is an empty instrument: one
// voice slot holding the silent placeholder (loop mode NoSound, no
// audio of its own) and an empty audio area. The first joined voice
// fills that slot instead of appending past it.
func placeholderSlot(d *dumpState) bool {
	return d.header.NVoice == 1 && len(d.fzf) == d.audioStart && slotIsPlaceholder(d, 0)
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
		return nil, errf("invalid-wav", "%v", err)
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
