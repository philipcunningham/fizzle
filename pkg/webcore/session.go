// Package webcore is the Web UI's core session: the document state the
// browser edits, behind the coarse boundary the front end speaks. It
// owns canonical state and validation; the WASM glue in web/wasm only
// translates calls. Errors carry stable codes that cross the boundary
// as structured envelopes, mirrored by the TypeScript contract in
// web/app/src/boundary.
package webcore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/disklist"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// Error is a boundary error envelope: a stable machine code plus a
// human message. It never wraps a panic; the glue recovers those.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Item names the offending file, voice, or field where one
	// exists, the way the spec's contract section promises.
	Item string `json:"item,omitempty"`
	// Detail carries the technical reason for a bug report; the
	// message above stays the one a musician reads.
	Detail string `json:"detail,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func errf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// errItemf is errf with the offending item named.
func errItemf(code, item, format string, args ...any) *Error {
	e := errf(code, format, args...)
	e.Item = item
	return e
}

// Envelope codes shared across calls; the TypeScript boundary matches
// on them.
const (
	codeNoDisk       = "no-disk"
	codeInvalidValue = "invalid-value"
	codeNotFound     = "not-found"
	// codePairMismatch covers every way two images fail to be disks 1
	// and 2 of one split set.
	codePairMismatch = "pair-mismatch"
	// codeLastArea refuses an area deletion that would leave a bank
	// with no areas, which drops the bank out of the dump.
	codeLastArea = "last-area"
	// codeMissingDisk refuses a mutation while half of a split pair is
	// absent; the message names the disk to fetch.
	codeMissingDisk = "missing-disk"
)

// FileSnapshot describes one directory entry for the UI. Voice files
// carry their editable parameter values keyed by schema field ID, plus
// the bespoke-editor detail (waveform extent, loops, envelopes).
type FileSnapshot struct {
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	SizeBytes int            `json:"sizeBytes"`
	Params    map[string]any `json:"params,omitempty"`
	Voice     *VoiceDetail   `json:"voice,omitempty"`
}

// DiskSnapshot describes the open disk set for the UI. Instrument is
// the parsed full dump when the disk carries one. Disks is 1 or 2: a
// split instrument spans a pair and the capacity figures cover both
// (R23). MissingDisk names the absent half when one image of a pair
// was opened alone (R5): 2 means "this is disk 1, disk 2 is missing".
type DiskSnapshot struct {
	Label         string `json:"label"`
	UsedBytes     int    `json:"usedBytes"`
	CapacityBytes int    `json:"capacityBytes"`
	// AudioBytes is what the instrument asks the sampler's memory to
	// hold, and MemoryBytes is what the user says their sampler has.
	// The two make the second reading of R23's capacity readout.
	AudioBytes  int                 `json:"audioBytes"`
	MemoryBytes int                 `json:"memoryBytes"`
	Disks       int                 `json:"disks"`
	MissingDisk int                 `json:"missingDisk,omitempty"`
	Files       []FileSnapshot      `json:"files"`
	Instrument  *InstrumentSnapshot `json:"instrument,omitempty"`
}

// Snapshot is the state the UI renders from. Revision is a monotonic
// per-session token; a changed revision means changed state.
type Snapshot struct {
	Revision int           `json:"revision"`
	Disk     *DiskSnapshot `json:"disk"`
	CanUndo  bool          `json:"canUndo"`
	CanRedo  bool          `json:"canRedo"`
}

// historyCap bounds the undo stack at the 100 deep R24 requires; Q-D
// leaves open only the depth beyond that floor. Each entry is a whole
// document, so the cap bounds memory too: at 1.25 MB an image, 100
// entries is 125 MB for one disk and 250 MB for a split pair, the price
// of holding images rather than diffs.
const historyCap = 100

// imagePair is one document state: a single image, or two when a
// split instrument spans a pair (img2 nil otherwise). History entries
// snapshot whole pairs so undo restores both halves together.
type imagePair struct {
	img1 []byte
	img2 []byte
	// disMode is the parse mode the state was recorded under, restored
	// with the bytes: re-deriving it from bytes an edit had already
	// moved flips the mode.
	disMode bool
}

// Session holds one open disk set. Not safe for concurrent use; the
// worker serialises calls.
type Session struct {
	revision int
	// memoryBytes is the sampler's sample memory as the user declared
	// it. Zero means they haven't, so sampleMemory supplies the default.
	memoryBytes int
	// audioBytes is the open instrument's audio area, measured when the
	// document changes rather than on every snapshot.
	audioBytes int
	label      string
	image      []byte
	image2     []byte // disk 2 of a split pair; nil for one disk documents
	used       int
	files      []FileSnapshot

	instrument  *InstrumentSnapshot
	missingDisk int // 1 or 2 when one half of a pair was opened alone
	// disMode marks a dump parsing under its DIS voice count. Decided
	// at document boundaries and held through edits, so an edit that
	// moves a bstep cannot flip it.
	disMode bool

	past        []imagePair // undo stack, oldest first
	future      []imagePair // redo stack
	inGesture   bool
	gestureBase *imagePair // pre-gesture document; nil until the first edit
}

// NewSession returns an empty session with no disk open.
func NewSession() *Session {
	return &Session{}
}

// Snapshot returns the state the UI renders from.
func (s *Session) Snapshot() Snapshot {
	snap := Snapshot{
		Revision: s.revision,
		CanUndo:  len(s.past) > 0,
		CanRedo:  len(s.future) > 0,
	}
	if s.image != nil {
		disks := 1
		if s.image2 != nil {
			disks = 2
		}
		snap.Disk = &DiskSnapshot{
			Label:         s.label,
			UsedBytes:     s.used,
			CapacityBytes: disks * disk.ImageSize,
			AudioBytes:    s.audioBytes,
			MemoryBytes:   s.sampleMemory(),
			Disks:         disks,
			MissingDisk:   s.missingDisk,
			Files:         append([]FileSnapshot{}, s.files...),
			Instrument:    s.instrument,
		}
	}
	return snap
}

// SampleMemoryMin and SampleMemoryMax bound what an FZ can hold. The
// FZ-1 shipped with 1 MB and reaches 2 MB with Casio's expansion card;
// the rack units shipped with 2 MB. The machine discovers which it has
// at power on: the wave memory probe at F000:07D4 counts 64 KB banks
// from a floor of 16, and length_limit at F000:7A74 spends what it
// found. Only five bits of the bank register reach memory, so 32 banks
// is the ceiling. See llm-wiki/topics/sample-memory.md.
const (
	SampleMemoryMin = 1 << 20
	SampleMemoryMax = 2 << 20
)

// sampleMemory is the machine the user declared, defaulting to the
// smaller one: a disk built for 1 MB loads on any FZ.
func (s *Session) sampleMemory() int {
	if s.memoryBytes == 0 {
		return SampleMemoryMin
	}
	return s.memoryBytes
}

// SetSampleMemory records how much sample memory the sampler has (R27).
// It describes the machine rather than the document, so it changes no
// bytes and leaves the revision, the undo history, and the dirty flag
// alone. It never refuses an import or an export; it informs.
func (s *Session) SetSampleMemory(bytes int) (Snapshot, *Error) {
	if bytes < SampleMemoryMin || bytes > SampleMemoryMax {
		return s.Snapshot(), errf(codeInvalidValue,
			"sample memory %d is outside the 1 MB to 2 MB an FZ holds", bytes)
	}
	s.memoryBytes = bytes
	return s.Snapshot(), nil
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
	fzf, err := diskget.FromImage(img, disk.FullDumpName)
	if err != nil {
		return false
	}
	_, src, rerr := fzutil.ResolveFZFHeader(fzf, vn)
	return rerr == nil && src == fzutil.VoiceCountDIS
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
		if int(e.DisSector) < disk.ReservedSectors || int(e.DisSector) >= disk.SectorCount {
			return 0
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
	if s.image == nil {
		return s.Snapshot(), errf(codeNoDisk, "no disk is open")
	}
	voice, cerr := convertWAV(filename, wavData, rate, channel)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	img, rerr := disk.ReadImage(bytes.NewReader(s.image))
	if rerr != nil {
		return s.Snapshot(), errf("invalid-image", "not a readable FZ image: %v", rerr)
	}
	if err := diskadd.AddToImage(img, voice, 0); err != nil {
		return s.Snapshot(), addError(err)
	}
	return s.adopt(img)
}

// typeCode reduces a disklist display name to the boundary's stable
// one-word lowercase code ("Full Dump" becomes "full").
func typeCode(displayName string) string {
	first, _, _ := strings.Cut(displayName, " ")
	return strings.ToLower(first)
}

// voiceParams maps parsed voice values to schema field IDs. The LFO
// waveform and playback mode come back as the identifiers the setters
// accept, read from the raw byte where the display name differs.
func voiceParams(vp *fzvinfo.VoiceParams, voiceBytes []byte) map[string]any {
	wave := ""
	if idx := int(lfoNameByte(voiceBytes) & disk.LFOWaveformMask); idx < len(lfoWaveNames) {
		wave = lfoWaveNames[idx]
	}
	mode := vp.PlaybackMode
	if mode == "synthesized" {
		mode = "synth"
	}
	// fzvinfo reports tune in semitones and KF in raw bytes; the schema
	// speaks the setters' units (the panel's cents, hardware display scale), so
	// both convert here.
	tune := 0
	sync := "off"
	if lfoNameByte(voiceBytes)&disk.LFOPhaseFlag != 0 {
		sync = "on"
	}
	if len(voiceBytes) >= disk.VoiceDCPOffset+2 {
		tune = int(int16(binary.LittleEndian.Uint16(voiceBytes[disk.VoiceDCPOffset : disk.VoiceDCPOffset+2]))) // #nosec G115 -- intentional signed reinterpretation
	}
	return map[string]any{
		fieldPlaybackMode: mode,
		fieldTune:         disk.TuneWordToDisplay(int16(tune)), //nolint:gosec // read back as int16 above
		fieldRootKey:      int(vp.KeyCentre),
		fieldKeyLow:       int(vp.KeyLow),
		fieldKeyHigh:      int(vp.KeyHigh),
		fieldCutoff:       int(vp.FilterCutoff),
		fieldResonance:    int(vp.FilterQ),
		fieldDcaLevelKF:   disk.KFByteToDisplay(uint8(vp.DCALevelKF)), // #nosec G115 -- two's complement round trip
		fieldDcaRateKF:    disk.KFByteToDisplay(uint8(vp.DCARateKF)),  // #nosec G115
		fieldDcfLevelKF:   disk.KFByteToDisplay(uint8(vp.DCFLevelKF)), // #nosec G115
		fieldDcfRateKF:    disk.KFByteToDisplay(uint8(vp.DCFRateKF)),  // #nosec G115
		fieldVelDcaKF:     int(vp.VelDCAKF),
		fieldVelDcfKF:     int(vp.VelDCFKF),
		fieldVelDcqKF:     disk.VelDCQByteToDisplay(uint8(vp.VelDCQKF)), //nolint:gosec // two's complement round trip
		fieldVelDcaRS:     int(vp.VelDCARS),
		fieldVelDcfRS:     int(vp.VelDCFRS),
		fieldLfoWave:      wave,
		fieldLfoRate:      int(vp.LFORate),
		fieldLfoDelay:     disk.LFODelayWordToDisplay(vp.LFODelay),
		fieldLfoPitch:     int(vp.LFODepthPitch),
		fieldLfoAmp:       int(vp.LFODepthAmp),
		fieldLfoFilter:    int(vp.LFODepthFilter),
		fieldLfoSync:      sync,
	}
}

// clampNumberField resolves a numeric schema edit and clamps the
// value to the field's declared range; every numeric setter shares
// it (R14).
func clampNumberField(fieldID string, value int) (int, *Error) {
	field, ok := schemaField(fieldID)
	if !ok || field.Kind == kindSelect {
		return 0, errItemf("invalid-field", fieldID, "%q is not a numeric schema field", fieldID)
	}
	return clampInt(value, field.Min, field.Max), nil
}

// checkSelectField resolves a select schema edit.
func checkSelectField(fieldID string) *Error {
	field, ok := schemaField(fieldID)
	if !ok || field.Kind != kindSelect {
		return errItemf("invalid-field", fieldID, "%q is not a select schema field", fieldID)
	}
	return nil
}

// loopSelectBuilder clamps the sustain and release designations both
// loop-select setters share; 8 means none.
func loopSelectBuilder(sustain, release int) func([]byte) ([]voiceedit.Patch, error) {
	sustain = clampInt(sustain, 0, disk.NoSustainLoop)
	release = clampInt(release, 0, disk.NoSustainLoop)
	return func([]byte) ([]voiceedit.Patch, error) {
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
	return s.patchVoice(fileName, func(voiceBytes []byte) ([]voiceedit.Patch, error) {
		return numberPatches(fieldID, value, voiceBytes)
	})
}

// SetParamOption sets a select schema field on a voice file.
func (s *Session) SetParamOption(fileName, fieldID, option string) (Snapshot, *Error) {
	if cerr := checkSelectField(fieldID); cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.patchVoice(fileName, func(voiceBytes []byte) ([]voiceedit.Patch, error) {
		return optionPatches(fieldID, option, voiceBytes)
	})
}

// openedImage parses the open disk, or answers the standard
// envelope: the guard and parse every image reader shares.
func (s *Session) openedImage() (*disk.Image, *Error) {
	if s.image == nil {
		return nil, errf(codeNoDisk, "no disk is open")
	}
	img, err := disk.ReadImage(bytes.NewReader(s.image))
	if err != nil {
		return nil, errf("invalid-image", "not a readable FZ image: %v", err)
	}
	return img, nil
}

// patchVoice applies build's patches to a voice file on the image and
// adopts the result.
func (s *Session) patchVoice(fileName string, build func([]byte) ([]voiceedit.Patch, error)) (Snapshot, *Error) {
	img, cerr := s.openedImage()
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	voiceBytes, gerr := diskget.FromImage(img, fileName)
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
	if err := diskadd.ReplaceInMemory(img, fileName, voiceBytes, 0); err != nil {
		return s.Snapshot(), errf("replace-failed", "%v", err)
	}
	return s.adopt(img)
}

func (s *Session) pushHistory(doc imagePair) {
	s.past = append(s.past, doc)
	if len(s.past) > historyCap {
		s.past = s.past[1:]
	}
}

// BeginGesture opens an undo bracket: edits until CommitGesture
// coalesce into one history entry.
func (s *Session) BeginGesture() {
	if !s.inGesture {
		s.inGesture = true
		s.gestureBase = nil
	}
}

// CommitGesture closes the bracket, landing the coalesced entry. It
// reports whether the gesture changed anything: a press and release
// with no movement lands no entry, and the UI must not call such a
// document dirty.
func (s *Session) CommitGesture() bool {
	if !s.inGesture {
		return false
	}
	s.inGesture = false
	if s.gestureBase != nil {
		s.pushHistory(*s.gestureBase)
		s.gestureBase = nil
		// The landed entry changes what undo does, so the snapshot
		// after the commit is new state: revision-keyed caches must
		// refetch.
		s.revision++
		return true
	}
	return false
}

// endGesture closes any open bracket before a history move. An undo
// interleaved into a drag would otherwise leave the pre-gesture image
// pending, and the later commit would push it on top of the undone
// state, inverting the timeline.
//
// The bracket reopens straight away: the pointer is still down, so the
// rest of that drag belongs in one entry rather than one per movement.
// The reopened bracket starts from the undone state, so its entry sits
// after the undo rather than across it.
func (s *Session) endGesture() {
	if !s.inGesture {
		return
	}
	s.CommitGesture()
	s.BeginGesture()
}

// Undo restores the previous state. The restored snapshot carries a
// fresh revision; a changed revision is the only change signal.
func (s *Session) Undo() (Snapshot, *Error) {
	s.endGesture()
	if len(s.past) == 0 {
		return s.Snapshot(), errf("nothing-to-undo", "nothing to undo")
	}
	prev := s.past[len(s.past)-1]
	img, err := disk.ReadImage(bytes.NewReader(prev.img1))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "history entry unreadable: %v", err)
	}
	s.past = s.past[:len(s.past)-1]
	s.future = append(s.future, imagePair{img1: s.image, img2: s.image2, disMode: s.disMode})
	s.disMode = prev.disMode
	return s.adoptState(img, prev.img2)
}

// Redo restores the state most recently undone.
func (s *Session) Redo() (Snapshot, *Error) {
	s.endGesture()
	if len(s.future) == 0 {
		return s.Snapshot(), errf("nothing-to-redo", "nothing to redo")
	}
	next := s.future[len(s.future)-1]
	img, err := disk.ReadImage(bytes.NewReader(next.img1))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "history entry unreadable: %v", err)
	}
	s.future = s.future[:len(s.future)-1]
	s.pushHistory(imagePair{img1: s.image, img2: s.image2, disMode: s.disMode})
	s.disMode = next.disMode
	return s.adoptState(img, next.img2)
}

// ExportImage returns a copy of disk 1's image bytes. Opening then
// exporting is byte identical.
func (s *Session) ExportImage() ([]byte, *Error) {
	return s.ExportImageAt(0)
}

// ExportImageAt returns a copy of one image of the document: index 0
// is disk 1, index 1 is disk 2 of a split pair (R25).
func (s *Session) ExportImageAt(index int) ([]byte, *Error) {
	if s.image == nil {
		return nil, errf(codeNoDisk, "no disk is open")
	}
	switch index {
	case 0:
		return bytes.Clone(s.image), nil
	case 1:
		if s.image2 == nil {
			return nil, errf(codeNotFound, "the document is one disk; there is no disk 2")
		}
		return bytes.Clone(s.image2), nil
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
	s.disMode = documentDISMode(img)
	snap, cerr := s.adoptState(img, img2)
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
	return s.adoptPair(img, s.image2, modeKeep)
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
	prev := imagePair{img1: s.image, img2: s.image2, disMode: s.disMode}
	if mode == modeDerive {
		s.disMode = documentDISMode(img)
	}
	snap, cerr := s.adoptState(img, img2)
	if cerr != nil {
		s.disMode = prev.disMode
		return snap, cerr
	}
	if prev.img1 != nil {
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
func (s *Session) adoptState(img *disk.Image, img2 []byte) (Snapshot, *Error) {
	listing, err := disklist.ParseImage(img)
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "not a readable FZ image: %v", err)
	}
	files := make([]FileSnapshot, 0, len(listing.Entries))
	for _, e := range listing.Entries {
		f := FileSnapshot{Name: e.Name, Type: typeCode(e.TypeName), SizeBytes: e.Size}
		if f.Type == "voice" {
			// A voice that fails to parse simply carries no params;
			// the listing itself already validated the container.
			if data, err := diskget.FromImage(img, e.Name); err == nil {
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
	if s.disMode {
		vn = disVoiceCount(img)
	}
	for _, f := range files {
		if f.Name == disk.FullDumpName && f.Type == "full" {
			if data, err := diskget.FromImage(img, f.Name); err == nil {
				if parsed, err := instrumentFrom(f.Name, data, vn); err == nil {
					inst = parsed
				}
			}
		}
	}
	s.instrument = inst
	s.image = img.Bytes()
	s.image2 = img2
	s.label = img.Label()
	s.used = disk.ImageSize - img.FreeSectors()*disk.SectorSize
	if img2 != nil {
		if i2, err := disk.ReadImage(bytes.NewReader(img2)); err == nil {
			s.used += disk.ImageSize - i2.FreeSectors()*disk.SectorSize
		}
	}
	s.files = files
	s.missingDisk = missingDiskOf(img, img2)
	// Measured once here, where the document changes and both images
	// are in hand, so a snapshot stays a plain read.
	s.audioBytes = 0
	if inst != nil {
		if fzf, cerr := s.stitchedDump(img); cerr == nil {
			s.audioBytes = dumpAudioBytes(fzf, vn)
		}
	}
	s.revision++
	return s.Snapshot(), nil
}
