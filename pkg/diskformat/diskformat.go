// Package diskformat implements the fizzle disk new command. It creates a
// blank FZ series floppy disk image with the given label.
package diskformat

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/rs/zerolog/log"
)

// unformattedFillByte fills all non-reserved sectors of a freshly formatted
// disk. 'Z' is an arbitrary printable byte: it makes unused regions instantly
// recognisable in a hex dump and distinguishes a fresh image from one that
// has held data and been zeroed. The sampler never reads these bytes because
// the CAT marks the sectors as free.
const unformattedFillByte = 'Z'

// catInitialAlloc marks clusters 0 and 1 as allocated in the CAT bitmap.
const catInitialAlloc = 0x03

// Format creates a new blank FZ series disk image at path with the given label.
// The label is padded or truncated to 12 bytes. The image is written
// atomically via a temporary file and rename.
func Format(path, label string) error {
	if label == "" {
		return fmt.Errorf("diskformat: disk label must not be empty")
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf("diskformat: %q is a directory, IMAGE must be a file path", path)
	}
	for _, r := range label {
		if r < disk.PrintableASCIIMin || r > disk.PrintableASCIIMax {
			return fmt.Errorf("diskformat: disk label contains non-ASCII character %q (the sampler only supports printable ASCII)", string(r))
		}
	}
	if len(label) > disk.LabelSize {
		log.Warn().
			Str("label", label).
			Int("limit", disk.LabelSize).
			Msgf("diskformat: disk label truncated to %d characters", disk.LabelSize)
		label = label[:disk.LabelSize]
	}
	log.Info().
		Str("file", filepath.Base(path)).
		Str("label", label).
		Msg("creating disk image")
	log.Debug().
		Str("path", path).
		Str("size", fmt.Sprintf("%d bytes", disk.ImageSize)).
		Msg("disk image details")
	img := buildImage(label)
	if err := fileutil.WriteAtomic(path, img); err != nil {
		return fmt.Errorf("diskformat: %w", err)
	}
	return nil
}

// buildImage constructs the raw bytes of a blank formatted disk image.
func buildImage(label string) []byte {
	img := make([]byte, disk.ImageSize)

	// Sector 0: label at LabelOffset, padded to 12 bytes.
	paddedLabel := disk.PadLabel(label)
	copy(img[disk.LabelOffset:disk.LabelOffset+disk.LabelSize], paddedLabel[:])

	// Disk name tag at DiskNameTagOffset identifies this as an FZ series image.
	img[disk.DiskNameTagOffset] = disk.DiskNameTag

	// Spec §1-2 marks bytes 0x10..0x1B as the Password field. fizzle does
	// not support password protection; we write the disk name into this
	// slot to match the byte pattern that factory FZ-1 disks carry. The
	// FZ-1 firmware ignores this slot on unprotected disks.
	copy(img[disk.PasswordOffset:disk.PasswordOffset+disk.LabelSize], paddedLabel[:])

	// CAT bitmap starts at CATOffset. Byte 0 has bits 0 and 1 set, marking
	// clusters 0 and 1 (the label/CAT sector and the directory sector) as
	// allocated.
	img[disk.CATOffset] = catInitialAlloc

	// Mark clusters beyond the physical disk capacity as allocated so the
	// sampler never tries to use them.
	copy(img[disk.CATPhysicalEnd:disk.SectorSize], bytes.Repeat([]byte{0xff}, disk.SectorSize-disk.CATPhysicalEnd))

	// Sector 1: empty directory (already zero from make).

	// Sectors 2 onward: fill with 'Z' to indicate unformatted space.
	copy(img[disk.ReservedSectors*disk.SectorSize:], bytes.Repeat([]byte{unformattedFillByte}, disk.ImageSize-disk.ReservedSectors*disk.SectorSize))

	return img
}
