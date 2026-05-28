package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

const bytesPerKB = 1024

// headerBar renders the 3-line header at the top of the studio
// window. Spec §2.1 fields: file basename, file type, bank count, voice
// count, [modified] indicator, PCM/Total sizes, hotkey reminders,
// version. The widget subscribes to model changes via the app shell so
// the [modified] indicator updates as soon as Apply / Undo / Save lands.
type headerBar struct {
	view *tview.TextView
	m    *model.Model
}

// newHeaderBar builds the header from m's current state. The caller is
// responsible for subscribing m.Subscribe -> Refresh (typically the
// owning App).
func newHeaderBar(m *model.Model) *headerBar {
	h := &headerBar{view: tview.NewTextView(), m: m}
	h.view.SetDynamicColors(true)
	h.view.SetTextColor(tview.Styles.PrimaryTextColor)
	h.Refresh()
	return h
}

// Primitive exposes the underlying TextView.
func (h *headerBar) Primitive() tview.Primitive { return h.view }

// Refresh redraws the header text from the model's current state.
func (h *headerBar) Refresh() {
	h.view.SetText(h.render())
}

// render formats the three header lines. Format follows spec §2.1
// verbatim, including the trailing version on line 3.
func (h *headerBar) render() string {
	hdr := h.m.Header()
	base := filepath.Base(h.m.Path())
	fileType := fileTypeLabel(h.m.Path())
	mod := ""
	if h.m.IsDirty() {
		mod = "  [yellow][modified][-]"
	}
	pcmKB, totalKB := h.byteCounts()
	line1 := fmt.Sprintf("File: %s  Type: %s  Banks: %d  Voices: %d%s",
		base, fileType, hdr.NBankSectors, hdr.NVoice, mod)
	line2 := fmt.Sprintf("PCM: %d KB        Total: %d KB", pcmKB, totalKB)
	// Version + branding live in the Ctrl+H help overlay so this line
	// stays compact. tview with SetDynamicColors(true) treats
	// unbracket-escaped "[X]" as a style tag and eats it; double-
	// bracket "[X[]" renders as literal "[X]" (per tview docs).
	line3 := "[Ctrl+S[]ave  [Ctrl+Q[]uit  [Ctrl+Z[]Undo  [Ctrl+I[]Info  [Ctrl+H[]Help"
	return strings.Join([]string{line1, line2, line3}, "\n")
}

// byteCounts returns PCM size (the audio area in KB) and total file
// size in KB. The PCM area starts at the voice area end and runs to
// end-of-file.
func (h *headerBar) byteCounts() (int, int) {
	hdr := h.m.Header()
	b := h.m.Bytes()
	total := len(b)
	voiceSectors := disk.VoiceAreaSectors(hdr.NVoice)
	voiceAreaEnd := hdr.VoiceAreaStart + voiceSectors*disk.SectorSize
	pcm := total - voiceAreaEnd
	if pcm < 0 {
		pcm = 0
	}
	return pcm / bytesPerKB, total / bytesPerKB
}

// fileTypeLabel converts a path's extension into the spec §2.1 file
// type string. Today the studio only edits full dumps and standalone
// voice files, but the .img full-dump-extraction path produces the
// same in-memory shape as a .fzf so we display "Full Dump" there.
func fileTypeLabel(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".fzv":
		return "Voice"
	case ".fzb":
		return "Bank"
	case ".img":
		return "Full Dump (.img)"
	default:
		return "Full Dump"
	}
}
