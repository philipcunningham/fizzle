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
	"github.com/philipcunningham/fizzle/pkg/diskfs"
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
	// SlotIndex is the 0-based physical directory slot, set on corrupt
	// rows only. Pass it directly to disk.Image.ClearDirectorySlot.
	SlotIndex *int `json:"slot_index,omitempty"`
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
	return ParseImage(img)
}

// ParseImage returns the listing for an in-memory disk image: the same
// result as Parse with no filesystem access.
func ParseImage(img *disk.Image) (*Listing, error) {
	parsed, err := diskfs.List(img)
	if err != nil {
		return nil, fmt.Errorf("disklist: %w", err)
	}
	listing := &Listing{Label: parsed.Label, Entries: make([]FileEntry, len(parsed.Entries)), FreeBytes: parsed.FreeBytes, TotalBytes: parsed.TotalBytes, UsedPct: parsed.UsedPct}
	for i, entry := range parsed.Entries {
		typeName := entry.Type.String()
		if entry.Corrupt {
			typeName = CorruptTypeName
			log.Warn().
				Str("name", entry.Name).
				Int("entry", entry.Index).
				Msg("disklist: " + entry.CorruptReason + "; marking entry as corrupt")
		}
		listing.Entries[i] = FileEntry{Index: entry.Index, Name: entry.Name, TypeName: typeName, Size: entry.Size, SlotIndex: entry.SlotIndex}
	}
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
