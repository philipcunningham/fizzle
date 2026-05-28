// Package disk provides types and helpers for reading and writing FZ series
// floppy disk images. An image is exactly 1280 sectors of 1024 bytes each.
//
// Sector 0 holds the disk label and the Cluster Allocation Table (CAT). The
// CAT is a bitmap starting at byte 0x080: each bit represents one cluster
// (sector). A set bit means allocated; a clear bit means free. Sector 1 is
// the directory. Sectors 2 through 1279 hold file data.
//
// The directory is a sequence of 16-byte entries packed into sector 1. Each
// entry points to a Data Information Sector (DIS) which holds an extent table
// listing the (start, end) sector pairs that make up the file.
//
// File format details are documented in the Casio FZ-1 Data Structures
// reference.
package disk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Disk geometry and layout constants.
const (
	SectorSize  = 1024
	SectorCount = 1280
	ImageSize   = SectorSize * SectorCount

	// LabelOffset is the byte offset of the disk label in sector 0.
	LabelOffset = 0x000

	// DiskNameTagOffset is the byte offset of the disk-name identification tag in sector 0.
	DiskNameTagOffset = 0x00e

	// PasswordOffset is the byte offset of the 12-byte Password field per
	// spec §1-2 a) Disk ID. fizzle does not support password-protected
	// disks; the byte slot exists in the on-disk format and must be
	// occupied. Format() writes the disk name here (rather than zeros) to
	// match the byte pattern observed on real-world factory FZ-1 disks.
	PasswordOffset = 0x010

	// CATOffset is the byte offset of the CAT bitmap within sector 0.
	// Spec §1-2: the Disk ID occupies bytes 0..127, and the CAT bitmap is
	// 768 bytes from byte 128 onwards. fizzle uses only the first 160
	// bytes (1280 clusters / 8 bits per byte); the rest of the CAT region
	// covers cluster numbers that don't exist on a 1280-sector disk and is
	// filled with 0xFF at format time so the sampler never allocates them.
	CATOffset = 0x080

	// CATPhysicalEnd is the byte offset within sector 0 where the
	// beyond-physical-capacity region begins. Bytes from this offset to the
	// end of sector 0 are set to 0xff during formatting to prevent the sampler
	// from allocating clusters that do not exist on the physical disk.
	CATPhysicalEnd = 0x120

	// UsableDataSize is the maximum number of bytes available for file data on
	// a formatted FZ series disk. Sectors 0 (head) and 1 (directory) are
	// reserved, leaving 1278 sectors for data.
	UsableDataSize = (SectorCount - 2) * SectorSize

	// DirSector is the sector index of the directory.
	DirSector = 1

	// DirEntrySize is the size in bytes of a single directory entry.
	DirEntrySize = 16

	// MaxDirEntries is the maximum number of entries in the directory sector.
	MaxDirEntries = SectorSize / DirEntrySize

	// LabelSize is the fixed width of a disk label or file name in bytes.
	LabelSize = 12

	// MaxDisks is the maximum number of floppy disks the FZ series can load
	// in a single multi-disk session. The hardware has 2 MB of sample RAM;
	// two full disks is the most audio the sampler can ever hold in memory.
	MaxDisks = 2

	// MaxSampleRAM is the total sample memory available on the FZ series
	// hardware (2 MB). The sampler reports "no memory space" if the combined
	// audio across all disks exceeds this limit.
	MaxSampleRAM = 2 * 1024 * 1024

	// DiskNameTag is the byte written at offset 0x0e in sector 0 to identify
	// this as an FZ series disk image.
	DiskNameTag = 0x02

	// ReservedSectors is the number of sectors reserved for the disk head (sector 0)
	// and directory (sector 1), not available for file data.
	ReservedSectors = 2

	// ExtentEntrySize is the size in bytes of a single (start, end) sector pair
	// in the DIS extent table.
	ExtentEntrySize = 4

	// DBPAreaSize is the size in bytes of the dBP (data Block Pointer) area
	// in a DIS sector. Per spec §1-4, the file head consists of a 256-byte
	// dBP area followed by a 768-byte work area; only the dBP area holds
	// extent entries (the work area is not part of the extent list).
	DBPAreaSize = 256

	// MaxDBPEntries is the maximum number of (ss, es) extent pairs the dBP
	// area can hold. Spec §1-4: "There exists 64 dBP's in a dBP area".
	MaxDBPEntries = DBPAreaSize / ExtentEntrySize

	// DisTailOffset is the byte offset of the bank count field in the DIS sector tail.
	DisTailOffset = 0x3fa
	// DisVoiceCountOffset is the byte offset of the voice count field in the DIS sector tail.
	DisVoiceCountOffset = 0x3fc
	// DisWaveCountOffset is the byte offset of the wave count field in the DIS sector tail.
	DisWaveCountOffset = 0x3fe

	// FullDumpName is the canonical file name the FZ series firmware expects
	// for full dump entries on disk. Using any other name causes the sampler
	// to misidentify the file.
	FullDumpName = "FULL-DATA-FZ"

	// TypeFullDumpLabel is the human-readable label for a full dump file type.
	TypeFullDumpLabel = "Full Dump"
)

// FileType identifies the kind of file stored in a directory entry.
type FileType uint8

// File type codes as stored in directory entries.
const (
	TypeFullDump FileType = 0
	TypeVoice    FileType = 1
	TypeBank     FileType = 2
	TypeEffect   FileType = 3
	TypeSequence FileType = 4
	TypeProgram  FileType = 5
)

// ErrNotFound is returned when a named file does not exist in the directory.
var ErrNotFound = errors.New("disk: file not found")

// ValidateDiskNum converts a 1-based disk number to the 0-based byte stored
// in directory entries. n must be in [1, MaxDisks]; the FZ-1 spec describes
// at most a 2-disk save, bounded by the hardware's 2 MB of sample RAM (one
// full disk being 1.25 MB). See docs/casio-fz1-format.md "Multi-Disk Full
// Dumps" for the on-disk encoding.
func ValidateDiskNum(n int) (uint8, error) {
	if n < 1 {
		return 0, fmt.Errorf("disk: --disk-num must be 1 or greater (got %d)", n)
	}
	if n > MaxDisks {
		return 0, fmt.Errorf("disk: --disk-num must be 1 or %d (got %d)", MaxDisks, n)
	}
	return uint8(n - 1), nil //nolint:gosec // bounded 1..MaxDisks above
}

// ErrDirFull is returned when the directory has no free entry slots.
var ErrDirFull = errors.New("disk: directory is full")

// ErrNoSpace is returned when there are not enough free sectors for an allocation.
var ErrNoSpace = errors.New("disk: not enough free sectors")

// typeNames maps file type codes to human-readable display strings.
// Order matches the type constants: Full Dump, Voice, Bank, Effect, Sequence, Program.
var typeNames = [...]string{TypeFullDumpLabel, "Voice", "Bank", "Effect", "Sequence", "Program"}

// String returns a human-readable label for a file type code.
func (t FileType) String() string {
	if int(t) < len(typeNames) {
		return typeNames[t]
	}
	return fmt.Sprintf("Unknown(%d)", t)
}

// DirEntry represents a single 16-byte directory entry.
type DirEntry struct {
	Name      [LabelSize]byte
	FileType  FileType
	DiskNum   uint8
	DisSector uint16
}

// NameString returns the entry name trimmed of trailing spaces.
func (e DirEntry) NameString() string {
	return TrimPadded(e.Name[:])
}

// DecodeDirEntry decodes a 16-byte directory entry from b.
func DecodeDirEntry(b []byte) (DirEntry, error) {
	if len(b) < DirEntrySize {
		return DirEntry{}, errors.New("disk: buffer too small for directory entry")
	}
	var e DirEntry
	copy(e.Name[:], b[0:LabelSize])
	e.FileType = FileType(b[LabelSize])
	e.DiskNum = b[LabelSize+1]
	e.DisSector = binary.LittleEndian.Uint16(b[LabelSize+2 : LabelSize+4])
	return e, nil
}

// EncodeDirEntry encodes e into a 16-byte slice.
func EncodeDirEntry(e DirEntry) []byte {
	b := make([]byte, DirEntrySize)
	copy(b[0:LabelSize], e.Name[:])
	b[LabelSize] = uint8(e.FileType)
	b[LabelSize+1] = e.DiskNum
	binary.LittleEndian.PutUint16(b[LabelSize+2:LabelSize+4], e.DisSector)
	return b
}

// DisSector holds the extent table and tail counts for a file. It occupies
// one full sector. The extent table is a sequence of (start, end) 16-bit
// sector pair entries. Each entry covers a contiguous run of sectors.
// The last six bytes of the sector hold bank, voice, and wave sector counts
// at offsets 0x3fa, 0x3fc, and 0x3fe respectively.
//
// Note: the FZ-1 Data Structures document states the order as "vn bn wn" but
// the actual on-disk order written by the machine is "bn vn wn". This
// correction was documented by Jacob Vosmaer
// (https://blog.jacobvosmaer.nl/0057-fz-1-images/).
type DisSector struct {
	Extents    [][2]uint16
	BankCount  uint16
	VoiceCount uint16
	WaveCount  uint16
}

// FileSize returns the total size in bytes covered by the extents.
// Each extent covers (end - start + 1) sectors. This includes the
// DIS sector itself (prepended as ss0 of the first extent); for the
// extracted payload size that `disk get` produces, use PayloadSize.
func (d DisSector) FileSize() int {
	total := 0
	for _, ext := range d.Extents {
		if ext[1] >= ext[0] {
			total += (int(ext[1]) - int(ext[0]) + 1) * SectorSize
		}
	}
	return total
}

// PayloadSize returns the size in bytes of the file payload, excluding
// the DIS metadata sector. This matches what `disk get` writes when
// extracting the file, so `disk ls` and `disk get` agree on size.
func (d DisSector) PayloadSize() int {
	size := d.FileSize() - SectorSize
	if size < 0 {
		return 0
	}
	return size
}

// ErrCorruptDIS is returned when a DIS sector contains extent entries that
// point at reserved or out-of-range sectors. A corrupt DIS cannot be used
// safely: following its extents would read garbage from sector 0/1 (label
// or directory) or past the end of the disk.
var ErrCorruptDIS = errors.New("disk: corrupt DIS sector")

// DecodeDisSector decodes a data information sector from a 1024-byte buffer.
// Each extent's (start, end) pair is validated to lie within the data region
// (ReservedSectors..SectorCount). A corrupt DIS pointing at sector 0 or 1
// (reserved for label/directory) or past the end of the disk returns
// ErrCorruptDIS rather than silently reading garbage.
func DecodeDisSector(b []byte) (DisSector, error) {
	if len(b) < SectorSize {
		return DisSector{}, errors.New("disk: buffer too small for DIS sector")
	}
	var d DisSector
	// Per spec §1-4 the dBP area is 256 bytes (64 entries) and the
	// remaining 768 bytes of the file head are an unrelated work area.
	// Scan only the dBP area so we don't misread work-area bytes as
	// extra extents on third-party files.
	for i := 0; i+ExtentEntrySize <= DBPAreaSize; i += ExtentEntrySize {
		start := binary.LittleEndian.Uint16(b[i : i+2])
		end := binary.LittleEndian.Uint16(b[i+2 : i+4])
		if start == 0 && i > 0 {
			break
		}
		if start == 0 && end == 0 {
			break
		}
		if int(start) < ReservedSectors || int(end) < ReservedSectors ||
			int(start) >= SectorCount || int(end) >= SectorCount ||
			end < start {
			return DisSector{}, fmt.Errorf("%w: extent [%d,%d] out of range [%d,%d]", ErrCorruptDIS, start, end, ReservedSectors, SectorCount-1)
		}
		d.Extents = append(d.Extents, [2]uint16{start, end})
	}
	d.BankCount = binary.LittleEndian.Uint16(b[DisTailOffset:DisVoiceCountOffset])
	d.VoiceCount = binary.LittleEndian.Uint16(b[DisVoiceCountOffset:DisWaveCountOffset])
	d.WaveCount = binary.LittleEndian.Uint16(b[DisWaveCountOffset : DisWaveCountOffset+2]) //nolint:gosec // G602: bounds guaranteed by SectorSize check above
	return d, nil
}

// EncodeDisSector encodes d into a 1024-byte sector buffer.
func EncodeDisSector(d DisSector) []byte {
	b := make([]byte, SectorSize)
	for i, ext := range d.Extents {
		if i >= MaxDBPEntries {
			break
		}
		off := i * ExtentEntrySize
		binary.LittleEndian.PutUint16(b[off:off+2], ext[0])
		binary.LittleEndian.PutUint16(b[off+2:off+4], ext[1])
	}
	binary.LittleEndian.PutUint16(b[DisTailOffset:DisVoiceCountOffset], d.BankCount)
	binary.LittleEndian.PutUint16(b[DisVoiceCountOffset:DisWaveCountOffset], d.VoiceCount)
	binary.LittleEndian.PutUint16(b[DisWaveCountOffset:DisWaveCountOffset+2], d.WaveCount)
	return b
}

// Image is an in-memory representation of a complete FZ series disk image.
type Image struct {
	data [ImageSize]byte
}

// ReadImage reads a complete disk image from r. It returns an error if the
// image is not exactly ImageSize bytes.
func ReadImage(r io.Reader) (*Image, error) {
	img := &Image{}
	// Read one byte beyond ImageSize to detect oversized files.
	lr := &io.LimitedReader{R: r, N: int64(ImageSize) + 1}
	n, err := io.ReadFull(lr, img.data[:])
	if n != ImageSize {
		return nil, fmt.Errorf("disk: image must be exactly %d bytes, got %d", ImageSize, n)
	}
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("disk: reading image: %w", err)
	}
	var extra [1]byte
	if n, _ := lr.Read(extra[:]); n > 0 {
		return nil, fmt.Errorf("disk: image must be exactly %d bytes (file is larger)", ImageSize)
	}
	return img, nil
}

// OpenImage opens the disk image file at path and reads it into memory.
func OpenImage(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("disk: opening image: %w", err)
	}
	defer f.Close() //nolint:errcheck
	return ReadImage(f)
}

// Bytes returns a mutable view of the raw image data. Callers must not
// retain the slice across mutations to the Image.
func (img *Image) Bytes() []byte {
	return img.data[:]
}

// Sector returns a copy of sector n. Prefer SectorRef for read-only access
// to avoid the per-call 1024-byte allocation.
func (img *Image) Sector(n int) ([]byte, error) {
	if n < 0 || n >= SectorCount {
		return nil, fmt.Errorf("disk: sector %d out of range", n)
	}
	s := make([]byte, SectorSize)
	copy(s, img.data[n*SectorSize:(n+1)*SectorSize])
	return s, nil
}

// SectorRef returns a sub-slice into the image at sector n. It aliases the
// underlying storage and must NOT be mutated; the caller is responsible for
// not writing through the returned slice. Use SetSector to write a sector.
// Use this in hot read-only paths (directory walks, DIS decoding) to avoid
// allocating a fresh 1024-byte copy.
func (img *Image) SectorRef(n int) ([]byte, error) {
	if n < 0 || n >= SectorCount {
		return nil, fmt.Errorf("disk: sector %d out of range", n)
	}
	return img.data[n*SectorSize : (n+1)*SectorSize], nil
}

// SetSector writes b into sector n.
func (img *Image) SetSector(n int, b []byte) error {
	if n < 0 || n >= SectorCount {
		return fmt.Errorf("disk: sector %d out of range", n)
	}
	if len(b) != SectorSize {
		return fmt.Errorf("disk: sector data must be %d bytes", SectorSize)
	}
	copy(img.data[n*SectorSize:(n+1)*SectorSize], b)
	return nil
}

// Label returns the disk label from sector 0 offset 0x000, trimmed of padding.
func (img *Image) Label() string {
	return TrimPadded(img.data[0:LabelSize])
}

// Directory reads all non-empty directory entries from sector 1.
func (img *Image) Directory() ([]DirEntry, error) {
	var entries []DirEntry
	base := DirSector * SectorSize
	for i := range MaxDirEntries {
		off := base + i*DirEntrySize
		b := img.data[off : off+DirEntrySize]
		if b[0] == 0 {
			break
		}
		e, err := DecodeDirEntry(b)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// FreeSectors returns the number of unallocated data sectors on the disk.
func (img *Image) FreeSectors() int {
	free := 0
	for i := ReservedSectors; i < SectorCount; i++ {
		if !img.CATAllocated(i) {
			free++
		}
	}
	return free
}

// CATAllocated reports whether sector n is marked allocated in the CAT bitmap.
// Returns false for out-of-range sector numbers. This is safe because
// FreeSectors and AllocateSectors only iterate valid ranges (2..SectorCount-1),
// so returning false for invalid input simplifies iteration without masking bugs.
func (img *Image) CATAllocated(n int) bool {
	if n < 0 || n >= SectorCount {
		return false
	}
	byteIdx := CATOffset + n/8
	bitIdx := uint(n % 8)
	return img.data[byteIdx]&(1<<bitIdx) != 0
}

// CATSetAllocated marks sector n as allocated in the CAT bitmap.
func (img *Image) CATSetAllocated(n int) error {
	if n < 0 || n >= SectorCount {
		return fmt.Errorf("disk: sector %d out of range [0, %d)", n, SectorCount)
	}
	byteIdx := CATOffset + n/8
	bitIdx := uint(n % 8)
	img.data[byteIdx] |= 1 << bitIdx
	return nil
}

// CATClearAllocated marks sector n as free in the CAT bitmap.
func (img *Image) CATClearAllocated(n int) error {
	if n < 0 || n >= SectorCount {
		return fmt.Errorf("disk: sector %d out of range [0, %d)", n, SectorCount)
	}
	byteIdx := CATOffset + n/8
	bitIdx := uint(n % 8)
	img.data[byteIdx] &^= 1 << bitIdx
	return nil
}

// RemoveFile removes a named file from the disk image. It frees the file's
// sectors in the CAT bitmap and compacts the directory so there are no gaps.
// The match is case-insensitive.
func (img *Image) RemoveFile(name string) error {
	entries, err := img.Directory()
	if err != nil {
		return fmt.Errorf("disk: reading directory: %w", err)
	}

	idx := -1
	for i, e := range entries {
		if strings.EqualFold(e.NameString(), name) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}

	// Reject directory entries whose DIS pointer lands in the reserved
	// range (sector 0 = label/CAT, sector 1 = directory per spec §1-1/§1-2)
	// or past the end of the disk. Without this guard, DecodeDisSector
	// would mis-parse label/directory bytes as a DIS, returning fake
	// extents that drive CATClearAllocated calls against arbitrary sectors,
	// and CATClearAllocated(0)/(1) would clear the reserved bits,
	// corrupting the CAT bitmap. Sibling readers in pkg/disklist and
	// pkg/diskget enforce the same bound; this closes the unpatched path.
	if int(entries[idx].DisSector) < ReservedSectors || int(entries[idx].DisSector) >= SectorCount {
		return fmt.Errorf("%w: directory entry %q DIS sector %d out of range [%d,%d)",
			ErrCorruptDIS, entries[idx].NameString(), entries[idx].DisSector, ReservedSectors, SectorCount)
	}

	disSec, err := img.SectorRef(int(entries[idx].DisSector))
	if err != nil {
		return fmt.Errorf("disk: reading DIS sector: %w", err)
	}
	dis, err := DecodeDisSector(disSec)
	if err != nil {
		return fmt.Errorf("disk: decoding DIS: %w", err)
	}

	if err := img.CATClearAllocated(int(entries[idx].DisSector)); err != nil {
		return err
	}

	for _, ext := range dis.Extents {
		for s := int(ext[0]); s <= int(ext[1]); s++ {
			if err := img.CATClearAllocated(s); err != nil {
				return err
			}
		}
	}

	base := DirSector * SectorSize
	removeOff := base + idx*DirEntrySize
	for i := range DirEntrySize {
		img.data[removeOff+i] = 0
	}

	for i := idx; i < len(entries)-1; i++ {
		srcOff := base + (i+1)*DirEntrySize
		dstOff := base + i*DirEntrySize
		copy(img.data[dstOff:dstOff+DirEntrySize], img.data[srcOff:srcOff+DirEntrySize])
	}
	lastOff := base + (len(entries)-1)*DirEntrySize
	for i := range DirEntrySize {
		img.data[lastOff+i] = 0
	}

	return nil
}

// AllocateSectors finds count free sectors starting from sector 2, marks them
// allocated in the CAT bitmap, and returns their indices. Returns an error if
// there is insufficient free space.
func (img *Image) AllocateSectors(count int) ([]int, error) {
	free := make([]int, 0, count)
	for i := 2; i < SectorCount && len(free) < count; i++ {
		if !img.CATAllocated(i) {
			free = append(free, i)
		}
	}
	if len(free) < count {
		return nil, fmt.Errorf("%w (need %d, found %d)", ErrNoSpace, count, len(free))
	}
	for _, s := range free {
		if err := img.CATSetAllocated(s); err != nil {
			return nil, err
		}
	}
	return free, nil
}

// NextFreeDirSlot returns the byte offset in the image of the next empty
// directory entry slot, or an error if the directory is full.
func (img *Image) NextFreeDirSlot() (int, error) {
	base := DirSector * SectorSize
	for i := range MaxDirEntries {
		off := base + i*DirEntrySize
		if img.data[off] == 0 {
			return off, nil
		}
	}
	return 0, ErrDirFull
}

// PadLabel returns s padded with spaces to exactly LabelSize bytes, or
// truncated if longer.
func PadLabel(s string) [LabelSize]byte {
	var b [LabelSize]byte
	for i := range b {
		b[i] = ' '
	}
	copy(b[:], s)
	return b
}

// IsPrintableName reports whether all bytes in b are printable ASCII.
// The FZ series identifies valid name fields by checking that every byte
// falls within the printable ASCII range (space through tilde, 0x20–0x7e).
func IsPrintableName(b []byte) bool {
	for _, c := range b {
		if !isPrintableASCII(c) {
			return false
		}
	}
	return true
}

// Printable ASCII range used for label and name validation.
const (
	PrintableASCIIMin = 0x20
	PrintableASCIIMax = 0x7e
)

func isPrintableASCII(c byte) bool {
	return c >= PrintableASCIIMin && c <= PrintableASCIIMax
}

// SectorsNeeded returns the number of sectors required to hold byteLen bytes.
func SectorsNeeded(byteLen int) int {
	return (byteLen + SectorSize - 1) / SectorSize
}

// PadToSector rounds n up to the next multiple of SectorSize.
// Returns n unchanged if already aligned.
func PadToSector(n int) int {
	if rem := n % SectorSize; rem != 0 {
		return n + SectorSize - rem
	}
	return n
}

// TrimPadded returns the string in b with trailing spaces removed.
func TrimPadded(b []byte) string {
	n := len(b)
	for n > 0 && b[n-1] == ' ' {
		n--
	}
	return string(b[:n])
}

// ProgramHeaderMinLen is the minimum size of a recognisable FZ-1 Program file
// (Type 5). It matches the standard 14-byte expanded-software preamble that
// every Program on the factory OPT_SOFTWARE diagnostic disk starts with.
const ProgramHeaderMinLen = 14

// IsPlausibleProgramHeader reports whether data starts with the byte pattern
// of a Casio FZ-1 expanded-software ("Program", Type 5) file. The FZ-1
// firmware FAR-CALLs 0:6000h for any Program file and expects a small
// preamble that immediately near-CALLs the program's main, with a RETF
// positioned to return control to the firmware when main eventually returns.
//
// Observed real-world variants on OPT_SOFTWARE.img:
//
//	standard:    E8 ?? ??       CB 8F 06 ?? ?? CC FF 36 ?? ?? C3   (CKMIDI etc)
//	with STI:    E8 ?? ?? FB    CB 8F 06 ?? ?? CC FF 36 ?? ??       (ONBOARDKEY)
//
// We require byte 0 == 0xE8 (near CALL) AND a 0xCB (RETF) at either offset 3
// or offset 4. Those two positions cover every program seen on the factory
// disk. Voice and full-dump files do not start with 0xE8, so this signature
// safely distinguishes Programs from the other file types.
func IsPlausibleProgramHeader(data []byte) bool {
	if len(data) < ProgramHeaderMinLen {
		return false
	}
	if data[0] != 0xE8 {
		return false
	}
	return data[3] == 0xCB || data[4] == 0xCB
}
