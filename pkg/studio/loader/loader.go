// Package loader reads .img and .fzf files into a studio model.Model
// and returns a ContainerInfo summary the App displays in its header.
//
// For .fzf: the file bytes are loaded directly and validated as an FZ
// full dump.
//
// For .img: the FULL-DATA-FZ directory entry is located and its
// extents are concatenated into the in-memory FZF byte slice. If no
// FULL-DATA-FZ entry exists the loader returns ErrNoFullDump; the App
// then surfaces a status message inviting the user to pick another
// file.
package loader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// Format is the container's on-disk format.
type Format int

// Container file extensions the loader recognises. Centralised so the
// extension dispatch and the goconst-flagged checks reference the
// same literal.
const (
	extFZF = ".fzf"
	extIMG = ".img"
)

const (
	// FormatUnknown is the zero value for an unloaded container.
	FormatUnknown Format = iota
	// FormatFZF is a standalone .fzf full dump.
	FormatFZF
	// FormatIMG is an .img disk image containing a FULL-DATA-FZ entry.
	FormatIMG
)

// ContainerInfo summarises the loaded container.
type ContainerInfo struct {
	Path           string // file path on disk
	Format         Format // .fzf or .img
	BankCount      int    // number of populated bank sectors
	VoiceCount     int    // total voices across all banks
	PCMBytes       int64  // bytes occupied by sample data
	TotalBytes     int64  // total in-memory size of the container
	AudioAreaStart int    // byte offset where the shared wave area begins
	Header         *fzutil.FZFHeader

	// DiskEntryName is the name of the file inside the .img we loaded
	// from. "FULL-DATA-FZ" for full-dump disks; for voice-only disks
	// it's the FZV's name (e.g. "HOOVER"). Used by Save to write back
	// into the same slot.
	DiskEntryName string

	// WrappedVoice is true when the container is a synthetic FZF built
	// around a single Voice (.img holds an FZV, not a full dump). Save
	// must unwrap before writing back as a Voice file.
	WrappedVoice bool
}

// ErrNoFullDump signals that an .img has no FULL-DATA-FZ entry.
var ErrNoFullDump = errors.New("loader: image contains no FULL-DATA-FZ entry")

// LoadContainer reads the file at path and returns a Model wrapping
// the in-memory FZF bytes plus a ContainerInfo summary.
func LoadContainer(path string) (*model.Model, ContainerInfo, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case extFZF:
		return loadFZF(path)
	case extIMG:
		return loadIMG(path)
	default:
		return nil, ContainerInfo{}, fmt.Errorf("loader: unsupported extension %q", ext)
	}
}

// NewUntitled returns a fresh untitled in-memory FZF with eight empty
// banks. Used by the App's launch flow when no file argument is
// supplied.
func NewUntitled() (*model.Model, ContainerInfo) {
	// Eight bank sectors of zeros, then a one-sector empty voice area.
	// This is the minimum that ParseFZFHeader will accept as a real
	// container; the App enables bank editing from here without a load
	// step.
	const banks = 8
	bytes := make([]byte, disk.SectorSize*(banks+1))
	// Seed each bank's bstep to 0 (already zero from make) and each
	// bank's name to spaces so the bank list shows blank slots rather
	// than uninitialised content. The voice area is zero; voicedata
	// records' loop-mode word at +0x10 is 0x0000 (NoSound), which
	// IsActiveOrEmptyVoiceSlot accepts as empty.
	for b := 0; b < banks; b++ {
		base := b * disk.SectorSize
		for i := 0; i < disk.VoiceNameFieldSize; i++ {
			bytes[base+disk.BankNameOffset+i] = ' '
		}
	}
	return model.FromBytes("", bytes), ContainerInfo{
		Path:           "",
		Format:         FormatFZF,
		BankCount:      banks,
		VoiceCount:     0,
		TotalBytes:     int64(len(bytes)),
		AudioAreaStart: disk.SectorSize * (banks + 1),
	}
}

func loadFZF(path string) (*model.Model, ContainerInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: reading %s: %w", path, err)
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		// A NewUntitled save (all bsteps zero, no audio) trips the
		// hardware-strict bstep check. Fall back to an empty editable
		// container bound to path so "new disk -> save -> reopen"
		// round-trips. Any non-empty parse failure is still surfaced.
		if looksLikeEmptyFZF(data) {
			return model.FromBytes(path, data), ContainerInfo{
				Path:           path,
				Format:         FormatFZF,
				BankCount:      8,
				VoiceCount:     0,
				TotalBytes:     int64(len(data)),
				AudioAreaStart: disk.SectorSize * 9,
			}, nil
		}
		return nil, ContainerInfo{}, fmt.Errorf("loader: parsing %s: %w", path, err)
	}
	info := ContainerInfo{
		Path:           path,
		Format:         FormatFZF,
		BankCount:      hdr.NBankSectors,
		VoiceCount:     hdr.NVoice,
		PCMBytes:       int64(len(data) - hdr.VoiceAreaStart - hdr.NVoice*disk.VoicePackSize),
		TotalBytes:     int64(len(data)),
		AudioAreaStart: hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize,
		Header:         hdr,
	}
	if info.PCMBytes < 0 {
		info.PCMBytes = 0
	}
	return model.FromBytes(path, data), info, nil
}

func loadIMG(path string) (*model.Model, ContainerInfo, error) {
	img, err := disk.OpenImage(path)
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: opening %s: %w", path, err)
	}
	entries, err := img.Directory()
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: reading directory of %s: %w", path, err)
	}
	const fullDumpName = "FULL-DATA-FZ"
	var match *disk.DirEntry
	var voiceFallback *disk.DirEntry
	for i := range entries {
		if strings.EqualFold(entries[i].NameString(), fullDumpName) {
			match = &entries[i]
			break
		}
		if entries[i].FileType == disk.TypeVoice && voiceFallback == nil {
			voiceFallback = &entries[i]
		}
	}
	if match == nil && voiceFallback != nil {
		// Voice-only .img: wrap the FZV into a synthetic single-voice
		// FZF so the rest of studio (Layout, Sound, Pool) can edit it
		// uniformly. Save unwraps back to a Voice before writing.
		return loadIMGVoice(path, img, voiceFallback)
	}
	if match == nil {
		// A blank disk from "new disk -> save as .img" carries no
		// FULL-DATA-FZ entry (we skip the diskadd.AddBytes step on
		// empty containers because the FZ-1's bstep>=1 check would
		// reject a zero-voice payload). Surface that as an empty
		// editable container bound to the IMG path, the same shape
		// NewUntitled produces, so the user can pick up where they
		// left off.
		m, untitled := NewUntitled()
		// Re-bind the model to the .img path so saves know where to
		// write. (NewUntitled returns a path-less model; FromBytes is
		// the only path-setting constructor.)
		mWithPath := model.FromBytes(path, m.Bytes())
		untitled.Path = path
		untitled.Format = FormatIMG
		untitled.DiskEntryName = disk.FullDumpName
		return mWithPath, untitled, nil
	}
	if int(match.DisSector) < disk.ReservedSectors || int(match.DisSector) >= disk.SectorCount {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: DIS sector %d out of range", path, match.DisSector)
	}
	disSec, err := img.SectorRef(int(match.DisSector))
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: reading DIS sector: %w", path, err)
	}
	dis, err := disk.DecodeDisSector(disSec)
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: decoding DIS sector: %w", path, err)
	}
	if len(dis.Extents) == 0 {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: FULL-DATA-FZ has no extents", path)
	}
	data, err := extractFileBytes(img, dis, int(match.DisSector))
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: extracting bytes: %w", path, err)
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: parsing extracted FZF: %w", path, err)
	}
	info := ContainerInfo{
		Path:           path,
		Format:         FormatIMG,
		BankCount:      hdr.NBankSectors,
		VoiceCount:     hdr.NVoice,
		PCMBytes:       int64(len(data) - hdr.VoiceAreaStart - hdr.NVoice*disk.VoicePackSize),
		TotalBytes:     int64(len(data)),
		AudioAreaStart: hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize,
		Header:         hdr,
		DiskEntryName:  disk.FullDumpName,
	}
	if info.PCMBytes < 0 {
		info.PCMBytes = 0
	}
	return model.FromBytes(path, data), info, nil
}

// loadIMGVoice handles an .img that contains a Voice file (no full
// dump). Extracts the FZV bytes, then synthesises a one-bank /
// one-voice FZF in memory so the studio editor can treat it
// uniformly. The wrapping is a fixed-layout 2-sector prefix:
//
//	[0..1023]    Bank 0 sector (bstep=1, otherwise zero)
//	[1024..2047] Voice header sector (voice 0 slot at offset 1024)
//	[2048..]     Audio data, copied verbatim from the FZV's audio area
//
// The FZV header's wave / gen / loop pointers are 0-relative to the
// FZV's audio start, which is exactly where this synthetic FZF's
// audio area begins (byte 2048). So the pointers don't need
// rewriting; they map one-to-one onto the synthetic container.
func loadIMGVoice(path string, img *disk.Image, entry *disk.DirEntry) (*model.Model, ContainerInfo, error) {
	if int(entry.DisSector) < disk.ReservedSectors || int(entry.DisSector) >= disk.SectorCount {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: voice DIS sector %d out of range", path, entry.DisSector)
	}
	disSec, err := img.SectorRef(int(entry.DisSector))
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: reading DIS sector: %w", path, err)
	}
	dis, err := disk.DecodeDisSector(disSec)
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: decoding DIS sector: %w", path, err)
	}
	fzv, err := extractFileBytes(img, dis, int(entry.DisSector))
	if err != nil {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: extracting voice bytes: %w", path, err)
	}
	if len(fzv) < disk.SectorSize {
		return nil, ContainerInfo{}, fmt.Errorf("loader: %s: voice payload too small (%d bytes)", path, len(fzv))
	}

	// Build the synthetic FZF: 1 bank sector + 1 voice sector + audio.
	data := make([]byte, 0, 2*disk.SectorSize+(len(fzv)-disk.SectorSize))
	bank := make([]byte, disk.SectorSize)
	// bstep = 1 (little-endian uint16); leaves the rest of the bank
	// sector zero. The bank name stays empty; there's nothing to
	// derive it from for a Voice-only disk.
	bank[disk.BankVoiceCountOffset] = 1
	data = append(data, bank...)
	data = append(data, fzv...)

	info := ContainerInfo{
		Path:           path,
		Format:         FormatIMG,
		BankCount:      1,
		VoiceCount:     1,
		PCMBytes:       int64(len(fzv) - disk.SectorSize),
		TotalBytes:     int64(len(data)),
		AudioAreaStart: 2 * disk.SectorSize,
		DiskEntryName:  entry.NameString(),
		WrappedVoice:   true,
		// Header stays nil; there's no FZF header in the synthetic
		// container. Code paths that read Header should already gate
		// on it; AudioAreaStart and the bank/voice counts give them
		// everything they need.
	}
	if info.PCMBytes < 0 {
		info.PCMBytes = 0
	}
	return model.FromBytes(path, data), info, nil
}

// extractFileBytes walks the DIS extent list and concatenates the
// referenced sector ranges into a single byte slice. Mirrors the
// logic in pkg/diskget but writes to memory rather than a file.
func extractFileBytes(img *disk.Image, dis disk.DisSector, disSectorLoc int) ([]byte, error) {
	total := dis.PayloadSize()
	out := make([]byte, 0, total)
	for _, ext := range dis.Extents {
		start := int(ext[0])
		end := start + int(ext[1])
		// Skip the DIS sector itself when it falls in the first extent.
		if start <= disSectorLoc && disSectorLoc < end {
			start = disSectorLoc + 1
		}
		for s := start; s < end; s++ {
			ref, err := img.SectorRef(s)
			if err != nil {
				return nil, err
			}
			out = append(out, ref...)
		}
	}
	if len(out) > total {
		out = out[:total]
	}
	return out, nil
}

// looksLikeEmptyFZF reports whether data has the NewUntitled shape:
// at least 8 bank sectors with every bstep word zero. We allow other
// bytes (bank renames, etc.) to vary so user edits to a fresh canvas
// still round-trip; the only invariant we check is that no bank has
// been populated with voices.
func looksLikeEmptyFZF(data []byte) bool {
	if len(data) < disk.SectorSize*8 {
		return false
	}
	for b := 0; b < 8; b++ {
		base := b * disk.SectorSize
		if data[base+disk.BankVoiceCountOffset] != 0 ||
			data[base+disk.BankVoiceCountOffset+1] != 0 {
			return false
		}
	}
	return true
}
