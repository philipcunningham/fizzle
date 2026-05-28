// Package render provides presentation and formatting helpers for terminal output.
package render

import (
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/philipcunningham/fizzle/pkg/disk"
)

var noteNames = [...]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// NoteName converts a MIDI note number to a human-readable note name (e.g. 60 yields "C4").
// Uses the convention that middle C (MIDI 60) is C4.
func NoteName(midi uint8) string {
	octave := int(midi)/disk.SemitonesPerOctave - 1
	return fmt.Sprintf("%s%d", noteNames[midi%disk.SemitonesPerOctave], octave)
}

// RateName returns a short display string for a sample rate index byte
// (e.g. index 0 yields "36k", 1 yields "18k", 2 yields "9k").
func RateName(idx uint8) string {
	if int(idx) < len(disk.SampleRates) {
		return fmt.Sprintf("%dk", disk.SampleRates[idx]/1000)
	}
	return "?"
}

// Printf writes formatted output to w, discarding any write error.
// Use this for terminal rendering where write failures are not actionable.
func Printf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format, args...) //nolint:errcheck
}

// Println writes a line to w, discarding any write error.
// Use this for terminal rendering where write failures are not actionable.
func Println(w io.Writer, args ...any) {
	fmt.Fprintln(w, args...) //nolint:errcheck
}

// NewTable creates a pre-configured table writer with the standard fizzle
// output style (light borders, default header format, no row separators).
func NewTable(w io.Writer) table.Writer {
	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.SetStyle(table.StyleLight)
	t.Style().Format.Header = text.FormatDefault
	t.Style().Options.SeparateRows = false
	return t
}

const (
	bytesPerKB = 1024
	bytesPerMB = 1024 * 1024
)

// FormatBytes returns a human-readable string for a byte count,
// using KB or MB as appropriate (e.g. "140.1 KB", "1.4 MB", "512 B").
func FormatBytes(b int) string {
	switch {
	case b >= bytesPerMB:
		return fmt.Sprintf("%.1f MB", float64(b)/bytesPerMB)
	case b >= bytesPerKB:
		return fmt.Sprintf("%.1f KB", float64(b)/bytesPerKB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
