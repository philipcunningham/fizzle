package webcore

import (
	"bytes"
	"encoding/binary"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// Document-level operations the mockup UI exposes: disk rename, file
// delete, per-file and per-slot extraction, and the empty instrument.

// Close discards the open document and its history: back to the
// start state, nothing written (N3: durable output is the exported
// file, and the UI confirms unexported changes first).
func (s *Session) Close() (Snapshot, *Error) {
	revision := s.revision
	*s = Session{revision: revision + 1}
	return s.Snapshot(), nil
}

// RenameDisk sets the open disk's 12-character printable ASCII label.
// On a split pair, disk 1 carries the document's name; disk 2 keeps
// the derived label it was formatted with.
func (s *Session) RenameDisk(label string) (Snapshot, *Error) {
	if s.image == nil {
		return s.Snapshot(), errf(codeNoDisk, "no disk is open")
	}
	if len(label) == 0 || len(label) > disk.LabelSize {
		return s.Snapshot(), errf(codeInvalidValue, "disk label must be 1 to %d characters", disk.LabelSize)
	}
	for _, r := range label {
		if r < disk.PrintableASCIIMin || r > disk.PrintableASCIIMax {
			return s.Snapshot(), errf(codeInvalidValue, "disk label contains non-ASCII character %q", string(r))
		}
	}
	img, rerr := disk.ReadImage(bytes.NewReader(s.image))
	if rerr != nil {
		return s.Snapshot(), errf("invalid-image", "not a readable FZ image: %v", rerr)
	}
	img.SetLabel(label)
	return s.adopt(img)
}

// DeleteFile removes a directory file from the disk. Deleting the
// full dump deletes the instrument; on a split pair the second disk
// goes with it.
func (s *Session) DeleteFile(name string) (Snapshot, *Error) {
	if s.image == nil {
		return s.Snapshot(), errf(codeNoDisk, "no disk is open")
	}
	img, rerr := disk.ReadImage(bytes.NewReader(s.image))
	if rerr != nil {
		return s.Snapshot(), errf("invalid-image", "not a readable FZ image: %v", rerr)
	}
	if err := img.RemoveFile(name); err != nil {
		return s.Snapshot(), errf(codeNotFound, "%v", err)
	}
	if name == disk.FullDumpName {
		return s.adoptPair(img, nil)
	}
	return s.adopt(img)
}

// ExtractFile returns a copy of a directory file's bytes for saving.
// The full dump of a split pair comes back stitched, the whole
// instrument in one file.
func (s *Session) ExtractFile(name string) ([]byte, *Error) {
	if s.image == nil {
		return nil, errf(codeNoDisk, "no disk is open")
	}
	img, rerr := disk.ReadImage(bytes.NewReader(s.image))
	if rerr != nil {
		return nil, errf("invalid-image", "not a readable FZ image: %v", rerr)
	}
	if name == disk.FullDumpName && s.image2 != nil {
		return s.stitchedDump(img)
	}
	data, gerr := diskget.FromImage(img, name)
	if gerr != nil {
		return nil, errf(codeNotFound, "%v", gerr)
	}
	return data, nil
}

// Extract formats for ExtractVoiceSlot.
const (
	ExtractFZV = "fzv"
	ExtractWAV = "wav"
)

// ExtractVoiceSlot returns one instrument voice as a standalone file:
// the unpacked .fzv (the CLI's fzf unpack output), or a 16-bit mono
// WAV at the voice's declared rate (the CLI's fzv extract output).
// The returned name is the voice's, for the download filename.
func (s *Session) ExtractVoiceSlot(slot int, format string) ([]byte, string, *Error) {
	fzv, cerr := s.slotFZV(slot)
	if cerr != nil {
		return nil, "", cerr
	}
	name := "VOICE"
	if vp, err := fzvinfo.ParseBytes(fzv); err == nil && vp.Name != "" {
		name = vp.Name
	}
	switch format {
	case ExtractFZV:
		return fzv, name, nil
	case ExtractWAV:
		rate, samples, derr := voiceextract.Decode(fzv)
		if derr != nil {
			return nil, "", errf("not-a-voice", "%v", derr)
		}
		var buf bytes.Buffer
		if err := wav.Write(&buf, &wav.File{SampleRate: rate, Samples: samples, Channels: 1}); err != nil {
			return nil, "", errf("extract-failed", "%v", err)
		}
		return buf.Bytes(), name, nil
	default:
		return nil, "", errf(codeInvalidValue, "format must be %s or %s, got %q", ExtractFZV, ExtractWAV, format)
	}
}

// NewInstrument creates an empty instrument on the open disk (R4), or
// on a fresh disk when none is open. The format cannot express a dump
// with no voices (a dump's voice area is sized from bank references),
// so the empty instrument is one bank whose single area references a
// silent placeholder slot; the first voice that joins fills it.
func (s *Session) NewInstrument(name string) (Snapshot, *Error) {
	if s.instrument != nil {
		return s.Snapshot(), errf("instrument-exists", "the disk already has an instrument")
	}
	if name == "" {
		name = "NEW INST"
	}
	if len(name) > disk.LabelSize {
		name = name[:disk.LabelSize]
	}
	img, cerr := s.imageOrNew()
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	if hasFile(img, disk.FullDumpName) {
		return s.Snapshot(), errf("instrument-exists", "the disk already has a full dump")
	}
	if err := diskadd.AddToImage(img, emptyInstrumentDump(name), 0); err != nil {
		return s.Snapshot(), addError(err)
	}
	return s.adopt(img)
}

// emptyInstrumentDump builds the smallest dump the format accepts:
// one bank sector with bstep 1 and a full default range on area 0,
// plus one voice sector whose slot 0 is a PlaybackModeNoSound
// placeholder (all zeros), which the parsers accept as an empty slot
// and the voice list hides.
func emptyInstrumentDump(name string) []byte {
	fzf := make([]byte, 2*disk.SectorSize)
	bank := fzf[:disk.SectorSize]
	binary.LittleEndian.PutUint16(bank[disk.BankVoiceCountOffset:], 1)
	padded := disk.PadLabel(name)
	copy(bank[disk.BankNameOffset:], padded[:])
	// Area 0: vp[0] is already 0 (the placeholder slot); make the
	// range playable so the first joined voice is audible untouched.
	bank[disk.BankKeyHighOffset] = disk.MaxMIDINote
	bank[disk.BankVelLowOffset] = 1
	bank[disk.BankVelHighOffset] = disk.MaxMIDINote
	bank[disk.BankKeyCentOffset] = 60
	bank[disk.BankAudioOutOffset] = 0xff
	return fzf
}
