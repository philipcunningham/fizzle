package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// showInfo pushes a modal with file-aggregate stats (spec §2.1 last
// paragraph). The body is computed on the fly from the current model
// state. Escape dismisses.
func (a *App) showInfo() {
	body := a.infoBody()

	view := tview.NewTextView()
	view.SetDynamicColors(true)
	view.SetText(body)
	view.SetBorder(true)
	view.SetTitle(" Info (Esc to close) ")

	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			a.stack.Pop()
			return nil
		}
		return event
	})

	// Centre the view inside a Flex so the modal feels modal-like even
	// though we use a TextView rather than tview.Modal (Modal can't
	// scroll, and an FZF with 64 voices needs scroll room).
	a.stack.Push(pageInfo, centreInBox(view, 70, 20))
}

// infoBody assembles the multi-line info text. Includes voice count,
// bank count, rate distribution, and free-slot counts.
func (a *App) infoBody() string {
	hdr := a.m.Header()
	bankCount := hdr.NBankSectors
	voiceCount := hdr.NVoice

	rateCounts := map[string]int{}
	for slot := 0; slot < voiceCount; slot++ {
		v, err := a.m.Voice(slot)
		if err != nil || v == nil {
			rateCounts["(error)"]++
			continue
		}
		rateCounts[fmt.Sprintf("%d kHz", v.SampleRate/1000)]++
	}

	var b strings.Builder
	fmt.Fprintf(&b, "File: %s\n\n", a.m.Path())
	fmt.Fprintf(&b, "Voices: %d\n", voiceCount)
	fmt.Fprintf(&b, "Banks:  %d\n", bankCount)
	fmt.Fprintf(&b, "Bytes:  %d  (%d KB)\n\n", len(a.m.Bytes()), len(a.m.Bytes())/bytesPerKB)

	// Bank-area counts per bank.
	b.WriteString("Bank areas:\n")
	for i := 0; i < bankCount; i++ {
		bank := a.m.Bytes()
		bankOff := i * disk.SectorSize
		var nAreas int
		if bankOff+disk.BankVoiceCountOffset+2 <= len(bank) {
			nAreas = int(bank[bankOff+disk.BankVoiceCountOffset]) |
				(int(bank[bankOff+disk.BankVoiceCountOffset+1]) << 8)
		}
		name := a.m.BankName(i)
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Fprintf(&b, "  %d %-12s  %d areas\n", i+1, name, nAreas)
	}

	// Sample-rate distribution, sorted by rate string for stable
	// ordering (the studio is rendered character-by-character so a
	// deterministic order matters for screenshot comparisons).
	b.WriteString("\nSample-rate distribution:\n")
	keys := make([]string, 0, len(rateCounts))
	for k := range rateCounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %-10s  %d voices\n", k, rateCounts[k])
	}

	return b.String()
}

// centreInBox wraps content in a Flex layout that horizontally and
// vertically centres it at the given dimensions. Used for the info
// and help overlays where tview.Modal's auto-sized layout is too
// constraining.
func centreInBox(content tview.Primitive, width, height int) tview.Primitive {
	row := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(content, height, 0, true).
		AddItem(nil, 0, 1, false)
	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(row, width, 0, true).
		AddItem(nil, 0, 1, false)
}
