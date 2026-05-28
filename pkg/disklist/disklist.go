// Package disklist implements the fizzle disk ls command. It reads an FZ series
// disk image and returns its directory contents as structured data, with a
// separate renderer for terminal output.
package disklist

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/render"
)

// CorruptTypeName is the placeholder Type column value shown for directory
// entries whose DIS sector cannot be located or decoded. `disk ls` is the
// tool used to diagnose damaged disks, so a single bad entry must not hide
// every good entry behind it.
const CorruptTypeName = "(corrupt)"

// FileEntry describes a single file on the disk.
type FileEntry struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	TypeName string `json:"type"`
	Size     int    `json:"size"`
}

// Listing holds the parsed contents of a disk image.
type Listing struct {
	Label      string      `json:"label"`
	Entries    []FileEntry `json:"entries"`
	FreeBytes  int         `json:"free_bytes"`
	TotalBytes int         `json:"total_bytes"`
	UsedPct    int         `json:"used_percent"`
}

// Parse reads the disk image at path and returns its directory listing as structured data.
func Parse(path string) (*Listing, error) {
	img, err := disk.OpenImage(path)
	if err != nil {
		return nil, fmt.Errorf("disklist: %w", err)
	}

	entries, err := img.Directory()
	if err != nil {
		return nil, fmt.Errorf("disklist: reading directory: %w", err)
	}

	listing := &Listing{
		Label:   img.Label(),
		Entries: []FileEntry{},
	}

	for i, e := range entries {
		// `disk ls` is the diagnostic tool a user reaches for when a disk
		// looks broken: one bad entry must not hide all good entries. If
		// the DIS sector pointer is out of range or its contents fail to
		// decode, emit a placeholder row with TypeName="(corrupt)" and
		// continue with the next entry.
		if int(e.DisSector) < disk.ReservedSectors || int(e.DisSector) >= disk.SectorCount {
			log.Warn().
				Str("name", e.NameString()).
				Int("entry", i+1).
				Uint16("dis_sector", e.DisSector).
				Msg("disklist: directory entry points at reserved or out-of-range sector; marking as corrupt")
			listing.Entries = append(listing.Entries, FileEntry{
				Index:    i + 1,
				Name:     e.NameString(),
				TypeName: CorruptTypeName,
				Size:     0,
			})
			continue
		}
		// DIS sectors are decoded into a typed value and never mutated, so a
		// no-copy view is safe.
		disSec, err := img.SectorRef(int(e.DisSector))
		if err != nil {
			log.Warn().
				Err(err).
				Str("name", e.NameString()).
				Int("entry", i+1).
				Msg("disklist: failed to read DIS sector; marking entry as corrupt")
			listing.Entries = append(listing.Entries, FileEntry{
				Index:    i + 1,
				Name:     e.NameString(),
				TypeName: CorruptTypeName,
				Size:     0,
			})
			continue
		}
		dis, err := disk.DecodeDisSector(disSec)
		if err != nil {
			log.Warn().
				Err(err).
				Str("name", e.NameString()).
				Int("entry", i+1).
				Msg("disklist: failed to decode DIS sector; marking entry as corrupt")
			listing.Entries = append(listing.Entries, FileEntry{
				Index:    i + 1,
				Name:     e.NameString(),
				TypeName: CorruptTypeName,
				Size:     0,
			})
			continue
		}
		listing.Entries = append(listing.Entries, FileEntry{
			Index:    i + 1,
			Name:     e.NameString(),
			TypeName: e.FileType.String(),
			Size:     dis.PayloadSize(),
		})
	}

	free := img.FreeSectors()
	total := disk.SectorCount - disk.ReservedSectors
	listing.FreeBytes = free * disk.SectorSize
	listing.TotalBytes = total * disk.SectorSize
	listing.UsedPct = 100 * (total - free) / total

	return listing, nil
}

// RenderJSON writes the listing as indented JSON to w.
func RenderJSON(w io.Writer, listing *Listing) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(listing)
}

// Render writes a formatted directory listing to w.
func Render(w io.Writer, listing *Listing) {
	render.Printf(w, "Disk: %s\n\n", listing.Label)

	if len(listing.Entries) == 0 {
		render.Println(w, "  (empty)")
	} else {
		t := render.NewTable(w)
		t.AppendHeader(table.Row{"#", "Name", "Type", "Size"})
		for _, e := range listing.Entries {
			t.AppendRow(table.Row{e.Index, e.Name, e.TypeName, render.FormatBytes(e.Size)})
		}
		t.Render()
	}

	render.Printf(w, "\n%s free of %s (%d%% used)\n",
		render.FormatBytes(listing.FreeBytes),
		render.FormatBytes(listing.TotalBytes),
		listing.UsedPct,
	)
}

// List reads the disk image at path and writes a formatted directory listing to w.
func List(path string, w io.Writer) error {
	listing, err := Parse(path)
	if err != nil {
		return err
	}
	Render(w, listing)
	return nil
}
