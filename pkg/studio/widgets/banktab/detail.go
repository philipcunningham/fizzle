package banktab

import (
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/studio/helpers"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// rebindDetail rebuilds the area-detail editor for the currently
// selected area. Called from refresh (which sees the latest model
// bytes) and from the area-table SetSelectionChangedFunc callback.
//
// The editor is rebuilt from scratch each time rather than mutated in
// place: the per-row state involves several InputFields and a
// DropDown, and re-wiring their DoneFuncs to point at the new area
// index is easier than diffing.
func (w *Widget) rebindDetail() {
	w.detail.Clear()

	n := w.areaCount()
	if n == 0 {
		// Empty bank: nothing to bind. Leave the detail flex empty.
		return
	}

	area := w.selectedArea
	if area >= n {
		area = n - 1
	}
	if area < 0 {
		area = 0
	}

	bank := fzutil.BankSliceAt(w.m.Bytes(), w.bankIdx)
	if bank == nil {
		return
	}

	w.keyLow = w.makeNoteField("Key Low (C-1..G9)", bank[disk.BankKeyLowOffset+area], disk.BankKeyLowOffset, area)
	w.keyOrig = w.makeNoteField("Key Orig (C-1..G9)", bank[disk.BankKeyCentOffset+area], disk.BankKeyCentOffset, area)
	w.keyHigh = w.makeNoteField("Key High (C-1..G9)", bank[disk.BankKeyHighOffset+area], disk.BankKeyHighOffset, area)
	w.velLow = w.makeUnsignedField("Vel Low (0-127)", int(bank[disk.BankVelLowOffset+area]), 127, disk.BankVelLowOffset, area)
	w.velHigh = w.makeUnsignedField("Vel High (0-127)", int(bank[disk.BankVelHighOffset+area]), 127, disk.BankVelHighOffset, area)
	w.volume = w.makeUnsignedField("Volume (0-127)", int(bank[disk.BankVolumeOffset+area]), 127, disk.BankVolumeOffset, area)
	w.channel = w.makeChannelField("MIDI Channel (1-16)", int(bank[disk.BankMIDIRecvChanOffset+area])+1, area)
	w.output = w.makeOutputDropDown("Output", bank[disk.BankAudioOutOffset+area], area)

	w.detail.AddItem(w.keyLow, 1, 0, false)
	w.detail.AddItem(w.keyOrig, 1, 0, false)
	w.detail.AddItem(w.keyHigh, 1, 0, false)
	w.detail.AddItem(w.velLow, 1, 0, false)
	w.detail.AddItem(w.velHigh, 1, 0, false)
	w.detail.AddItem(w.volume, 1, 0, false)
	w.detail.AddItem(w.channel, 1, 0, false)
	w.detail.AddItem(w.output, 1, 0, false)
}

// makeNoteField builds a note-name InputField bound to the bank-sector
// byte at arrayOffset+area. On commit the typed text is parsed via
// helpers.ParseNote; invalid commits leave the byte unchanged (the
// accept-function makes invalid commits rare, but empty-string at
// commit time is still possible).
func (w *Widget) makeNoteField(label string, current uint8, arrayOffset, area int) *tview.InputField {
	f := tview.NewInputField()
	f.SetLabel(label + " ")
	f.SetText(helpers.FormatNote(current))
	f.SetFieldWidth(6)
	f.SetAcceptanceFunc(helpers.AcceptNote())
	f.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter && key != tcell.KeyTab && key != tcell.KeyBacktab {
			// Esc: revert.
			cur := w.readBankByte(arrayOffset, area)
			f.SetText(helpers.FormatNote(cur))
			return
		}
		v, err := helpers.ParseNote(f.GetText())
		if err != nil {
			// Restore committed value. The live accept-function should
			// already prevent most invalid commits, so we keep the
			// behaviour simple; the bank tab does not currently route
			// validation errors to a status line (a follow-up).
			cur := w.readBankByte(arrayOffset, area)
			f.SetText(helpers.FormatNote(cur))
			return
		}
		_ = w.applyByte(arrayOffset, area, v)
		f.SetText(helpers.FormatNote(v))
		w.handleDoneKey(key)
	})
	return f
}

// makeUnsignedField builds an InputField for a 0..hi unsigned byte at
// arrayOffset+area.
func (w *Widget) makeUnsignedField(label string, current, hi, arrayOffset, area int) *tview.InputField {
	f := tview.NewInputField()
	f.SetLabel(label + " ")
	f.SetText(strconv.Itoa(current))
	f.SetFieldWidth(6)
	f.SetAcceptanceFunc(helpers.AcceptUnsigned(hi))
	f.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter && key != tcell.KeyTab && key != tcell.KeyBacktab {
			cur := w.readBankByte(arrayOffset, area)
			f.SetText(strconv.Itoa(int(cur)))
			return
		}
		v, err := strconv.Atoi(f.GetText())
		if err != nil || v < 0 || v > hi {
			cur := w.readBankByte(arrayOffset, area)
			f.SetText(strconv.Itoa(int(cur)))
			return
		}
		_ = w.applyByte(arrayOffset, area, uint8(v)) //nolint:gosec // G115: bound-checked above
		f.SetText(strconv.Itoa(v))
		w.handleDoneKey(key)
	})
	return f
}

// makeChannelField builds the MIDI channel InputField. Displayed
// 1-16, stored 0-15; commit subtracts 1 before writing.
func (w *Widget) makeChannelField(label string, current, area int) *tview.InputField {
	f := tview.NewInputField()
	f.SetLabel(label + " ")
	f.SetText(strconv.Itoa(current))
	f.SetFieldWidth(4)
	f.SetAcceptanceFunc(helpers.AcceptUnsigned(16))
	f.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter && key != tcell.KeyTab && key != tcell.KeyBacktab {
			cur := int(w.readBankByte(disk.BankMIDIRecvChanOffset, area)) + 1
			f.SetText(strconv.Itoa(cur))
			return
		}
		v, err := strconv.Atoi(f.GetText())
		if err != nil || v < 1 || v > disk.MaxMIDIChannel {
			cur := int(w.readBankByte(disk.BankMIDIRecvChanOffset, area)) + 1
			f.SetText(strconv.Itoa(cur))
			return
		}
		_ = w.applyByte(disk.BankMIDIRecvChanOffset, area, uint8(v-1))
		f.SetText(strconv.Itoa(v))
		w.handleDoneKey(key)
	})
	return f
}

// makeOutputDropDown builds the gchn DropDown. The label set matches
// outputOptions (poly/1..8/all); on selection we parse the label via
// helpers.ParseOutput to get the bitmask byte and apply it.
func (w *Widget) makeOutputDropDown(label string, current uint8, area int) *tview.DropDown {
	d := tview.NewDropDown()
	d.SetLabel(label + " ")

	// Find the current option index. FormatOutput renders single-bit
	// masks as "1".."8" and 0xff as "all"; we prefer to display "poly"
	// when the byte is PolyphonicAudioOut, but ParseOutput accepts
	// both poly and all so the choice is cosmetic. The selected index
	// picks the canonical label.
	canonical := helpers.FormatOutput(current)
	if current == disk.PolyphonicAudioOut {
		canonical = "poly"
	}
	selected := 0
	for i, opt := range outputOptions {
		if opt == canonical {
			selected = i
			break
		}
	}

	// Install options + current selection FIRST without a handler so
	// SetCurrentOption doesn't fire a spurious write at construction
	// time (and on every rebindDetail). Attach the user-edit handler
	// only after the initial state is in place.
	d.SetOptions(outputOptions, nil)
	d.SetCurrentOption(selected)
	d.SetSelectedFunc(func(text string, _ int) {
		mask, err := helpers.ParseOutput(text)
		if err != nil {
			return
		}
		_ = w.applyByte(disk.BankAudioOutOffset, area, mask)
	})
	// Tab / Shift+Tab when the DropDown has focus (closed state) should
	// advance focus the same way an InputField commit does. tview's
	// DropDown fires SetDoneFunc on Enter/Tab/Backtab when closed; we
	// don't need to re-commit because SetSelectedFunc has already fired
	// for any selection change.
	d.SetDoneFunc(func(key tcell.Key) {
		w.handleDoneKey(key)
	})
	return d
}

// readBankByte returns the byte at bankIdx*SectorSize + arrayOffset +
// area, or 0 if out of range. Used by DoneFuncs to revert on bad
// input.
func (w *Widget) readBankByte(arrayOffset, area int) uint8 {
	bank := fzutil.BankSliceAt(w.m.Bytes(), w.bankIdx)
	off := arrayOffset + area
	if bank == nil || off >= len(bank) {
		return 0
	}
	return bank[off]
}

// applyByte writes one byte at the bank-sector offset
// bankIdx*SectorSize + arrayOffset + area via m.Apply.
func (w *Widget) applyByte(arrayOffset, area int, value uint8) error {
	patch := voiceedit.Patch{
		Offset: w.bankIdx*disk.SectorSize + arrayOffset + area,
		Size:   1,
		Value:  uint16(value),
	}
	return w.m.Apply(patch)
}
