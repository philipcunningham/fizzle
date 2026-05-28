// Package model is the in-memory state container for fizzle studio.
//
// The model owns the raw FZF bytes, the parsed header, the undo/redo
// stacks, and the dirty flag. It exposes a small API the widgets under
// pkg/studio/widgets bind to: Apply for edits, Undo/Redo for time
// travel, Save for persistence, and Subscribe for change notification.
//
// No tview imports live in this package; the model is pure logic and
// is unit-testable without spinning up a tview Application. Subscribe
// callbacks are invoked synchronously on the goroutine that called
// Apply/Undo/Redo/Save, so widget callers must drive the model from
// the main goroutine (the standard tview rule).
package model

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// ErrPatchOutOfBounds is returned by Apply when the patch offset or size
// would write outside the FZF byte slice.
var ErrPatchOutOfBounds = errors.New("model: patch out of bounds")

// sourceKind tags how the bytes in memory were loaded; used by Save to
// route the write through the correct persistence helper.
type sourceKind int

const (
	sourceFZF sourceKind = iota // standalone .fzf file
	sourceIMG                   // .img disk image
)

// Model is the studio in-memory FZF state. Construct via New.
type Model struct {
	path   string
	source sourceKind

	// Header companion data for .img sources. For sourceFZF these are
	// zero values and unused.
	imgPath    string // path to primary .img (== path for sourceIMG)
	diskName   string // canonical FZ entry name (FULL-DATA-FZ or voice name)
	diskNum1   uint8  // 1-based primary disk number; 0 = single-disk
	companion  string // companion image path (multi-disk); empty otherwise
	imgBaseDir string // resolved dir of the primary image (for companion search)

	bytes  []byte
	header *fzutil.FZFHeader

	undo []undoRecord
	redo []undoRecord
	// saveIndex is the position in the undo stack that corresponds to the
	// on-disk state. The model is dirty when saveIndex != len(undo).
	saveIndex int

	subs []*subscription
}

type subscription struct {
	fn func()
}

// New loads the FZF at path. Accepts standalone .fzf files and .img disk
// images. For .img: the directory is searched for a full-dump entry
// (FULL-DATA-FZ); if found that file is loaded. If no full-dump entry
// exists, the first voice file in the directory is loaded instead.
//
// The returned model holds the in-memory FZF bytes, the parsed header,
// and the source metadata Save needs to write back (the .fzf path or the
// .img path plus disk-number and companion-image info for the multi-disk
// dual-write).
func New(path string) (*Model, error) {
	m := &Model{path: path}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".fzf":
		data, err := fzutil.ReadBounded(path, fzutil.MaxReadSize)
		if err != nil {
			return nil, fmt.Errorf("model: %w", err)
		}
		hdr, err := fzutil.ParseFZFHeader(data)
		if err != nil {
			return nil, fmt.Errorf("model: %w", err)
		}
		m.source = sourceFZF
		m.bytes = data
		m.header = hdr
	case ".img":
		if err := m.loadFromImage(path); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("model: unsupported file extension %q (want .fzf or .img)", ext)
	}
	return m, nil
}

// loadFromImage extracts the full-dump (or first voice) entry from a disk
// image and parses it into bytes + header. Companion-image discovery for
// multi-disk dumps is deferred to Save. At load time we only need the
// payload bytes; the dual-disk dance happens at write time.
func (m *Model) loadFromImage(path string) error {
	img, err := disk.OpenImage(path)
	if err != nil {
		return fmt.Errorf("model: opening image: %w", err)
	}
	entries, err := img.Directory()
	if err != nil {
		return fmt.Errorf("model: reading directory: %w", err)
	}
	var entryName string
	for _, e := range entries {
		if e.FileType == disk.TypeFullDump {
			entryName = e.NameString()
			break
		}
	}
	if entryName == "" {
		// Fall back to the first voice file. The spec says full-dump is
		// preferred, but if absent and there's a voice we can still edit
		// it (used by per-voice extraction workflows).
		for _, e := range entries {
			if e.FileType == disk.TypeVoice {
				entryName = e.NameString()
				break
			}
		}
	}
	if entryName == "" {
		return fmt.Errorf("model: image %q contains no full-dump or voice files", path)
	}
	// diskget writes through fileutil.WriteAtomic, so we route through a
	// scratch file rather than copying private logic out of diskget. The
	// scratch file lives in the user's temp dir; we delete it after the
	// read.
	scratchDir, err := os.MkdirTemp("", "fizzle-model-")
	if err != nil {
		return fmt.Errorf("model: scratch dir: %w", err)
	}
	defer os.RemoveAll(scratchDir) //nolint:errcheck // best-effort cleanup
	scratch := filepath.Join(scratchDir, "extract.fzf")
	if err := diskget.Get(path, entryName, scratch); err != nil {
		return fmt.Errorf("model: extracting from image: %w", err)
	}
	data, err := fzutil.ReadBounded(scratch, fzutil.MaxReadSize)
	if err != nil {
		return fmt.Errorf("model: reading extracted data: %w", err)
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		return fmt.Errorf("model: parsing extracted FZF: %w", err)
	}

	// Resolve disk-num and companion path eagerly so Save doesn't have to
	// do file-system work while it already holds the cross-process lock.
	diskNum1 := uint8(0)
	for _, e := range entries {
		if strings.EqualFold(e.NameString(), entryName) {
			diskNum1 = e.DiskNum + 1
			break
		}
	}

	m.source = sourceIMG
	m.imgPath = path
	m.diskName = entryName
	m.diskNum1 = diskNum1
	m.imgBaseDir = filepath.Dir(path)
	m.bytes = data
	m.header = hdr

	// Multi-disk companion discovery: only required when this disk
	// declares itself as disk 2 of a pair. Discovery walks the directory
	// for sibling .img files; we record the result but do not error on
	// failure (Save surfaces a clearer error in that case).
	if diskNum1 == 2 {
		comp, err := findCompanionImage(path, m.imgBaseDir, int(diskNum1))
		if err == nil {
			m.companion = comp
		}
	}
	return nil
}

// Path returns the source path passed to New.
func (m *Model) Path() string { return m.path }

// Header returns the parsed FZF header. Mutation by the caller is undefined.
func (m *Model) Header() *fzutil.FZFHeader { return m.header }

// Bytes returns the underlying FZF byte slice. Callers must not mutate the
// slice; use Apply / SetBankName / Undo / Redo for mutations.
func (m *Model) Bytes() []byte { return m.bytes }

// Apply writes patch into the in-memory bytes, captures the pre-image for
// undo, and notifies subscribers. Bytes-payload patches and Value-payload
// patches are both supported. The redo stack is cleared (standard
// undo/redo semantics).
//
// Returns ErrPatchOutOfBounds if the patch would touch bytes outside the
// in-memory slice.
//
// Apply takes an FZF-absolute offset. For voice-header-relative patches
// from voiceedit.BuildXPatches, use ApplyVoicePatch instead; it
// translates offsets and fans out key-range patches to bank sites.
func (m *Model) Apply(patch voiceedit.Patch) error {
	op, err := m.applyOne(patch)
	if err != nil {
		return err
	}
	m.undo = append(m.undo, undoRecord{ops: []opRecord{op}})
	m.redo = nil
	m.notify()
	return nil
}

// applyOne writes one patch and returns the opRecord describing the
// reversal. Does NOT push onto the undo stack; callers (Apply,
// ApplyBatch) decide how to group ops into undo records. Does NOT
// notify subscribers; the caller fires notify once after the whole
// logical edit lands.
func (m *Model) applyOne(patch voiceedit.Patch) (opRecord, error) {
	size := patch.Size
	if patch.Bytes != nil {
		size = len(patch.Bytes)
	}
	if size <= 0 || patch.Offset < 0 || patch.Offset+size > len(m.bytes) {
		return opRecord{}, fmt.Errorf("%w: offset=%d size=%d len=%d", ErrPatchOutOfBounds, patch.Offset, size, len(m.bytes))
	}

	// Snapshot the pre-image BEFORE the write lands. Snapshotting the
	// post-image too keeps Redo a single memcpy without re-running the
	// patch encoder.
	pre := make([]byte, size)
	copy(pre, m.bytes[patch.Offset:patch.Offset+size])

	if patch.Bytes != nil {
		copy(m.bytes[patch.Offset:patch.Offset+size], patch.Bytes)
	} else {
		switch patch.Size {
		case 1:
			m.bytes[patch.Offset] = byte(patch.Value) //nolint:gosec // G115: spec restricts 1-byte Value to 0..255; widget validation enforces this upstream
		case 2:
			binary.LittleEndian.PutUint16(m.bytes[patch.Offset:], patch.Value)
		default:
			// Restore the pre-image; the failed write never lands.
			copy(m.bytes[patch.Offset:patch.Offset+size], pre)
			return opRecord{}, fmt.Errorf("model: unsupported patch size %d", patch.Size)
		}
	}
	post := make([]byte, size)
	copy(post, m.bytes[patch.Offset:patch.Offset+size])

	return opRecord{offset: patch.Offset, preImage: pre, postImage: post}, nil
}

// ApplyBatch applies several patches as one atomic undo step. Either
// all patches land and a single undo record is pushed, or (if any
// patch fails to apply) the partial writes are rolled back and no
// undo record is created.
//
// Use this when one logical edit comprises multiple byte writes
// (e.g. a key-range change that fans out to bank sites).
func (m *Model) ApplyBatch(patches []voiceedit.Patch) error {
	if len(patches) == 0 {
		return nil
	}
	ops := make([]opRecord, 0, len(patches))
	for _, p := range patches {
		op, err := m.applyOne(p)
		if err != nil {
			// Roll back the ops that already landed, in reverse order.
			for i := len(ops) - 1; i >= 0; i-- {
				prev := ops[i]
				copy(m.bytes[prev.offset:prev.offset+len(prev.preImage)], prev.preImage)
			}
			return err
		}
		ops = append(ops, op)
	}
	m.undo = append(m.undo, undoRecord{ops: ops})
	m.redo = nil
	m.notify()
	return nil
}

// ApplyVoicePatch is the widget-friendly entry point for editing a
// voice's parameters. It accepts a patch whose Offset is voice-header-
// relative (as produced by voiceedit.BuildNamePatch, BuildTunePatch,
// BuildLFOPatches, etc.) and translates it to FZF-absolute via the
// header's voice-area offset.
//
// For key-range patches (offsets VoiceKeyHighOffset, VoiceKeyLowOffset,
// or VoiceKeyCentOffset, spec §2-1 0xae/0xaf/0xb0), the patch is fanned
// out across every bank site that references the voice via vp[],
// writing the matching BankKeyHighOffset / BankKeyLowOffset /
// BankKeyCentOffset byte (spec §2-2, 0x02/0x42/0x102). All resulting
// byte writes are recorded as ONE undo step. Without the fan-out,
// hardware reading the bank sector ignores the key-range edit
// (silent wrong output). See voiceedit.syncBankKeyRange for the
// per-file equivalent invoked by the CLI edit path.
func (m *Model) ApplyVoicePatch(slot int, patch voiceedit.Patch) error {
	if slot < 0 || slot >= m.header.NVoice {
		return fmt.Errorf("model: voice slot %d out of range [0,%d)", slot, m.header.NVoice)
	}
	voiceOffset := disk.VoiceSlotOffset(m.header.VoiceAreaStart, slot)

	// Translate the voice-header-relative patch to absolute.
	abs := patch
	abs.Offset = voiceOffset + patch.Offset

	// Decide whether to fan out. Only single-byte key-range writes get
	// the bank-site treatment; bytes-payload patches at the same offset
	// are handled normally (no real-world voiceedit builder produces a
	// multi-byte key-range patch).
	bankOff, isKeyRange := bankOffsetForVoiceField(patch.Offset)
	if !isKeyRange || patch.Bytes != nil {
		return m.ApplyBatch([]voiceedit.Patch{abs})
	}

	sites := fzutil.FindBankSitesForVoice(m.bytes, m.header, slot)
	batch := make([]voiceedit.Patch, 0, 1+len(sites))
	batch = append(batch, abs)
	for _, site := range sites {
		batch = append(batch, voiceedit.Patch{
			Offset: site.BankIdx*disk.SectorSize + bankOff + site.SplitIdx,
			Size:   1,
			Value:  patch.Value,
		})
	}
	return m.ApplyBatch(batch)
}

// bankOffsetForVoiceField maps a voice-header field offset to the
// matching bank-sector array start. Returns (0, false) for fields that
// don't have a bank counterpart.
func bankOffsetForVoiceField(voiceFieldOffset int) (int, bool) {
	switch voiceFieldOffset {
	case disk.VoiceKeyHighOffset:
		return disk.BankKeyHighOffset, true
	case disk.VoiceKeyLowOffset:
		return disk.BankKeyLowOffset, true
	case disk.VoiceKeyCentOffset:
		return disk.BankKeyCentOffset, true
	}
	return 0, false
}

// Subscribe registers fn to be invoked on every Apply / Undo / Redo / Save.
// Returns an unsubscribe function. Callbacks run synchronously on the
// calling goroutine; widget callers must drive the model from the main
// goroutine (the standard tview rule).
func (m *Model) Subscribe(fn func()) func() {
	s := &subscription{fn: fn}
	m.subs = append(m.subs, s)
	return func() {
		for i, x := range m.subs {
			if x == s {
				m.subs = append(m.subs[:i], m.subs[i+1:]...)
				return
			}
		}
	}
}

func (m *Model) notify() {
	for _, s := range m.subs {
		s.fn()
	}
}

// Voice returns the parsed parameters for voice slot slot.
func (m *Model) Voice(slot int) (*fzvinfo.VoiceParams, error) {
	if m.header == nil {
		return nil, errors.New("model: header not loaded")
	}
	if slot < 0 || slot >= m.header.NVoice {
		return nil, fmt.Errorf("model: voice slot %d out of range (0-%d)", slot, m.header.NVoice-1)
	}
	off := disk.VoiceSlotOffset(m.header.VoiceAreaStart, slot)
	if off+disk.VoiceHeaderUsed > len(m.bytes) {
		return nil, fmt.Errorf("model: voice %d header truncated", slot)
	}
	// Look up the voice's stored name so we can use ParseVoiceInFZF as the
	// canonical decoder. Avoid duplicating parse logic here.
	rawName := m.bytes[off+disk.VoiceNameOffset : off+disk.VoiceNameOffset+disk.LabelSize]
	name := disk.TrimPadded(rawName)
	if name == "" {
		// Fallback name to match fzvinfo's substitution; required because
		// ParseVoiceInFZF resolves by name and will error on empty.
		name = fmt.Sprintf("VOICE %d", slot+1)
		// If the actual stored name is empty, ParseVoiceInFZF will fail
		// to find it. Build a synthetic copy with the substituted name
		// instead so v2 widgets always receive a non-nil VoiceParams.
		// This mirrors fzvinfo.ParseBankVoiceEntry's substitution.
		return parseSlotDirectly(m.bytes, off, name)
	}
	return fzvinfo.ParseVoiceInFZF(m.bytes, name)
}

// parseSlotDirectly is a fallback for voices whose name bytes are blank.
// It copies the 192-byte voice header into a 1024-byte buffer, fills in
// the substituted name, and re-parses via the standard fzvinfo path.
// The substituted name is written only into this scratch buffer; the
// in-memory FZF is not modified.
func parseSlotDirectly(data []byte, off int, name string) (*fzvinfo.VoiceParams, error) {
	hdr := make([]byte, disk.SectorSize)
	copy(hdr, data[off:off+disk.VoiceHeaderUsed])
	padded := disk.PadLabel(strings.ToUpper(name))
	copy(hdr[disk.VoiceNameOffset:disk.VoiceNameOffset+disk.LabelSize], padded[:])
	// Synthesise a minimal FZF wrapper: bank sector (1024 bytes of
	// zeros) plus voice header. The downstream parser only needs the
	// voice header bytes for the fields we expose.
	wrapper := make([]byte, disk.SectorSize+disk.SectorSize)
	binary.LittleEndian.PutUint16(wrapper[disk.BankVoiceCountOffset:], 1)
	bankName := disk.PadLabel("BANK")
	copy(wrapper[disk.BankNameOffset:disk.BankNameOffset+disk.LabelSize], bankName[:])
	copy(wrapper[disk.SectorSize:], hdr)
	return fzvinfo.ParseVoiceInFZF(wrapper, name)
}

// BankName returns the trimmed bank name at bankIdx. Returns the empty
// string if bankIdx is out of range or the bytes are truncated.
func (m *Model) BankName(bankIdx int) string {
	off := bankIdx*disk.SectorSize + disk.BankNameOffset
	if off+disk.LabelSize > len(m.bytes) {
		return ""
	}
	return disk.TrimPadded(m.bytes[off : off+disk.LabelSize])
}

// SetBankName writes a new bank name at bankIdx via Apply. The name is
// padded to disk.LabelSize and stored verbatim (mixed case preserved).
// The FZ-1 hardware supports mixed-case names; factory disks such as
// "All Voices" demonstrate this. Returns an error if name exceeds
// disk.LabelSize characters.
func (m *Model) SetBankName(bankIdx int, name string) error {
	if len(name) > disk.LabelSize {
		return fmt.Errorf("model: bank name %q exceeds %d chars", name, disk.LabelSize)
	}
	if m.header == nil || bankIdx < 0 || bankIdx >= m.header.NBankSectors {
		return fmt.Errorf("model: bank index %d out of range (0-%d)", bankIdx, m.header.NBankSectors-1)
	}
	padded := disk.PadLabel(name)
	off := bankIdx*disk.SectorSize + disk.BankNameOffset
	return m.Apply(voiceedit.Patch{Offset: off, Bytes: padded[:]})
}

// Save persists the in-memory FZF bytes back to the source path. For
// standalone .fzf files Save uses fileutil.WriteAtomic. For .img sources
// Save patches the in-image FZF via diskadd.ReplaceInMemory and writes
// the resulting image atomically; if a companion image was discovered at
// load time the pair is written together via writeImagePairAtomic, so a
// partial failure rolls the primary back to its pre-write bytes.
//
// On success, the undo stack is cleared and IsDirty returns false.
// Subscribers are notified after the save lands.
func (m *Model) Save() error {
	switch m.source {
	case sourceFZF:
		if err := fileutil.WithFileLock(m.path, func() error {
			return fileutil.WriteAtomic(m.path, m.bytes)
		}); err != nil {
			return err
		}
	case sourceIMG:
		if err := m.saveToImage(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("model: unknown source kind %d", m.source)
	}
	m.undo = nil
	m.redo = nil
	m.saveIndex = 0
	m.notify()
	return nil
}

// findCompanionImage walks dir for sibling .img files whose disk-number
// metadata identifies them as the other half of a multi-disk pair. It
// returns the resolved companion path or an error. The matching logic is a
// trimmed-down version of pkg/studio/companion.go::findCompanionImage.
// We don't need diskLabel matching here because the bank/voice contents
// already constrain the pair, but we do reuse the disk-number directory
// scan.
func findCompanionImage(imgPath, dir string, currentDiskNum int) (string, error) {
	absImg, err := filepath.Abs(imgPath)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".img") {
			continue
		}
		cand := filepath.Join(dir, e.Name())
		abs, _ := filepath.Abs(cand)
		if abs == absImg {
			continue
		}
		img, err := disk.OpenImage(cand)
		if err != nil {
			continue
		}
		dirEntries, err := img.Directory()
		if err != nil {
			continue
		}
		for _, de := range dirEntries {
			if !strings.EqualFold(de.NameString(), disk.FullDumpName) {
				continue
			}
			dn1 := int(de.DiskNum) + 1
			if dn1 != currentDiskNum && dn1 > 0 {
				return cand, nil
			}
		}
	}
	return "", fmt.Errorf("model: companion image not found in %s", dir)
}
