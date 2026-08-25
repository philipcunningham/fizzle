// Package diskget implements the fizzle disk get command. It extracts a named
// file from an FZ series disk image and writes it to a local path.
package diskget

import (
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
)

// Get reads the file named name from the disk image at imagePath and writes
// it to outputPath. The match is case-insensitive and trims padding. Returns
// an error if no matching entry is found.
func Get(imagePath, name, outputPath string) error {
	img, err := disk.OpenImage(imagePath)
	if err != nil {
		return fmt.Errorf("diskget: %w", err)
	}

	raw, err := FromImage(img, name)
	if err != nil {
		return err
	}

	log.Info().
		Str("name", name).
		Str("output", filepath.Base(outputPath)).
		Str("size", fmt.Sprintf("%d bytes", len(raw))).
		Msg("extracting from disk")
	log.Debug().
		Str("path", imagePath).
		Msg("disk image")
	if err := fileutil.WriteAtomic(outputPath, raw); err != nil {
		return fmt.Errorf("diskget: %w", err)
	}
	return nil
}

// FromImage extracts the named file's bytes from an in-memory disk
// image: the same result as Get with no filesystem access.
func FromImage(img *disk.Image, name string) ([]byte, error) {
	raw, err := diskfs.Extract(img, name)
	if err != nil {
		return nil, fmt.Errorf("diskget: %w", err)
	}
	return raw, nil
}

func extractFileBytes(img *disk.Image, dis disk.DisSector, disSectorLoc int) ([]byte, error) {
	// Walk the extents once for the exact final byte count so raw is
	// allocated once, then copy through SectorRef to avoid a temporary
	// 1024-byte allocation per sector.
	total := 0
	for i, ext := range dis.Extents {
		start := int(ext[0])
		end := int(ext[1])
		if i == 0 && start == disSectorLoc {
			start++
		}
		if end >= start {
			total += (end - start + 1) * disk.SectorSize
		}
	}
	raw := make([]byte, 0, total)
	for i, ext := range dis.Extents {
		start := int(ext[0])
		end := int(ext[1])
		if i == 0 && start == disSectorLoc {
			start++
		}
		for sec := start; sec <= end; sec++ {
			b, err := img.SectorRef(sec)
			if err != nil {
				return nil, fmt.Errorf("reading sector %d: %w", sec, err)
			}
			raw = append(raw, b...)
		}
	}
	return raw, nil
}
