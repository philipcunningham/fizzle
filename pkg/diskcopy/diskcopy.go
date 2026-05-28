// Package diskcopy implements the fizzle disk copy command. It copies a named
// file from one disk image to another.
package diskcopy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskget"
)

const primaryDisk uint8 = 0

// Copy extracts a named file from srcImg and writes it onto dstImg. The temp
// file used for the intermediate extraction is created in the same directory
// as dstImg so the atomic rename in diskget stays on the same filesystem.
func Copy(srcImg, name, dstImg string) error {
	tmp, err := os.CreateTemp(filepath.Dir(dstImg), "fizzle-diskcopy-*")
	if err != nil {
		return fmt.Errorf("diskcopy: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()              //nolint:errcheck
	defer os.Remove(tmpPath) //nolint:errcheck

	if err := diskget.Get(srcImg, name, tmpPath); err != nil {
		return fmt.Errorf("diskcopy: %w", err)
	}

	log.Info().Str("name", name).Str("src", filepath.Base(srcImg)).Str("dst", filepath.Base(dstImg)).Msg("copying file between disk images")
	return diskadd.Add(dstImg, tmpPath, primaryDisk)
}
