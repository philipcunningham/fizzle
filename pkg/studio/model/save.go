package model

import (
	"fmt"
	"os"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
)

// saveToImage writes the in-memory FZF bytes back into the source .img
// disk image. For multi-disk dumps (companion != ""), both images are
// patched and the pair is committed via the transactional dual-disk
// helper so a partial failure rolls the primary back to its pre-write
// bytes.
//
// Dual-disk invariant: write the primary first, then the companion,
// both under cross-process file locks acquired in deterministic order;
// roll the primary back if the companion write fails. This keeps the
// on-disk pair consistent under partial failure (without it a crash
// between writes can leave the two images describing different states
// of the same instrument).
func (m *Model) saveToImage() error {
	primary := m.imgPath
	companion := m.companion
	return m.withImageLocks(primary, companion, func() error {
		return m.saveToImageLocked(primary, companion)
	})
}

func (m *Model) withImageLocks(primary, companion string, fn func() error) error {
	if companion == "" || companion == primary {
		return fileutil.WithFileLock(primary, fn)
	}
	first, second := primary, companion
	if second < first {
		first, second = second, first
	}
	return fileutil.WithFileLock(first, func() error {
		return fileutil.WithFileLock(second, fn)
	})
}

func (m *Model) saveToImageLocked(primary, companion string) error {
	primaryDiskNum := uint8(0)
	if companion != "" {
		primaryDiskNum = 1
	}

	// Open the primary image and patch the in-memory copy. ReplaceInMemory
	// owns the snapshot/rollback for the image-level transaction (F8).
	primaryImg, err := disk.OpenImage(primary)
	if err != nil {
		return fmt.Errorf("model: opening primary image: %w", err)
	}
	if err := diskadd.ReplaceInMemory(primaryImg, m.diskName, m.bytes, primaryDiskNum); err != nil {
		return fmt.Errorf("model: replacing on primary: %w", err)
	}

	if companion == "" {
		return fileutil.WriteAtomic(primary, primaryImg.Bytes())
	}

	companionImg, err := disk.OpenImage(companion)
	if err != nil {
		return fmt.Errorf("model: opening companion image: %w", err)
	}
	// Companion disk holds the same FZF body. ReplaceInMemory uses
	// diskNum=0 (the companion's own disk number).
	if err := diskadd.ReplaceInMemory(companionImg, m.diskName, m.bytes, 0); err != nil {
		return fmt.Errorf("model: replacing on companion: %w", err)
	}
	return writeImagePairAtomic(primary, companion, primaryImg.Bytes(), companionImg.Bytes())
}

// writeImagePairAtomic commits two related disk images so the on-disk
// pair stays consistent under partial failure: either both writes
// succeed, or the primary is rolled back to its pre-write bytes.
func writeImagePairAtomic(primaryPath, companionPath string, primaryData, companionData []byte) error {
	primarySnapshot, err := os.ReadFile(primaryPath)
	if err != nil {
		return fmt.Errorf("model: reading primary for rollback snapshot: %w", err)
	}
	if err := fileutil.WriteAtomic(primaryPath, primaryData); err != nil {
		return err
	}
	if err := fileutil.WriteAtomic(companionPath, companionData); err != nil {
		if rbErr := fileutil.WriteAtomic(primaryPath, primarySnapshot); rbErr != nil {
			return fmt.Errorf("model: companion write failed: %w; primary rollback also failed: %w; image pair may be inconsistent at %q and %q", err, rbErr, primaryPath, companionPath)
		}
		return fmt.Errorf("model: writing companion (primary rolled back): %w", err)
	}
	return nil
}
