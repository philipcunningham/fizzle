// Package diskfs provides filesystem-like operations on an in-memory FZ disk.
// It contains no host filesystem or command concerns, so application code can
// mutate a document without depending on CLI packages.
package diskfs

import (
	"fmt"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/render"
)

// File describes the directory and DIS metadata needed to store a payload.
type File struct {
	Name       [disk.LabelSize]byte
	Type       disk.FileType
	DiskNumber uint8
	Banks      int
	Voices     int
	Waves      int
}

// Entry describes one directory row. Corrupt entries retain their physical
// slot so a caller can offer repair without exposing disk parsing details.
type Entry struct {
	Index         int
	Name          string
	Type          disk.FileType
	Size          int
	Corrupt       bool
	CorruptReason string
	SlotIndex     *int
}

// Listing is an in-memory disk directory and its capacity summary.
type Listing struct {
	Label      string
	Entries    []Entry
	FreeBytes  int
	TotalBytes int
	UsedPct    int
}

// FullDump describes a full dump using either its parsed voice count or a
// trusted count supplied by the owning document.
func FullDump(data []byte, diskNumber uint8, voiceCount int) (File, error) {
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		return File{}, err
	}
	if voiceCount == 0 {
		voiceCount = hdr.NVoice
	}
	if voiceCount < 1 || voiceCount > disk.MaxVoices {
		return File{}, fmt.Errorf("voice count %d outside 1..%d", voiceCount, disk.MaxVoices)
	}
	waves := disk.SectorsNeeded(len(data)) - hdr.NBankSectors - disk.VoiceAreaSectors(voiceCount)
	if waves < 0 {
		return File{}, fmt.Errorf("voice count %d needs a voice area running past the file", voiceCount)
	}
	return File{Name: disk.PadLabel(disk.FullDumpName), Type: disk.TypeFullDump, DiskNumber: diskNumber, Banks: hdr.NBankSectors, Voices: voiceCount, Waves: waves}, nil
}

// Voice describes a standalone voice payload.
func Voice(data []byte, diskNumber uint8) (File, error) {
	if !disk.IsPlausibleVoiceHeader(data) {
		return File{}, fmt.Errorf("not a voice file")
	}
	var name [disk.LabelSize]byte
	copy(name[:], data[disk.VoiceNameOffset:disk.VoiceNameOffset+disk.LabelSize])
	return File{Name: name, Type: disk.TypeVoice, DiskNumber: diskNumber, Voices: 1, Waves: max(disk.SectorsNeeded(len(data))-1, 0)}, nil
}

// Add stores data under explicit metadata. Detection belongs to adapters.
func Add(img *disk.Image, data []byte, file File) error {
	if len(data) == 0 {
		return errorsf("file is empty")
	}
	if file.Type == disk.TypeFullDump {
		data = append([]byte(nil), data...)
		fzutil.ClearVoiceCountMarker(data)
	}
	entries, err := img.Directory()
	if err != nil {
		return fmt.Errorf("diskfs: reading directory: %w", err)
	}
	name := disk.TrimPadded(file.Name[:])
	for _, entry := range entries {
		if strings.EqualFold(entry.NameString(), name) {
			return errorsf("file %q already exists on disk (rename with 'fzv edit --name' before adding, or create a new disk)", name)
		}
	}
	dirOff, err := img.NextFreeDirSlot()
	if err != nil {
		return fmt.Errorf("diskfs: %w", err)
	}
	sectorCount := disk.SectorsNeeded(len(data))
	allocated, err := img.AllocateSectors(1 + sectorCount)
	if err != nil {
		return fmt.Errorf("diskfs: not enough space on disk (file is %s, usable disk capacity is %s): %w", render.FormatBytes(len(data)), render.FormatBytes(disk.UsableDataSize), err)
	}
	disSector, sectors := allocated[0], allocated[1:]
	dis := buildDIS(disSector, sectors, file.Banks, file.Voices, file.Waves)
	if err := img.SetSector(disSector, disk.EncodeDisSector(dis)); err != nil {
		return fmt.Errorf("diskfs: writing sector: %w", err)
	}
	padded := make([]byte, sectorCount*disk.SectorSize)
	copy(padded, data)
	for i, sector := range sectors {
		if err := img.SetSector(sector, padded[i*disk.SectorSize:(i+1)*disk.SectorSize]); err != nil {
			return fmt.Errorf("diskfs: writing sector: %w", err)
		}
	}
	entry := disk.DirEntry{Name: file.Name, FileType: file.Type, DiskNum: file.DiskNumber, DisSector: uint16(disSector)} //nolint:gosec
	copy(img.Bytes()[dirOff:dirOff+disk.DirEntrySize], disk.EncodeDirEntry(entry))
	return nil
}

// Replace transactionally replaces a named file. On failure the image is
// restored byte for byte.
func Replace(img *disk.Image, oldName string, data []byte, file File) (retErr error) {
	snapshot := append([]byte(nil), img.Bytes()...)
	defer func() {
		if retErr != nil {
			copy(img.Bytes(), snapshot)
		}
	}()
	if err := img.RemoveFile(oldName); err != nil {
		return fmt.Errorf("diskfs: %w", err)
	}
	return Add(img, data, file)
}

// Extract returns a named file's padded payload bytes.
func Extract(img *disk.Image, name string) ([]byte, error) {
	entries, err := img.Directory()
	if err != nil {
		return nil, fmt.Errorf("diskfs: %w", err)
	}
	var match *disk.DirEntry
	for i := range entries {
		if strings.EqualFold(entries[i].NameString(), name) {
			match = &entries[i]
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("diskfs: %q: %w", name, disk.ErrNotFound)
	}
	disBytes, err := img.SectorRef(int(match.DisSector))
	if err != nil {
		return nil, fmt.Errorf("diskfs: reading DIS sector: %w", err)
	}
	dis, err := disk.DecodeDisSector(disBytes)
	if err != nil {
		return nil, fmt.Errorf("diskfs: decoding DIS sector: %w", err)
	}
	if len(dis.Extents) == 0 {
		return nil, errorsf("%q has no extents", name)
	}
	var raw []byte
	for i, extent := range dis.Extents {
		start, end := int(extent[0]), int(extent[1])
		if i == 0 && start == int(match.DisSector) {
			start++
		}
		for sector := start; sector <= end; sector++ {
			data, err := img.SectorRef(sector)
			if err != nil {
				return nil, fmt.Errorf("diskfs: reading sector %d: %w", sector, err)
			}
			raw = append(raw, data...)
		}
	}
	return raw, nil
}

// List parses the directory without logging or performing host I/O.
func List(img *disk.Image) (*Listing, error) {
	listing := &Listing{Label: img.Label(), Entries: []Entry{}}
	for slot, dirSlot := range img.DirectorySlots() {
		e := dirSlot.Entry
		switch dirSlot.Kind {
		case disk.DirSlotBlank:
			continue
		case disk.DirSlotRubbish:
			if !disk.IsPrintableName(e.Name[:]) {
				continue
			}
			slotIndex := slot
			listing.Entries = append(listing.Entries, Entry{
				Name:          e.NameString(),
				Corrupt:       true,
				CorruptReason: fmt.Sprintf("named directory slot points outside the data sectors (DIS sector %d)", e.DisSector),
				SlotIndex:     &slotIndex,
			})
		case disk.DirSlotEntry:
			disBytes, err := img.SectorRef(int(e.DisSector))
			if err != nil {
				listing.Entries = append(listing.Entries, Entry{Name: e.NameString(), Corrupt: true, CorruptReason: fmt.Sprintf("failed to read DIS sector %d: %v", e.DisSector, err)})
				continue
			}
			dis, err := disk.DecodeDisSector(disBytes)
			if err != nil {
				listing.Entries = append(listing.Entries, Entry{Name: e.NameString(), Corrupt: true, CorruptReason: fmt.Sprintf("failed to decode DIS sector %d: %v", e.DisSector, err)})
				continue
			}
			listing.Entries = append(listing.Entries, Entry{Name: e.NameString(), Type: e.FileType, Size: dis.PayloadSize()})
		}
	}
	for i := range listing.Entries {
		listing.Entries[i].Index = i + 1
	}
	free := img.FreeSectors()
	total := disk.SectorCount - disk.ReservedSectors
	listing.FreeBytes = free * disk.SectorSize
	listing.TotalBytes = total * disk.SectorSize
	listing.UsedPct = 100 * (total - free) / total
	return listing, nil
}

// LooseFileCount returns the number of directory entries other than the full dump.
func LooseFileCount(img *disk.Image) int {
	entries, err := img.Directory()
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.NameString() != disk.FullDumpName {
			count++
		}
	}
	return count
}

func buildDIS(disSector int, sectors []int, banks, voices, waves int) disk.DisSector {
	var dis disk.DisSector
	all := append([]int{disSector}, sectors...)
	if len(sectors) == 0 {
		return dis
	}
	start, end := all[0], all[0]
	for _, sector := range all[1:] {
		if sector == end+1 {
			end = sector
			continue
		}
		dis.Extents = append(dis.Extents, [2]uint16{uint16(start), uint16(end)}) //nolint:gosec
		start, end = sector, sector
	}
	dis.Extents = append(dis.Extents, [2]uint16{uint16(start), uint16(end)})                    //nolint:gosec
	dis.BankCount, dis.VoiceCount, dis.WaveCount = uint16(banks), uint16(voices), uint16(waves) //nolint:gosec
	return dis
}

func errorsf(format string, args ...any) error {
	return fmt.Errorf("diskfs: "+format, args...)
}
