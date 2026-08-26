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
// disk. 'Z' is an arbitrary printable byte: it makes unused regions
// recognisable in a hex dump and tells a fresh image apart from one that has
// held data and been zeroed. The sampler never reads these bytes because the
// CAT marks the sectors as free.
const unformattedFillByte = 'Z'

// catInitialAlloc marks clusters 0 (label and CAT) and 1 (directory) as
// allocated in the CAT bitmap.
const catInitialAlloc = 0x03

// Format creates a new blank FZ series disk image at path with the given label.
// Labels longer than the sampler display are refused. The image is written
// atomically via a temporary file and rename.
func Format(path, label string) error {
	if label == "" {
		return fmt.Errorf("disk label must not be empty")
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf("%q is a directory, IMAGE must be a file path", path)
	}
	// Validate the complete label the user typed before creating anything.
	for _, r := range label {
		if r < disk.PrintableASCIIMin || r > disk.PrintableASCIIMax {
			return fmt.Errorf("disk label contains non-ASCII character %q (the sampler only supports printable ASCII)", string(r))
		}
	}
	if len(label) > disk.LabelSize {
		return fmt.Errorf("disk label %q exceeds %d characters", label, disk.LabelSize)
	}
	log.Info().
		Str("file", filepath.Base(path)).
		Str("label", label).
		Msg("creating disk image")
	log.Debug().
		Str("path", path).
		Str("size", fmt.Sprintf("%d bytes", disk.ImageSize)).
		Msg("disk image details")
	img, err := BuildImage(label)
	if err != nil {
		return err
	}
	if err := fileutil.WriteAtomic(path, img); err != nil {
		return err
	}
	return nil
}

// BuildImage constructs the raw bytes of a blank formatted disk image in
// memory. Unlike Format it never touches the filesystem and validates
// strictly: an empty, over-length, or non printable ASCII label is an error.
// Format and BuildImage deliberately share the same strict label contract.
func BuildImage(label string) ([]byte, error) {
	if label == "" {
		return nil, fmt.Errorf("disk label must not be empty")
	}
	if len(label) > disk.LabelSize {
		return nil, fmt.Errorf("disk label exceeds %d characters", disk.LabelSize)
	}
	for _, r := range label {
		if r < disk.PrintableASCIIMin || r > disk.PrintableASCIIMax {
			return nil, fmt.Errorf("disk label contains non-ASCII character %q (the sampler only supports printable ASCII)", string(r))
		}
	}
	return buildImage(label), nil
}

// buildImage constructs the raw bytes of a blank formatted disk image.
func buildImage(label string) []byte {
	img := make([]byte, disk.ImageSize)

	// Sector 0: label, FZ series identification tag, then the CAT bitmap.
	paddedLabel := disk.PadLabel(label)
	copy(img[disk.LabelOffset:disk.LabelOffset+disk.LabelSize], paddedLabel[:])

	img[disk.DiskNameTagOffset] = disk.DiskNameTag

	// Spec §1-2 marks bytes 0x10..0x1B as the Password field; the disk name
	// goes there to match factory FZ-1 disks (see disk.PasswordOffset).
	copy(img[disk.PasswordOffset:disk.PasswordOffset+disk.LabelSize], paddedLabel[:])

	img[disk.CATOffset] = catInitialAlloc

	// Mark clusters beyond the physical disk capacity as allocated so the
	// sampler never tries to use them.
	copy(img[disk.CATPhysicalEnd:disk.SectorSize], bytes.Repeat([]byte{0xff}, disk.SectorSize-disk.CATPhysicalEnd))

	// Sector 1: empty directory (already zero from make).

	// Sectors 2 onward: fill with 'Z' to indicate unformatted space.
	copy(img[disk.ReservedSectors*disk.SectorSize:], bytes.Repeat([]byte{unformattedFillByte}, disk.ImageSize-disk.ReservedSectors*disk.SectorSize))

	return img
}
