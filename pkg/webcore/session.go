// Package webcore is the Web UI's core session: the document state the
// browser edits, behind the coarse boundary the front end speaks. It
// owns canonical state and validation; the WASM glue in web/wasm only
// translates calls. Errors carry stable codes that cross the boundary
// as structured envelopes, mirrored by the TypeScript contract in
// web/app/src/boundary.
package webcore

import (
	"bytes"
	"errors"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	documentmodel "github.com/philipcunningham/fizzle/pkg/document"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

func authorityFromDIS(disMode bool) documentmodel.Authority {
	if disMode {
		return documentmodel.AuthorityDIS
	}
	return documentmodel.AuthorityWalk
}

// Session holds one open disk set. Not safe for concurrent use; the
// worker serialises calls.
type Session struct {
	preparedDocument
	revision int
	// memoryBytes is the sampler's sample memory as the user declared
	// it. Zero means they haven't, so sampleMemory supplies the default.
	memoryBytes int
	past        []documentmodel.State // undo stack, oldest first
	future      []documentmodel.State // redo stack
	inGesture   bool
	gestureBase *documentmodel.State // pre-gesture document; nil until the first edit
}

// preparedDocument is a fully parsed candidate state. Building one is
// side-effect free; a Session installs it only after every derivation succeeds.
type preparedDocument struct {
	state documentmodel.State
	// audioBytes is the open instrument's audio area, measured when the
	// document changes rather than on every snapshot.
	audioBytes  int
	label       string
	used        int
	files       []FileSnapshot
	instrument  *InstrumentSnapshot
	missingDisk int
}

// NewSession returns an empty session with no disk open.
func NewSession() *Session {
	return &Session{}
}

// dumpAudioBytes totals the audio a dump asks the sampler to hold: the
// area past its bank and voice header sectors. That is what the
// sampler loads and what the disk's own wave sector count records, so
// it counts audio no voice header claims and the sector padding that
// loads with it. The estimate measures the same way, which is why the
// reading and the import dialog agree.
//
// A per voice sum cannot do this. It drops slots the voice list hides,
// such as one set to make no sound, whose samples stay in the dump and
// still load, and it counts frames where the dump pads to a sector.
func dumpAudioBytes(fzf []byte, disVN int) int {
	d, cerr := newDumpState(fzf, disVN)
	if cerr != nil {
		return 0
	}
	return len(d.fzf) - d.audioStart
}

// parseMode says how a document transition decides the dump's parse
// mode: an edit keeps it, a wholesale replacement re-derives it from
// the image being adopted.
type parseMode uint8

// The two transitions.
const (
	modeKeep parseMode = iota
	modeDerive
)

// documentDISMode reports whether the disk's dump parses under its
// DIS voice count rather than the walk.
func documentDISMode(img *disk.Image) bool {
	vn := disVoiceCount(img)
	if vn == 0 {
		return false
	}
	fzf, err := diskfs.Extract(img, disk.FullDumpName)
	if err != nil {
		return false
	}
	layout, rerr := fzutil.ResolveDiskFZFLayout(fzf, vn)
	return rerr == nil && layout.VoiceCountSource() == fzutil.VoiceCountDIS
}

// disVoiceCount reads the voice count from the FULL-DATA-FZ entry's
// DIS tail, or 0 when the disk has no readable dump. For a split
// pair, disk 1's tail already carries the pair's total.
func disVoiceCount(img *disk.Image) int {
	entries, err := img.Directory()
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.FileType != disk.TypeFullDump || e.NameString() != disk.FullDumpName {
			continue
		}
		sec, serr := img.SectorRef(int(e.DisSector))
		if serr != nil {
			return 0
		}
		dis, derr := disk.DecodeDisSector(sec)
		if derr != nil {
			return 0
		}
		return int(dis.VoiceCount)
	}
	return 0
}

// NewDisk replaces the session's disk with a blank formatted image.
func (s *Session) NewDisk(label string) (Snapshot, *Error) {
	img, err := diskformat.BuildImage(label)
	if err != nil {
		return s.Snapshot(), errf("invalid-label", "%v", err)
	}
	return s.install(img)
}

// OpenImage replaces the session's disk with the given image after
// validating that the core can parse it. The bytes are copied, so the
// caller's buffer (often a transferable from the UI) is never aliased.
func (s *Session) OpenImage(data []byte) (Snapshot, *Error) {
	if len(data) != disk.ImageSize {
		return s.Snapshot(), errf("invalid-image", "an FZ image is %d bytes, got %d", disk.ImageSize, len(data))
	}
	return s.install(data)
}

// Channel names accepted by ImportWAV, mirrored by the TypeScript
// contract.
const (
	ChannelLeft  = "left"
	ChannelRight = "right"
	ChannelMix   = "mix"
)

// ImportWAV converts a WAV to an FZ voice and adds it to the open
// disk: the browser side of J1. The voice name derives from the
// filename the way the CLI derives it. channel is ChannelLeft,
// ChannelRight, or ChannelMix and only matters for stereo input.
func (s *Session) ImportWAV(filename string, wavData []byte, rate uint32, channel string) (Snapshot, *Error) {
	if !s.state.IsOpen() {
		return s.Snapshot(), errf(codeNoDisk, "no disk is open")
	}
	voice, cerr := convertWAV(filename, wavData, rate, channel)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	img, rerr := disk.ReadImage(bytes.NewReader(s.state.Image1()))
	if rerr != nil {
		return s.Snapshot(), errf("invalid-image", "not a readable FZ image: %v", rerr)
	}
	file, err := diskfs.Voice(voice, 0)
	if err != nil {
		return s.Snapshot(), addError(err)
	}
	if err := diskfs.Add(img, voice, file); err != nil {
		return s.Snapshot(), addError(err)
	}
	return s.adopt(img)
}

// clampNumberField resolves a numeric schema edit and clamps the
// value to the field's declared range; every numeric setter shares
// it (R14).
func clampNumberField(fieldID string, value int) (int, *Error) {
	field, ok := schemaField(fieldID)
	if !ok || field.Kind == kindSelect {
		return 0, errItemf(codeInvalidField, fieldID, "%q is not a numeric schema field", fieldID)
	}
	return clampInt(value, field.Min, field.Max), nil
}

// checkSelectField resolves a select schema edit.
func checkSelectField(fieldID string) *Error {
	field, ok := schemaField(fieldID)
	if !ok || field.Kind != kindSelect {
		return errItemf(codeInvalidField, fieldID, "%q is not a select schema field", fieldID)
	}
	return nil
}

// loopSelectBuilder clamps the sustain and release designations both
// loop-select setters share; 8 means none.
func loopSelectBuilder(sustain, release int) func([]byte) ([]voiceedit.Edit, error) {
	sustain = clampInt(sustain, 0, disk.NoSustainLoop)
	release = clampInt(release, 0, disk.NoSustainLoop)
	return func([]byte) ([]voiceedit.Edit, error) {
		return voiceedit.BuildLoopSelectPatch(sustain, release)
	}
}

// SetParamNumber sets a numeric schema field on a voice file, clamping
// the value to the field's declared range (R14).
func (s *Session) SetParamNumber(fileName, fieldID string, value int) (Snapshot, *Error) {
	value, cerr := clampNumberField(fieldID, value)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.patchVoice(fileName, func(voiceBytes []byte) ([]voiceedit.Edit, error) {
		return numberPatches(fieldID, value, voiceBytes)
	})
}

// SetParamOption sets a select schema field on a voice file.
func (s *Session) SetParamOption(fileName, fieldID, option string) (Snapshot, *Error) {
	if cerr := checkSelectField(fieldID); cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.patchVoice(fileName, func(voiceBytes []byte) ([]voiceedit.Edit, error) {
		return optionPatches(fieldID, option, voiceBytes)
	})
}

// openedImage parses the open disk, or answers the standard
// envelope: the guard and parse every image reader shares.
func (s *Session) openedImage() (*disk.Image, *Error) {
	if !s.state.IsOpen() {
		return nil, errf(codeNoDisk, "no disk is open")
	}
	img, err := disk.ReadImage(bytes.NewReader(s.state.Image1()))
	if err != nil {
		return nil, errf("invalid-image", "not a readable FZ image: %v", err)
	}
	return img, nil
}

// patchVoice applies build's patches to a voice file on the image and
// adopts the result.
func (s *Session) patchVoice(fileName string, build func([]byte) ([]voiceedit.Edit, error)) (Snapshot, *Error) {
	img, cerr := s.openedImage()
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	voiceBytes, gerr := diskfs.Extract(img, fileName)
	if gerr != nil {
		return s.Snapshot(), errf(codeNotFound, "%v", gerr)
	}
	patches, perr := build(voiceBytes)
	if perr != nil {
		var cerr *Error
		if errors.As(perr, &cerr) {
			return s.Snapshot(), cerr
		}
		return s.Snapshot(), errf(codeInvalidValue, "%v", perr)
	}
	if err := voiceedit.ApplyToFZVBytes(voiceBytes, patches); err != nil {
		return s.Snapshot(), errf("not-a-voice", "%v", err)
	}
	file, err := diskfs.Voice(voiceBytes, 0)
	if err != nil {
		return s.Snapshot(), errf("replace-failed", "%v", err)
	}
	if err := diskfs.Replace(img, fileName, voiceBytes, file); err != nil {
		return s.Snapshot(), errf("replace-failed", "%v", err)
	}
	return s.adopt(img)
}

// ExportImage returns a copy of disk 1's image bytes. Opening then
// exporting is byte identical.
func (s *Session) ExportImage() ([]byte, *Error) {
	return s.ExportImageAt(0)
}

// ExportImageAt returns a copy of one image of the document: index 0
// is disk 1, index 1 is disk 2 of a split pair (R25).
func (s *Session) ExportImageAt(index int) ([]byte, *Error) {
	if !s.state.IsOpen() {
		return nil, errf(codeNoDisk, "no disk is open")
	}
	switch index {
	case 0:
		return s.state.Image1(), nil
	case 1:
		if !s.state.HasSecondDisk() {
			return nil, errf(codeNotFound, "the document is one disk; there is no disk 2")
		}
		return s.state.Image2(), nil
	default:
		return nil, errf(codeInvalidValue, "disk index must be 0 or 1, got %d", index)
	}
}

// install parses and adopts an image, advancing the revision only on
// success.
func (s *Session) install(data []byte) (Snapshot, *Error) {
	img, err := disk.ReadImage(bytes.NewReader(data))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "not a readable FZ image: %v", err)
	}
	// ReadImage only checks the container. The Disk ID tag (spec
	// section 1-2) separates a formatted FZ disk from arbitrary bytes;
	// Directory() is a forgiving view that never returns an entry with
	// an out-of-range DIS pointer, so no per-entry check remains.
	if img.Bytes()[disk.DiskNameTagOffset] != disk.DiskNameTag {
		return s.Snapshot(), errf("invalid-image", "not a readable FZ image: no FZ disk identification tag")
	}
	return s.adoptFresh(img, nil)
}

// adoptFresh installs a newly opened document: history starts empty,
// because undoing past an open would resurrect the disk the user just
// left, and it would arrive under the new document's name.
func (s *Session) adoptFresh(img *disk.Image, img2 []byte) (Snapshot, *Error) {
	snap, cerr := s.adoptState(img, img2, documentDISMode(img))
	if cerr != nil {
		return snap, cerr
	}
	s.past = nil
	s.future = nil
	s.inGesture = false
	s.gestureBase = nil
	return s.Snapshot(), nil
}

// adopt takes a parsed image as the session's disk as a user-visible
// mutation, keeping the document's disk 2 half as it was.
func (s *Session) adopt(img *disk.Image) (Snapshot, *Error) {
	return s.adoptPair(img, s.state.Image2(), modeKeep)
}

// checkWholeDocument refuses a mutation while half of a split pair is
// absent (R5). A lone half carries a truncated dump: a size growing
// edit re-splits that truncation and formats a fresh disk 2 over the
// real one, and a header only edit rewrites disk 1's wave count down to
// what this half holds, so the sampler stops asking for the other disk.
// The refusal names the disk to fetch (E1) and changes nothing (E3).
// Reads stay open: the shell opens a lone half deliberately, so the
// user can still look at it and export it.
func (s *Session) checkWholeDocument() *Error {
	if s.missingDisk == 0 {
		return nil
	}
	return errf(codeMissingDisk,
		"disk %d of this split instrument is missing; open it alongside this one to edit the instrument",
		s.missingDisk)
}

// adoptPair takes a parsed image (and a split document's disk 2 image
// bytes, nil for one disk) as a user-visible mutation: the prior
// document joins the undo history (or the pending gesture) and the
// redo stack clears.
//
// Every mutating call lands here, so this is the one place a document
// missing half of its pair refuses one. Opening and undo go through
// adoptState instead, which is what lets the user open the other half,
// or a different document, from here.
func (s *Session) adoptPair(img *disk.Image, img2 []byte, mode parseMode) (Snapshot, *Error) {
	if cerr := s.checkWholeDocument(); cerr != nil {
		return s.Snapshot(), cerr
	}
	prev := s.state
	nextMode := s.state.UsesDIS()
	if mode == modeDerive {
		nextMode = documentDISMode(img)
	}
	snap, cerr := s.adoptState(img, img2, nextMode)
	if cerr != nil {
		return snap, cerr
	}
	if prev.IsOpen() {
		if s.inGesture {
			// The whole drag lands as one undo entry (R24): only the
			// pre-gesture document is kept, on the first edit.
			if s.gestureBase == nil {
				s.gestureBase = &prev
			}
		} else {
			s.pushHistory(prev)
		}
	}
	s.future = nil
	return snap, nil
}

// adoptState installs a parsed image pair without touching history;
// undo and redo use it directly.
func (s *Session) adoptState(img *disk.Image, img2 []byte, disMode bool) (Snapshot, *Error) {
	next, cerr := prepareDocument(img, img2, disMode)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	s.preparedDocument = next
	s.revision++
	return s.Snapshot(), nil
}

func prepareDocument(img *disk.Image, img2 []byte, disMode bool) (preparedDocument, *Error) {
	var parsedImage2 *disk.Image
	if img2 != nil {
		var err error
		parsedImage2, err = disk.ReadImage(bytes.NewReader(img2))
		if err != nil {
			return preparedDocument{}, errf("invalid-image", "disk 2 unreadable: %v", err)
		}
	}
	listing, err := diskfs.List(img)
	if err != nil {
		return preparedDocument{}, errf("invalid-image", "not a readable FZ image: %v", err)
	}
	files := make([]FileSnapshot, 0, len(listing.Entries))
	for _, e := range listing.Entries {
		typeName := e.Type.String()
		if e.Corrupt {
			typeName = "(corrupt)"
		}
		f := FileSnapshot{Name: e.Name, Type: typeCode(typeName), SizeBytes: e.Size}
		if f.Type == "voice" {
			// A voice that fails to parse simply carries no params;
			// the listing itself already validated the container.
			if data, err := diskfs.Extract(img, e.Name); err == nil {
				if vp, err := fzvinfo.ParseBytes(data); err == nil {
					f.Params = voiceParams(vp, data)
					f.Voice = voiceDetailFrom(vp)
				}
			}
		}
		files = append(files, f)
	}
	// The disk's full dump, when present, is the instrument the UI
	// edits (spec section 6). A dump that fails to parse simply
	// carries no instrument; the file row still lists it.
	var inst *InstrumentSnapshot
	vn := 0
	if disMode {
		vn = disVoiceCount(img)
	}
	for _, f := range files {
		if f.Name == disk.FullDumpName && f.Type == "full" {
			if data, err := diskfs.Extract(img, f.Name); err == nil {
				if parsed, err := instrumentFrom(f.Name, data, vn); err == nil {
					inst = parsed
				}
			}
		}
	}
	state, stateErr := documentmodel.NewState(img.Bytes(), img2, authorityFromDIS(disMode))
	if stateErr != nil {
		return preparedDocument{}, errf("invalid-image", "%v", stateErr)
	}
	next := preparedDocument{
		state:       state,
		label:       img.Label(),
		used:        disk.ImageSize - img.FreeSectors()*disk.SectorSize,
		files:       files,
		instrument:  inst,
		missingDisk: state.MissingDisk(),
	}
	if parsedImage2 != nil {
		next.used += disk.ImageSize - parsedImage2.FreeSectors()*disk.SectorSize
	}
	// Measured once here, where the document changes and both images
	// are in hand, so a snapshot stays a plain read.
	if inst != nil {
		if fzf, err := state.StitchedFullDump(); err == nil {
			next.audioBytes = dumpAudioBytes(fzf, vn)
		}
	}
	return next, nil
}
