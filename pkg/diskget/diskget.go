// Package diskget implements the fizzle disk get command. It extracts a named
// file from an FZ series disk image and writes it to a local path.
package diskget

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
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

	entries, err := img.Directory()
	if err != nil {
		return fmt.Errorf("diskget: %w", err)
	}

	var match *disk.DirEntry
	for i := range entries {
		// FZ labels are ASCII-only, so strings.EqualFold is sufficient.
		if strings.EqualFold(entries[i].NameString(), name) {
			match = &entries[i]
			break
		}
	}
	if match == nil {
		return fmt.Errorf("diskget: %q: %w", name, disk.ErrNotFound)
	}

	if int(match.DisSector) < disk.ReservedSectors || int(match.DisSector) >= disk.SectorCount {
		return fmt.Errorf("diskget: %q: directory entry DIS sector %d is out of range [%d,%d)", name, match.DisSector, disk.ReservedSectors, disk.SectorCount)
	}
	disSec, err := img.SectorRef(int(match.DisSector))
	if err != nil {
		return fmt.Errorf("diskget: reading DIS sector: %w", err)
	}
	dis, err := disk.DecodeDisSector(disSec)
	if err != nil {
		return fmt.Errorf("diskget: decoding DIS sector: %w", err)
	}
	if len(dis.Extents) == 0 {
		return fmt.Errorf("diskget: %q has no extents", name)
	}

	// FZ disks (whether written by the original sampler or by fizzle's
	// diskadd.buildDIS) place the DIS sector itself as ss0 of the first
	// extent, with the actual file payload starting at ss0+1. If we ever
	// encounter a disk where the DIS sector is stored outside the first
	// extent, extractFileBytes simply copies every extent byte (no skip).
	raw, err := extractFileBytes(img, dis, int(match.DisSector))
	if err != nil {
		return fmt.Errorf("diskget: %w", err)
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

func extractFileBytes(img *disk.Image, dis disk.DisSector, disSectorLoc int) ([]byte, error) {
	// Walk the extent list once to compute the exact final byte count so the
	// output buffer is allocated once. Then copy each sector via the no-copy
	// SectorRef view: the bytes are still copied into raw, but the temporary
	// 1024-byte per-sector allocation is eliminated.
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
