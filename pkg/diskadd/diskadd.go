// Package diskadd implements the fizzle disk add command. It copies a file
// onto an FZ series disk image, auto-detecting the file type from its contents.
package diskadd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/render"
	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// AddBytes copies fileData onto the disk image at imagePath with explicit
// file type, name, and DIS tail counts. Used for multi-disk full dumps where
// the DIS tail counts must be controlled precisely (e.g. disk 1 wn = total
// wave sectors across both disks). diskNum sets the directory entry disk number.
func AddBytes(imagePath string, fileData []byte, name [disk.LabelSize]byte, fileType disk.FileType, diskNum uint8, nbank, nvoice, nwave int) error {
	log.Info().
		Str("type", fileType.String()).
		Str("name", strings.TrimRight(string(name[:]), " ")).
		Int("disknum", int(diskNum)).
		Msg("adding to disk")
	log.Debug().
		Int("banks", nbank).
		Int("voices", nvoice).
		Int("waves", nwave).
		Int("sectors", disk.SectorsNeeded(len(fileData))).
		Msg("file layout")

	return writeToImage(imagePath, fileData, name, fileType, diskNum, nbank, nvoice, nwave)
}

// Add copies the file at filePath onto the disk image at imagePath. The file
// type is detected automatically from the file contents. diskNum sets the
// directory entry disk number: 0 for a single-disk or first-disk file,
// 1 for the continuation disk of a 2-disk full dump. The image is written
// atomically.
func Add(imagePath, filePath string, diskNum uint8) error {
	fileData, err := fzutil.ReadBounded(filePath, fzutil.MaxReadSize)
	if err != nil {
		return fmt.Errorf("diskadd: reading file: %w", err)
	}
	if len(fileData) == 0 {
		return errors.New("diskadd: file is empty")
	}

	fi, err := detectFile(fileData)
	if err != nil {
		return fmt.Errorf("diskadd: %w", err)
	}

	// Program files carry no name field; derive it from the host filepath.
	if fi.fileType == disk.TypeProgram {
		fi.name = programNameFromPath(filePath)
	}

	log.Info().
		Str("file", filepath.Base(filePath)).
		Str("type", fi.fileType.String()).
		Str("name", strings.TrimRight(string(fi.name[:]), " ")).
		Msg("adding to disk")
	log.Debug().
		Int("banks", fi.nbank).
		Int("voices", fi.nvoice).
		Int("waves", fi.nwave).
		Int("sectors", disk.SectorsNeeded(len(fileData))).
		Msg("file layout")

	return writeToImage(imagePath, fileData, fi.name, fi.fileType, diskNum, fi.nbank, fi.nvoice, fi.nwave)
}

// AddToImage adds fileData to an in-memory disk image with the same
// content detection as Add and no filesystem access. Program files are
// rejected: their name comes from a host filename, which an in-memory
// caller doesn't have; use Add or AddBytes for those.
func AddToImage(img *disk.Image, fileData []byte, diskNum uint8) error {
	if len(fileData) == 0 {
		return errors.New("diskadd: file is empty")
	}
	fi, err := detectFile(fileData)
	if err != nil {
		return fmt.Errorf("diskadd: %w", err)
	}
	if fi.fileType == disk.TypeProgram {
		return errors.New("diskadd: program files need a host filename for their name; use Add")
	}
	return addToImage(img, fileData, fi.name, fi.fileType, diskNum, fi.nbank, fi.nvoice, fi.nwave)
}

// AddBytesToImage adds fileData to an in-memory disk image with explicit
// file type, name, and DIS tail counts: the in-memory twin of AddBytes.
// Used for multi-disk full dumps, where the counts must be controlled
// rather than detected (disk 1's wn spans both disks, and disk 2 is a bare
// audio continuation that content detection cannot classify).
func AddBytesToImage(img *disk.Image, fileData []byte, name [disk.LabelSize]byte, fileType disk.FileType, diskNum uint8, nbank, nvoice, nwave int) error {
	if len(fileData) == 0 {
		return errors.New("diskadd: file is empty")
	}
	return addToImage(img, fileData, name, fileType, diskNum, nbank, nvoice, nwave)
}

func writeToImage(imagePath string, fileData []byte, name [disk.LabelSize]byte, fileType disk.FileType, diskNum uint8, nbank, nvoice, nwave int) error {
	return fileutil.WithFileLock(imagePath, func() error {
		img, err := disk.OpenImage(imagePath)
		if err != nil {
			return fmt.Errorf("diskadd: %w", err)
		}
		if err := addToImage(img, fileData, name, fileType, diskNum, nbank, nvoice, nwave); err != nil {
			return err
		}
		return fileutil.WriteAtomic(imagePath, img.Bytes())
	})
}

// addToImage adds a file to the in-memory disk image without writing to disk.
func addToImage(img *disk.Image, fileData []byte, name [disk.LabelSize]byte, fileType disk.FileType, diskNum uint8, nbank, nvoice, nwave int) error {
	entries, err := img.Directory()
	if err != nil {
		return fmt.Errorf("diskadd: reading directory: %w", err)
	}
	newName := disk.TrimPadded(name[:])
	for _, e := range entries {
		if strings.EqualFold(e.NameString(), newName) {
			return fmt.Errorf("diskadd: file %q already exists on disk (rename with 'fzv edit --name' before adding, or create a new disk)", newName)
		}
	}

	// Reserve the directory slot before allocating any sectors or mutating
	// the CAT, so ErrDirFull leaves the in-memory image untouched. After
	// AllocateSectors and SetSector, the CAT and data sectors would already
	// be dirty with no directory entry to point at them.
	dirOff, err := img.NextFreeDirSlot()
	if err != nil {
		return fmt.Errorf("diskadd: %w", err)
	}

	dataSectorCount := disk.SectorsNeeded(len(fileData))

	allocated, err := img.AllocateSectors(1 + dataSectorCount)
	if err != nil {
		return fmt.Errorf("diskadd: not enough space on disk (file is %s, usable disk capacity is %s): %w", render.FormatBytes(len(fileData)), render.FormatBytes(disk.UsableDataSize), err)
	}
	disSectorIdx := allocated[0]
	dataSectors := allocated[1:]

	dis := buildDIS(disSectorIdx, dataSectors, nbank, nvoice, nwave)
	disBytes := disk.EncodeDisSector(dis)
	if err := img.SetSector(disSectorIdx, disBytes); err != nil {
		return fmt.Errorf("diskadd: writing sector: %w", err)
	}

	padded := make([]byte, dataSectorCount*disk.SectorSize)
	copy(padded, fileData)
	for i, sec := range dataSectors {
		b := padded[i*disk.SectorSize : (i+1)*disk.SectorSize]
		if err := img.SetSector(sec, b); err != nil {
			return fmt.Errorf("diskadd: writing sector: %w", err)
		}
	}

	entry := disk.DirEntry{
		Name:      name,
		FileType:  fileType,
		DiskNum:   diskNum,
		DisSector: uint16(disSectorIdx), //nolint:gosec // G115: sector index < 1280, fits uint16
	}
	copy(img.Bytes()[dirOff:dirOff+disk.DirEntrySize], disk.EncodeDirEntry(entry))
	return nil
}

// ReplaceOnImage replaces a named file on a disk image with new data.
// The old file is removed (sectors freed, directory entry cleared) and the
// new data is added. The image is written atomically. Other files on the
// disk are preserved.
func ReplaceOnImage(imagePath string, oldName string, fileData []byte, diskNum uint8) error {
	return fileutil.WithFileLock(imagePath, func() error {
		img, err := disk.OpenImage(imagePath)
		if err != nil {
			return fmt.Errorf("diskadd: %w", err)
		}
		if err := ReplaceInMemory(img, oldName, fileData, diskNum); err != nil {
			return err
		}
		return fileutil.WriteAtomic(imagePath, img.Bytes())
	})
}

// ReplaceInMemory replaces a named file on an in-memory disk image without
// writing to disk. This allows the caller to batch multiple replacements
// before writing, enabling transactional semantics across multiple images.
//
// The mutation is transactional: if any step fails (file not found, detect
// failure, disk full, directory full) the image bytes are restored to their
// pre-call state, so callers can keep using the *Image after an error, say
// to try a different replacement or write a sibling image, without
// inheriting a half-modified CAT or directory.
func ReplaceInMemory(img *disk.Image, oldName string, fileData []byte, diskNum uint8) (retErr error) {
	// Snapshot the full image to roll back a partial mutation on error. At
	// 1.25 MiB the allocation is cheap next to the rest of the work, and
	// copy() restores in place without reallocating the caller's *Image.
	snapshot := append([]byte(nil), img.Bytes()...)
	defer func() {
		if retErr != nil {
			copy(img.Bytes(), snapshot)
		}
	}()

	if err := img.RemoveFile(oldName); err != nil {
		return fmt.Errorf("diskadd: %w", err)
	}
	fi, err := detectFile(fileData)
	if err != nil {
		return fmt.Errorf("diskadd: %w", err)
	}
	return addToImage(img, fileData, fi.name, fi.fileType, diskNum, fi.nbank, fi.nvoice, fi.nwave)
}

// AddToImageWithVoiceCount is AddToImage for a full dump whose voice
// count is known from the source disk's DIS tail. The known count
// overrides detection's bstep sum, and wn moves with it, or the
// sampler reads the audio a sector early.
func AddToImageWithVoiceCount(img *disk.Image, fileData []byte, diskNum uint8, vn int) error {
	if len(fileData) == 0 {
		return errors.New("diskadd: file is empty")
	}
	fi, err := detectFullDumpWithVoiceCount(fileData, vn)
	if err != nil {
		return fmt.Errorf("diskadd: %w", err)
	}
	return addToImage(img, fileData, fi.name, fi.fileType, diskNum, fi.nbank, fi.nvoice, fi.nwave)
}

// ReplaceInMemoryWithVoiceCount is ReplaceInMemory with
// AddToImageWithVoiceCount's known voice count semantics.
func ReplaceInMemoryWithVoiceCount(img *disk.Image, oldName string, fileData []byte, diskNum uint8, vn int) (retErr error) {
	snapshot := append([]byte(nil), img.Bytes()...)
	defer func() {
		if retErr != nil {
			copy(img.Bytes(), snapshot)
		}
	}()

	if err := img.RemoveFile(oldName); err != nil {
		return fmt.Errorf("diskadd: %w", err)
	}
	return AddToImageWithVoiceCount(img, fileData, diskNum, vn)
}

// detectFullDumpWithVoiceCount runs content detection, then overrides
// the full dump's voice count with the caller's known vn and rebuilds
// the counts that derive from it.
func detectFullDumpWithVoiceCount(fileData []byte, vn int) (fileInfo, error) {
	fi, err := detectFile(fileData)
	if err != nil {
		return fileInfo{}, err
	}
	if fi.fileType != disk.TypeFullDump {
		return fileInfo{}, fmt.Errorf("known voice count given for a %s file; only full dumps carry one", fi.fileType)
	}
	if vn < 1 || vn > disk.MaxVoices {
		return fileInfo{}, fmt.Errorf("voice count %d outside 1..%d", vn, disk.MaxVoices)
	}
	fi.nvoice = vn
	fi.nwave = disk.SectorsNeeded(len(fileData)) - fi.nbank - disk.VoiceAreaSectors(vn)
	if fi.nwave < 0 {
		fi.nwave = 0
	}
	applyMultiDiskMarker(fileData, &fi)
	return fi, nil
}

// buildDIS constructs a DisSector from a list of allocated sector indices,
// grouping contiguous sectors into extent (start, end) pairs. disSector is
// the index of the DIS sector itself, which the FZ-1 expects to be ss of the
// first extent. This matches the format written by the real hardware.
func buildDIS(disSector int, sectors []int, nbank, nvoice, nwave int) disk.DisSector {
	var dis disk.DisSector
	if len(sectors) == 0 {
		return dis
	}

	// Prepend the DIS sector so it becomes ss0 of the first extent.
	allSectors := append([]int{disSector}, sectors...)

	start := allSectors[0]
	end := allSectors[0]
	for _, s := range allSectors[1:] {
		if s == end+1 {
			end = s
		} else {
			dis.Extents = append(dis.Extents, [2]uint16{uint16(start), uint16(end)}) //nolint:gosec // G115: sector index < 1280, fits uint16
			start = s
			end = s
		}
	}
	dis.Extents = append(dis.Extents, [2]uint16{uint16(start), uint16(end)}) //nolint:gosec // G115: sector index < 1280, fits uint16

	dis.BankCount = uint16(nbank)   //nolint:gosec // G115: bank count ≤ 8
	dis.VoiceCount = uint16(nvoice) //nolint:gosec // G115: voice count bounded by FZ disk capacity (≤ 64)
	dis.WaveCount = uint16(nwave)   //nolint:gosec // G115: wave count bounded by FZ disk capacity
	return dis
}

// fileInfo holds the detected type metadata for a file being added to a disk.
type fileInfo struct {
	fileType disk.FileType
	name     [disk.LabelSize]byte
	nbank    int
	nvoice   int
	nwave    int
}

// sumBankBSteps returns the sum of bstep (voice count) across the first
// nbank bank sectors. Each bank's bstep counts the voice slots that bank
// uses; for files where banks don't share slots via vp[] (the common case)
// the sum equals the total voice-area slot count, which is what the DIS
// tail's vn field should reflect.
func sumBankBSteps(fileData []byte, nbank int) int {
	total := 0
	for b := range nbank {
		off := b * disk.SectorSize
		if off+disk.BankVoiceCountOffset+2 > len(fileData) {
			break
		}
		total += int(binary.LittleEndian.Uint16(fileData[off+disk.BankVoiceCountOffset : off+disk.BankVoiceCountOffset+2]))
	}
	if total > disk.MaxVoices {
		total = disk.MaxVoices
	}
	return total
}

// hasMultiDiskBoundaryVoice reports whether the voice area in fileData holds
// at least one plausible voice slot whose wavst (cumulative sample address)
// points past the local audio area. Such a slot corroborates the bank
// sector's BankTotalWaveOffset marker as a real disk-1-of-2 split rather
// than garbage.
//
// The walk mirrors fzfinfo.Parse: only IsPlausibleVoiceSlot candidates count,
// and wavst * BytesPerSample >= localAudioBytes means the slot's first sample
// sits at or past the end of this disk's audio, so its audio is on disk 2.
func hasMultiDiskBoundaryVoice(fileData []byte, voiceAreaStart, localAudioBytes, nvoice int) bool {
	for i := range nvoice {
		off := disk.VoiceSlotOffset(voiceAreaStart, i)
		if off+disk.VoiceHeaderUsed > len(fileData) {
			return false
		}
		slot := fileData[off : off+disk.VoiceHeaderUsed]
		if !disk.IsPlausibleVoiceSlot(slot) {
			continue
		}
		wavst := binary.LittleEndian.Uint32(slot[disk.VoiceWaveStartOffset : disk.VoiceWaveStartOffset+4])
		if int(wavst)*disk.BytesPerSample >= localAudioBytes {
			return true
		}
	}
	return false
}

// programNameFromPath derives the 12-byte on-disk directory name for a
// Type-5 "Program" file from its host filepath. Programs carry no name
// field in the file itself, so the on-disk name comes from the input file:
// take the basename, strip the extension, uppercase it, truncate to 12, and
// space-pad, so "evenlongername.bin" becomes "EVENLONGERNA".
func programNameFromPath(filePath string) [disk.LabelSize]byte {
	base := filepath.Base(filePath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.ToUpper(base)
	if len(base) > disk.LabelSize {
		base = base[:disk.LabelSize]
	}
	return disk.PadLabel(base)
}

// detectFile inspects fileData to determine its FZ type, name, and sector counts.
// Voice files are identified by a printable 12-byte name at offset 0xb2.
// Files that do not match are treated as full dumps.
//
// The full-dump heuristic (scanning for bank/voice name signatures at known
// offsets) addresses a real-world problem: FZF files found on the internet
// are commonly missing their layout numbers, so the counts must be
// reconstructed. The approach was documented by Jacob Vosmaer
// (https://blog.jacobvosmaer.nl/0057-fz-1-images/).
func detectFile(fileData []byte) (fileInfo, error) {
	if len(fileData) < 2 {
		return fileInfo{}, errors.New("file too small to detect type")
	}

	totalSectors := disk.SectorsNeeded(len(fileData))

	// A Type-5 "Program" file (FZ-1 expanded software, loaded at 0:6000h)
	// starts with a fixed 14-byte preamble: near-CALL to main, RETF back to
	// the firmware, plus a BRK 3 / INT 3 trampoline for ROM-call dispatch.
	// The file stores no name, so detectFile leaves it zero and Add fills it
	// from the host filepath basename. AddBytes callers pass an explicit
	// name and skip detection entirely.
	if disk.IsPlausibleProgramHeader(fileData) {
		return fileInfo{fileType: disk.TypeProgram}, nil
	}

	// A voice file has a printable 12-byte name at 0xb2 and a valid sample
	// rate index at 0xb1.
	if disk.IsPlausibleVoiceHeader(fileData) {
		var fi fileInfo
		copy(fi.name[:], fileData[disk.VoiceNameOffset:disk.VoiceNameOffset+disk.LabelSize])
		fi.fileType = disk.TypeVoice
		fi.nvoice = 1
		fi.nwave = max(totalSectors-1, 0)
		return fi, nil
	}

	fi := fileInfo{
		fileType: disk.TypeFullDump,
		name:     disk.PadLabel(disk.FullDumpName),
	}

	// Try to read the voice count directly from the bank sector header (bytes
	// 0-1 hold bstep, the voice count). This is reliable for FZFs produced by
	// fizzle and avoids the name-scan heuristic failing on voices with
	// non-printable names (e.g. after resampling to 9 kHz).
	if len(fileData) >= disk.SectorSize {
		bstep0 := int(binary.LittleEndian.Uint16(fileData[disk.BankVoiceCountOffset : disk.BankVoiceCountOffset+2]))
		if bstep0 > 0 && bstep0 <= disk.MaxVoices {
			// Real-world FZFs carry up to 8 bank sectors (the factory
			// Clarinet.fzf has 4), so walk the chain: a hardcoded bn of 1
			// leaves the firmware reading only bank 0 of a multi-bank dump.
			// nvoice sums each bank's bstep, since the voice area holds one
			// slot per bstep entry per bank.
			fi.nbank = fzutil.CountBankSectors(fileData)
			fi.nvoice = sumBankBSteps(fileData, fi.nbank)
			voiceSectors := disk.VoiceAreaSectors(fi.nvoice)
			fi.nwave = totalSectors - fi.nbank - voiceSectors
			if fi.nwave < 0 {
				fi.nwave = 0
			}

			applyMultiDiskMarker(fileData, &fi)
			return fi, nil
		}
	}

	fi = heuristicDetectFile(fileData, fi)
	if fi.nbank == 0 && fi.nvoice == 0 {
		return fileInfo{}, errors.New("file does not appear to be a voice (.fzv) or full dump (.fzf) file")
	}

	// The heuristic path also needs the multi-disk marker check. Disk-1 files
	// whose bank-0 bstep is zero or corrupt fall through to the heuristic,
	// and without this check their nwave would reflect only local audio
	// sectors, breaking the sampler's "Next disk?" prompt.
	applyMultiDiskMarker(fileData, &fi)
	return fi, nil
}

// applyMultiDiskMarker honours the multi-disk total-wave marker at
// BankTotalWaveOffset when it is present AND corroborated by at least one
// plausible voice slot whose wavst points past the local audio area.
// AssembleMultiDisk writes the total wave sector count (across both disks)
// at this offset so disk 1's DIS tail wn reflects total instrument size and
// the sampler prompts for disk 2.
//
// The FZ-1 firmware doesn't always write BankTotalWaveOffset, so the field
// is frequently garbage in real-world FZFs. The corroboration rule mirrors
// fzfinfo.Parse; see llm-wiki/topics/multi-disk-dumps.md. Both detection
// paths call this, so it is a no-op whenever the evidence falls short.
func applyMultiDiskMarker(fileData []byte, fi *fileInfo) {
	if fi.nbank == 0 || fi.nvoice == 0 {
		return
	}
	if len(fileData) < disk.BankTotalWaveOffset+4 {
		return
	}
	totalWave := int(binary.LittleEndian.Uint32(fileData[disk.BankTotalWaveOffset : disk.BankTotalWaveOffset+4]))
	if totalWave <= fi.nwave {
		return
	}
	voiceSectors := disk.VoiceAreaSectors(fi.nvoice)
	voiceAreaStart := fi.nbank * disk.SectorSize
	voiceAreaEnd := voiceAreaStart + voiceSectors*disk.SectorSize
	localAudioBytes := len(fileData) - voiceAreaEnd
	if localAudioBytes < 0 {
		localAudioBytes = 0
	}
	if hasMultiDiskBoundaryVoice(fileData, voiceAreaStart, localAudioBytes, fi.nvoice) {
		log.Debug().
			Int("disk_wave_sectors", fi.nwave).
			Int("total_wave_sectors", totalWave).
			Msg("multi-disk: using total wave count from bank sector")
		fi.nwave = totalWave
		return
	}
	log.Debug().
		Int("disk_wave_sectors", fi.nwave).
		Int("total_wave_sectors", totalWave).
		Msg("multi-disk: ignoring total wave marker (no corroborating boundary voice)")
}

// heuristicDetectFile scans sector-by-sector through an FZF file to
// reconstruct its layout (bank count, voice count, wave count) from content
// signatures, for legacy FZF files found on the internet whose bstep field
// is zeroed or corrupt.
//
// The FZF physical layout is always banks, then voices, then audio, so the
// scan is a three-phase state machine: the first voice sector stops the bank
// count, and the first audio sector stops the voice count. A bank sector
// carries a printable 12-byte name at BankNameOffset; a voice sector packs
// up to 4 256-byte voice headers, checked via the first slot's name at
// VoiceNameOffset; everything after the voice region is audio.
func heuristicDetectFile(data []byte, fi fileInfo) fileInfo {
	const (
		phaseBanks  = iota // scanning bank sectors
		phaseVoices        // scanning voice sectors (4 voices per sector)
		phaseAudio         // counting remaining sectors as audio
	)

	phase := phaseBanks
	totalSectors := disk.SectorsNeeded(len(data))

	for i := range totalSectors {
		lo := i * disk.SectorSize
		hi := min(lo+disk.SectorSize, len(data))
		sec := data[lo:hi]

		switch phase {
		case phaseBanks:
			if fi.nbank < disk.MaxBanks && isBankSector(sec) {
				fi.nbank++
				continue
			}
			phase = phaseVoices
			fallthrough

		case phaseVoices:
			if isVoiceSector(sec) {
				// countVoicesInSector adds up to VoicesPerSector (4) at a
				// time, so the cap has to be checked post-add: a pre-add
				// `nvoice < MaxVoices` guard lets 63 climb to 67. Move to
				// phaseAudio once the next sector would exceed the cap.
				if fi.nvoice+countVoicesInSector(sec) <= disk.MaxVoices {
					fi.nvoice += countVoicesInSector(sec)
					continue
				}
			}
			phase = phaseAudio
			fallthrough

		case phaseAudio:
			fi.nwave++
		}
	}
	return fi
}

func isBankSector(sec []byte) bool {
	return len(sec) >= disk.BankNameOffset+disk.LabelSize &&
		disk.IsPrintableName(sec[disk.BankNameOffset:disk.BankNameOffset+disk.LabelSize])
}

func isVoiceSector(sec []byte) bool {
	return disk.IsPlausibleVoiceHeader(sec)
}

// countVoicesInSector counts how many of the 4 voice slots in a sector have
// printable names. Voice headers are packed at 256-byte intervals.
func countVoicesInSector(sec []byte) int {
	n := 0
	for slot := range disk.VoicesPerSector {
		off := slot*disk.VoicePackSize + disk.VoiceNameOffset
		if off+disk.LabelSize > len(sec) {
			break
		}
		if disk.IsPrintableName(sec[off : off+disk.LabelSize]) {
			n++
		}
	}
	return n
}
