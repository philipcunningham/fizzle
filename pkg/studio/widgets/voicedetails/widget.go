// Package voicedetails implements the Voice Details panel used by the lower
// section's first tab in fizzle studio (spec §2.3.1).
//
// The panel is a three-column Flex (DCA | DCF | LFO) plus a footer with the
// voice number, name InputField, and playback-mode DropDown. Every editable
// field uses tview's InputField acceptance functions for live char-by-char
// validation (spec §3) and commits via the relevant voiceedit.BuildXPatches
// helper on Tab / Enter.
//
// The widget binds to a Model. Bind(slot) repopulates every InputField /
// DropDown from the voice at the given slot; the app shell calls it when
// the voicelist's selection changes.
package voicedetails

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/helpers"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// Header row count for the envelope-stage tables (one header row above the
// 8 stage rows).
const envHeaderRows = 1

// Envelope kind tags. The Widget routes commits to BuildDCAPatches /
// BuildDCFPatches based on these tags so each envelope's editors don't
// need their own commit helpers.
const (
	envKindDCA = "dca"
	envKindDCF = "dcf"
)

// LFO waveform options exposed in the DropDown, in CLI vocabulary order
// matching disk.LFO* indices.
var waveformOptions = []string{"sine", "saw-up", "saw-down", "triangle", "rectangle", "random"}

// Playback-mode options exposed in the footer DropDown, in CLI vocabulary.
var playbackOptions = []string{"normal", "reverse", "cue", "synth"}

// Widget owns the Voice Details panel. Construct via New, then call Bind to
// point it at a voice slot.
type Widget struct {
	root *tview.Flex
	m    *model.Model

	slot int

	// currentSection tracks which "section" of the panel focus is in,
	// used by CycleSection (Shift+Tab) to advance forward with wrap.
	// 0 = DCA column, 1 = DCF column, 2 = LFO column, 3 = footer.
	currentSection int

	// Envelope sub-widgets (DCA and DCF share the same shape).
	dca envelopeFields
	dcf envelopeFields

	// DCF static-filter extras (not present in DCA).
	cutoff    *tview.InputField
	resonance *tview.InputField
	resVel    *tview.InputField

	// LFO column.
	lfoWave        *tview.DropDown
	lfoPhaseSync   *tview.Checkbox
	lfoDelay       *tview.InputField
	lfoRate        *tview.InputField
	lfoAttack      *tview.InputField
	lfoDepthPitch  *tview.InputField
	lfoDepthAmp    *tview.InputField
	lfoDepthFilter *tview.InputField
	lfoDepthQ      *tview.InputField

	// Footer.
	voiceLabel   *tview.TextView
	nameField    *tview.InputField
	playbackMode *tview.DropDown

	// Error callback wired by the app shell once it owns a status line.
	onError func(error)

	// Suppress commit handlers while Bind is repopulating widgets (so
	// SetText / DropDown.SetCurrentOption don't trigger spurious patches).
	binding bool

	// tApp is set via SetApp by the app shell so commitOnDone +
	// non-InputField SetInputCapture handlers can call tApp.SetFocus
	// when Tab advances field-by-field. nil before SetApp is called;
	// the focus-advance helpers no-op safely in that case.
	tApp *tview.Application

	// onCycleOut is invoked by CycleSection when the cycle would wrap
	// past the last section. The app shell wires this to focusUpperPane
	// so Shift+Tab from the footer hops out into the upper pane (and
	// then continues round on the next press). nil before SetOnCycleOut
	// is called; CycleSection falls back to in-widget wraparound.
	onCycleOut func()
}

// SetApp injects the tview.Application so the widget can advance focus
// on Tab. Call once after construction, before the user interacts.
func (w *Widget) SetApp(tApp *tview.Application) { w.tApp = tApp }

// SetOnCycleOut registers the cross-pane cycle continuation. Called by
// CycleSection when it would wrap from the last section back to the
// first; the app uses this to extend the Shift+Tab cycle into the
// upper pane.
func (w *Widget) SetOnCycleOut(fn func()) { w.onCycleOut = fn }

// envelopeFields groups the widgets for one envelope (DCA or DCF).
type envelopeFields struct {
	kind      string // "dca" or "dcf" (used in error messages and patch routing)
	stages    *tview.Table
	sustain   *tview.DropDown
	end       *tview.DropDown
	levelKF   *tview.InputField
	levelVF   *tview.InputField
	rateKF    *tview.InputField
	rateVF    *tview.InputField
	stageRate *tview.InputField
	stageLvl  *tview.InputField

	// Selected stage index (0..7); the stageRate/stageLvl fields bind to
	// this stage. Defaults to 0 on Bind.
	selectedStage int
}

// New builds the panel. The widget is not bound to any voice yet; callers
// must call Bind(slot) before the panel's InputFields show meaningful
// values. Calling Bind before adding the Primitive to a layout is fine.
func New(m *model.Model) *Widget {
	w := &Widget{m: m}
	w.dca.kind = envKindDCA
	w.dcf.kind = envKindDCF

	dcaCol := w.buildEnvelopeColumn(&w.dca, " DCA ", false)
	dcfCol := w.buildEnvelopeColumn(&w.dcf, " DCF ", true)
	lfoCol := w.buildLFOColumn()

	columns := tview.NewFlex().SetDirection(tview.FlexColumn)
	columns.AddItem(dcaCol, 0, 1, false)
	columns.AddItem(dcfCol, 0, 1, false)
	columns.AddItem(lfoCol, 0, 1, false)

	footer := w.buildFooter()

	root := tview.NewFlex().SetDirection(tview.FlexRow)
	root.AddItem(columns, 0, 1, false)
	root.AddItem(footer, 3, 0, false)
	root.SetBorder(true).SetTitle(" Voice Details ")

	w.root = root
	w.installFieldCycling()
	return w
}

// Primitive returns the underlying tview primitive for embedding in a Flex.
func (w *Widget) Primitive() tview.Primitive { return w.root }

// Focus moves keyboard focus into the panel's first section (DCA stage
// table). The root is a Flex with no focus-delegated child, so a plain
// tApp.SetFocus(Primitive()) lands on the Flex itself and has no
// visible effect; callers should prefer this method.
func (w *Widget) Focus(tApp *tview.Application) {
	w.currentSection = 0
	w.focusSection(tApp, 0)
}

// CycleSection advances focus to the next section anchor. If the
// widget is on its last section, instead of wrapping internally it
// resets the section pointer to 0 and invokes onCycleOut so the app
// can hand focus to the upper pane. The next CycleSection call will
// start at section 0 again. Without an onCycleOut callback the widget
// falls back to in-widget wraparound (the pre-cross-pane behaviour).
func (w *Widget) CycleSection(tApp *tview.Application) {
	anchors := w.sectionAnchors()
	if len(anchors) == 0 {
		return
	}
	if w.currentSection >= len(anchors)-1 {
		w.currentSection = 0
		if w.onCycleOut != nil {
			w.onCycleOut()
			return
		}
		w.focusSection(tApp, 0)
		return
	}
	w.currentSection++
	w.focusSection(tApp, w.currentSection)
}

// sectionAnchors returns the primitive each section's focus should
// land on. Building this lazily lets us bail safely if a section's
// widget didn't get built (e.g. partial construction error).
func (w *Widget) sectionAnchors() []tview.Primitive {
	out := []tview.Primitive{}
	if w.dca.stages != nil {
		out = append(out, w.dca.stages)
	}
	if w.dcf.stages != nil {
		out = append(out, w.dcf.stages)
	}
	if w.lfoWave != nil {
		out = append(out, w.lfoWave)
	}
	if w.nameField != nil {
		out = append(out, w.nameField)
	}
	return out
}

func (w *Widget) focusSection(tApp *tview.Application, idx int) {
	anchors := w.sectionAnchors()
	if len(anchors) == 0 {
		tApp.SetFocus(w.root)
		return
	}
	if idx < 0 || idx >= len(anchors) {
		idx = 0
	}
	tApp.SetFocus(anchors[idx])
}

// Close releases any external resources. The widget does not subscribe to
// the model directly (Bind is driven by the app shell), so Close is a
// no-op today. Included for interface symmetry with voicelist.Widget.
func (w *Widget) Close() {}

// SetOnError registers a callback for commit-time validation errors.
// The app shell wires this to the status line. If no handler is
// registered the error is silently logged.
func (w *Widget) SetOnError(fn func(error)) { w.onError = fn }

// reportError forwards err to the registered handler. Nil err is ignored.
func (w *Widget) reportError(err error) {
	if err == nil || w.onError == nil {
		return
	}
	w.onError(err)
}

// Bind repopulates the panel from the voice at slot. The selected envelope
// stage is reset to 0 (stage 1 in 1-based UI parlance).
func (w *Widget) Bind(slot int) {
	w.slot = slot
	w.binding = true
	defer func() { w.binding = false }()

	v, err := w.m.Voice(slot)
	if err != nil || v == nil {
		// Surface the parse error but leave the widgets in their previous
		// state; the app shell can show a Modal or fall back to a sibling.
		w.reportError(fmt.Errorf("voicedetails: voice %d: %w", slot, err))
		return
	}

	// Footer.
	w.voiceLabel.SetText(fmt.Sprintf(" Voice #%02d ", slot+1))
	w.nameField.SetText(v.Name)
	if idx, ok := indexOf(playbackOptions, v.PlaybackMode); ok {
		w.playbackMode.SetCurrentOption(idx)
	} else {
		// Unknown mode (e.g. no_sound / normal_variant): leave DropDown on
		// "normal" so the user sees a sane default; saving it back doesn't
		// happen unless they explicitly pick a value.
		w.playbackMode.SetCurrentOption(0)
	}

	// Envelopes.
	w.dca.selectedStage = 0
	w.dcf.selectedStage = 0
	w.bindEnvelope(&w.dca, v.DCASustain, v.DCAEnd, v.DCARates, v.DCAStops, v.DCALevelKF, v.DCARateKF, v.VelDCAKF, v.VelDCARS)
	w.bindEnvelope(&w.dcf, v.DCFSustain, v.DCFEnd, v.DCFRates, v.DCFStops, v.DCFLevelKF, v.DCFRateKF, v.VelDCFKF, v.VelDCFRS)

	// DCF static filter extras.
	w.cutoff.SetText(strconv.Itoa(int(v.FilterCutoff)))
	w.resonance.SetText(strconv.Itoa(int(v.FilterQ)))
	w.resVel.SetText(strconv.Itoa(int(v.VelDCQKF)))

	// LFO column.
	if idx, ok := indexOfLFO(v.LFOWaveform); ok {
		w.lfoWave.SetCurrentOption(idx)
	} else {
		w.lfoWave.SetCurrentOption(0)
	}
	w.lfoPhaseSync.SetChecked(v.LFOPhaseSync)
	w.lfoDelay.SetText(strconv.Itoa(int(v.LFODelay)))
	w.lfoRate.SetText(strconv.Itoa(int(v.LFORate)))
	w.lfoAttack.SetText(strconv.Itoa(int(v.LFOAttack)))
	w.lfoDepthPitch.SetText(strconv.Itoa(int(v.LFODepthPitch)))
	w.lfoDepthAmp.SetText(strconv.Itoa(int(v.LFODepthAmp)))
	w.lfoDepthFilter.SetText(strconv.Itoa(int(v.LFODepthFilter)))
	w.lfoDepthQ.SetText(strconv.Itoa(int(v.LFODepthQ)))
}

// indexOf returns the index of want in options or (0, false).
func indexOf(options []string, want string) (int, bool) {
	for i, opt := range options {
		if opt == want {
			return i, true
		}
	}
	return 0, false
}

// indexOfLFO maps the human-readable waveform name returned by fzvinfo
// (e.g. "Sine", "Saw Up") to the lowercase CLI vocabulary index. fzvinfo
// returns title-case strings; the DropDown carries CLI tokens.
var fzvLFOToCLI = map[string]string{
	"Sine":      "sine",
	"Saw Up":    "saw-up",
	"Saw Down":  "saw-down",
	"Triangle":  "triangle",
	"Rectangle": "rectangle",
	"Random":    "random",
}

func indexOfLFO(fzvName string) (int, bool) {
	if cli, ok := fzvLFOToCLI[fzvName]; ok {
		return indexOf(waveformOptions, cli)
	}
	return 0, false
}

// --- Envelope column construction -----------------------------------------

// buildEnvelopeColumn assembles the DCA or DCF column. When withFilter is
// true, three extra InputFields (Cutoff, Resonance, Resonance Velocity) are
// added below the envelope; those are owned by w, not by the envelope.
func (w *Widget) buildEnvelopeColumn(env *envelopeFields, title string, withFilter bool) *tview.Flex {
	col := tview.NewFlex().SetDirection(tview.FlexRow)
	col.SetBorder(true).SetTitle(title)

	// Sustain/end DropDowns.
	susOpts := susOptions()
	endOpts := endOptions()
	env.sustain = tview.NewDropDown().SetLabel("Sustain stage (0-7 or none) ").SetOptions(susOpts, nil)
	env.end = tview.NewDropDown().SetLabel("End stage (0-7) ").SetOptions(endOpts, nil)
	env.sustain.SetSelectedFunc(func(_ string, _ int) {
		if w.binding {
			return
		}
		w.commitEnvelopeSustainEnd(env)
	})
	env.end.SetSelectedFunc(func(_ string, _ int) {
		if w.binding {
			return
		}
		w.commitEnvelopeSustainEnd(env)
	})

	// Stage table.
	env.stages = tview.NewTable().SetSelectable(true, false).SetFixed(envHeaderRows, 0)
	env.stages.SetBorder(false)
	env.stages.SetSelectionChangedFunc(func(row, _ int) {
		if w.binding {
			return
		}
		stage := row - envHeaderRows
		if stage < 0 || stage >= disk.EnvelopeStages {
			return
		}
		env.selectedStage = stage
		w.refreshStageEditors(env)
	})

	// KF/VF inputs.
	env.levelKF = makeSignedField("Level KF (-15..+15) ", 6, disk.MinKFDisplay, disk.MaxKFDisplay)
	env.levelVF = makeSignedField("Level VF (-127..+127) ", 7, -127, 127)
	env.rateKF = makeSignedField("Rate KF (-15..+15) ", 6, disk.MinKFDisplay, disk.MaxKFDisplay)
	env.rateVF = makeSignedField("Rate VF (-127..+127) ", 7, -127, 127)

	env.levelKF.SetDoneFunc(commitOnDone(w, func() { w.commitEnvelopeModulation(env) }))
	env.levelVF.SetDoneFunc(commitOnDone(w, func() { w.commitEnvelopeModulation(env) }))
	env.rateKF.SetDoneFunc(commitOnDone(w, func() { w.commitEnvelopeModulation(env) }))
	env.rateVF.SetDoneFunc(commitOnDone(w, func() { w.commitEnvelopeModulation(env) }))

	// Stage-specific editors below the stage table.
	env.stageRate = makeUnsignedField("Stage Rate (0-99) ", 5, disk.DisplayMax)
	env.stageLvl = makeUnsignedField("Stage Level (0-99) ", 5, disk.DisplayMax)
	env.stageRate.SetDoneFunc(commitOnDone(w, func() { w.commitEnvelopeStage(env) }))
	env.stageLvl.SetDoneFunc(commitOnDone(w, func() { w.commitEnvelopeStage(env) }))

	col.AddItem(env.sustain, 1, 0, false)
	col.AddItem(env.end, 1, 0, false)
	col.AddItem(env.stages, 0, 1, false)
	col.AddItem(env.stageRate, 1, 0, false)
	col.AddItem(env.stageLvl, 1, 0, false)
	col.AddItem(env.levelKF, 1, 0, false)
	col.AddItem(env.levelVF, 1, 0, false)
	col.AddItem(env.rateKF, 1, 0, false)
	col.AddItem(env.rateVF, 1, 0, false)

	if withFilter {
		w.cutoff = makeUnsignedField("Cutoff (0-127) ", 6, 127)
		w.resonance = makeUnsignedField("Resonance (0-127) ", 6, disk.MaxResonance)
		w.resVel = makeSignedField("Resonance Velocity (-127..+127) ", 7, -127, 127)
		w.cutoff.SetDoneFunc(commitOnDone(w, w.commitFilter))
		w.resonance.SetDoneFunc(commitOnDone(w, w.commitFilter))
		w.resVel.SetDoneFunc(commitOnDone(w, w.commitResonanceVelocity))
		col.AddItem(w.cutoff, 1, 0, false)
		col.AddItem(w.resonance, 1, 0, false)
		col.AddItem(w.resVel, 1, 0, false)
	}

	return col
}

// susOptions returns the DropDown options for an envelope's sustain stage:
// 8 numeric stages plus "none" (encoded as disk.NoSustainLoop / 8 by the
// hardware spec).
func susOptions() []string {
	opts := make([]string, 0, 9)
	for i := 0; i < disk.EnvelopeStages; i++ {
		opts = append(opts, strconv.Itoa(i))
	}
	opts = append(opts, "none")
	return opts
}

// endOptions returns the DropDown options for an envelope's end stage:
// 8 numeric stages. Unlike sustain, end has no "none" sentinel.
func endOptions() []string {
	opts := make([]string, 0, disk.EnvelopeStages)
	for i := 0; i < disk.EnvelopeStages; i++ {
		opts = append(opts, strconv.Itoa(i))
	}
	return opts
}

// bindEnvelope repopulates the DropDowns, stage table, KF/VF fields, and
// stage editors from the given envelope parameters. dcaSus/dcaEnd are the
// raw bytes; envelope "none" is encoded as disk.NoSustainLoop (8).
func (w *Widget) bindEnvelope(env *envelopeFields, sustain, end uint8, rates, stops [disk.EnvelopeStages]uint8, levelKF, rateKF, velLevelKF, velRateKF int8) {
	// Sustain DropDown: "none" sentinel for 8, otherwise the numeric stage.
	if int(sustain) >= disk.EnvelopeStages {
		env.sustain.SetCurrentOption(disk.EnvelopeStages) // "none"
	} else {
		env.sustain.SetCurrentOption(int(sustain))
	}
	// End DropDown: clamp out-of-range to the last stage so the widget
	// always shows a defined value. End=8 doesn't have a "none" mapping in
	// the spec for end, so we clamp.
	if int(end) >= disk.EnvelopeStages {
		env.end.SetCurrentOption(disk.EnvelopeStages - 1)
	} else {
		env.end.SetCurrentOption(int(end))
	}

	// KF/VF.
	env.levelKF.SetText(fmt.Sprintf("%d", disk.KFByteToDisplay(uint8(levelKF)))) //nolint:gosec // intentional int8->uint8 reinterpretation
	env.levelVF.SetText(strconv.Itoa(int(velLevelKF)))
	env.rateKF.SetText(fmt.Sprintf("%d", disk.KFByteToDisplay(uint8(rateKF)))) //nolint:gosec // intentional int8->uint8 reinterpretation
	env.rateVF.SetText(strconv.Itoa(int(velRateKF)))

	// Stage table.
	w.populateStageTable(env, sustain, end, rates, stops)
	env.stages.Select(env.selectedStage+envHeaderRows, 0)
	w.refreshStageEditors(env)
}

// populateStageTable rewrites the envelope's stage table from rates/stops.
// Annotations show the SUS and END markers per spec §2.3.1.
func (w *Widget) populateStageTable(env *envelopeFields, sustain, end uint8, rates, stops [disk.EnvelopeStages]uint8) {
	t := env.stages
	t.Clear()
	titles := []string{"#", "Rate", "Level", ""}
	for col, title := range titles {
		t.SetCell(0, col, tview.NewTableCell(title).SetTextColor(tview.Styles.SecondaryTextColor).SetSelectable(false))
	}
	for stage := 0; stage < disk.EnvelopeStages; stage++ {
		row := stage + envHeaderRows
		mark := ""
		if int(sustain) == stage {
			mark = "[SUS]"
		}
		if int(end) == stage {
			if mark != "" {
				mark += " "
			}
			mark += "[END]"
		}
		t.SetCell(row, 0, tview.NewTableCell(strconv.Itoa(stage+1)))
		t.SetCell(row, 1, tview.NewTableCell(strconv.Itoa(disk.RateByteToDisplay(rates[stage]))))
		t.SetCell(row, 2, tview.NewTableCell(strconv.Itoa(disk.StopByteToDisplay(stops[stage]))))
		t.SetCell(row, 3, tview.NewTableCell(mark))
	}
}

// refreshStageEditors copies the selected stage's rate/level into the
// stage-editor InputFields without triggering a commit.
// refreshEnvelopeTable re-renders the stage table from the current
// model state. Call after any commit that changes per-stage rates,
// stops, sustain, or end so the cells reflect what's actually saved.
// Without this the table stays frozen at its Bind-time values, and
// refreshStageEditors (which reads from the table on stage selection
// change) returns stale data.
func (w *Widget) refreshEnvelopeTable(env *envelopeFields) {
	v, err := w.m.Voice(w.slot)
	if err != nil || v == nil {
		return
	}
	switch env.kind {
	case envKindDCA:
		w.populateStageTable(env, v.DCASustain, v.DCAEnd, v.DCARates, v.DCAStops)
	case envKindDCF:
		w.populateStageTable(env, v.DCFSustain, v.DCFEnd, v.DCFRates, v.DCFStops)
	}
}

func (w *Widget) refreshStageEditors(env *envelopeFields) {
	// Suppress commit while we re-stamp the field text.
	prev := w.binding
	w.binding = true
	defer func() { w.binding = prev }()

	rateCell := env.stages.GetCell(env.selectedStage+envHeaderRows, 1)
	lvlCell := env.stages.GetCell(env.selectedStage+envHeaderRows, 2)
	if rateCell != nil {
		env.stageRate.SetText(rateCell.Text)
	}
	if lvlCell != nil {
		env.stageLvl.SetText(lvlCell.Text)
	}
}

// --- LFO column construction ---------------------------------------------

func (w *Widget) buildLFOColumn() *tview.Flex {
	col := tview.NewFlex().SetDirection(tview.FlexRow)
	col.SetBorder(true).SetTitle(" LFO ")

	w.lfoWave = tview.NewDropDown().SetLabel("Waveform ").SetOptions(waveformOptions, nil)
	w.lfoWave.SetSelectedFunc(func(_ string, _ int) {
		if w.binding {
			return
		}
		w.commitLFOWaveform()
	})

	w.lfoPhaseSync = tview.NewCheckbox().SetLabel("Phase Sync ")
	w.lfoPhaseSync.SetChangedFunc(func(_ bool) {
		if w.binding {
			return
		}
		w.commitLFOWaveform()
	})

	w.lfoDelay = makeUnsignedField("Delay (0-65535) ", 7, disk.MaxLFODelay)
	w.lfoRate = makeUnsignedField("Rate (0-127) ", 5, 127)
	w.lfoAttack = makeUnsignedField("Attack (1-127) ", 5, 127)
	w.lfoDepthPitch = makeUnsignedField("Depth Pitch (0-127) ", 5, 127)
	w.lfoDepthAmp = makeUnsignedField("Depth Amp (0-127) ", 5, 127)
	w.lfoDepthFilter = makeUnsignedField("Depth Filter (0-127) ", 5, 127)
	w.lfoDepthQ = makeUnsignedField("Depth Resonance (0-127) ", 5, 127)

	for _, f := range []*tview.InputField{
		w.lfoDelay, w.lfoRate, w.lfoAttack,
		w.lfoDepthPitch, w.lfoDepthAmp, w.lfoDepthFilter, w.lfoDepthQ,
	} {
		f.SetDoneFunc(commitOnDone(w, w.commitLFO))
	}

	col.AddItem(w.lfoWave, 1, 0, false)
	col.AddItem(w.lfoPhaseSync, 1, 0, false)
	col.AddItem(w.lfoDelay, 1, 0, false)
	col.AddItem(w.lfoRate, 1, 0, false)
	col.AddItem(w.lfoAttack, 1, 0, false)
	col.AddItem(w.lfoDepthPitch, 1, 0, false)
	col.AddItem(w.lfoDepthAmp, 1, 0, false)
	col.AddItem(w.lfoDepthFilter, 1, 0, false)
	col.AddItem(w.lfoDepthQ, 1, 0, false)
	col.AddItem(tview.NewBox(), 0, 1, false) // spacer

	return col
}

// --- Footer --------------------------------------------------------------

func (w *Widget) buildFooter() *tview.Flex {
	w.voiceLabel = tview.NewTextView().SetText(" Voice #-- ")

	w.nameField = tview.NewInputField().
		SetLabel("Name ").
		SetAcceptanceFunc(helpers.AcceptName(disk.LabelSize)).
		SetFieldWidth(disk.LabelSize + 1)
	w.nameField.SetDoneFunc(commitOnDone(w, w.commitName))

	w.playbackMode = tview.NewDropDown().
		SetLabel("Playback Mode ").
		SetOptions(playbackOptions, nil)
	w.playbackMode.SetSelectedFunc(func(_ string, _ int) {
		if w.binding {
			return
		}
		w.commitPlaybackMode()
	})

	footer := tview.NewFlex().SetDirection(tview.FlexColumn)
	footer.AddItem(w.voiceLabel, 14, 0, false)
	footer.AddItem(w.nameField, 0, 1, false)
	footer.AddItem(w.playbackMode, 0, 1, false)
	footer.SetBorder(true)
	return footer
}

// --- Commit helpers ------------------------------------------------------

// commitOnDone wraps a commit function in the SetDoneFunc signature.
// Enter, Tab, and Shift+Tab commit; Tab additionally advances focus to
// the next field in the panel's fieldList, and Shift+Tab cycles to the
// next section anchor.
func commitOnDone(w *Widget, commit func()) func(tcell.Key) {
	return func(key tcell.Key) {
		if w.binding {
			return
		}
		switch key { //nolint:exhaustive // commit only on Enter/Tab/BacktTab
		case tcell.KeyEnter, tcell.KeyTab, tcell.KeyBacktab:
			commit()
		}
		switch key { //nolint:exhaustive // focus advance only on Tab/Backtab
		case tcell.KeyTab:
			w.focusNextField()
		case tcell.KeyBacktab:
			w.CycleSection(w.tApp)
		}
	}
}

// fieldList returns every focusable primitive in the panel, ordered
// for Tab navigation: down each column in turn (DCA, DCF, LFO), then
// the footer. Skips nil entries so partially-constructed widgets stay
// safe.
func (w *Widget) fieldList() []tview.Primitive {
	var out []tview.Primitive
	add := func(p tview.Primitive) {
		if p == nil {
			return
		}
		out = append(out, p)
	}
	addEnv := func(env *envelopeFields) {
		add(env.stages)
		add(env.sustain)
		add(env.end)
		add(env.stageRate)
		add(env.stageLvl)
		add(env.levelKF)
		add(env.levelVF)
		add(env.rateKF)
		add(env.rateVF)
	}
	addEnv(&w.dca)
	addEnv(&w.dcf)
	add(w.cutoff)
	add(w.resonance)
	add(w.resVel)
	add(w.lfoWave)
	add(w.lfoPhaseSync)
	add(w.lfoDelay)
	add(w.lfoRate)
	add(w.lfoAttack)
	add(w.lfoDepthPitch)
	add(w.lfoDepthAmp)
	add(w.lfoDepthFilter)
	add(w.lfoDepthQ)
	add(w.nameField)
	add(w.playbackMode)
	return out
}

// InputFields returns every InputField in the panel, in the same order
// as fieldList. Used by the app shell's focused-field finder so a
// Ctrl+S flush after a mouse click can still locate the InputField
// whose embedded TextArea has focus.
func (w *Widget) InputFields() []*tview.InputField {
	var out []*tview.InputField
	for _, p := range w.fieldList() {
		if in, ok := p.(*tview.InputField); ok {
			out = append(out, in)
		}
	}
	return out
}

// focusNextField advances focus to the next primitive in fieldList,
// wrapping at the end. No-op if SetApp hasn't been called or the
// currently-focused primitive isn't in fieldList (in which case
// focus jumps to the first entry as a sensible fallback).
func (w *Widget) focusNextField() {
	if w.tApp == nil {
		return
	}
	list := w.fieldList()
	if len(list) == 0 {
		return
	}
	current := w.tApp.GetFocus()
	for i, p := range list {
		if p == current {
			w.tApp.SetFocus(list[(i+1)%len(list)])
			return
		}
	}
	// Current focus not in list; start at the beginning.
	w.tApp.SetFocus(list[0])
}

// installFieldCycling wires Tab and Shift+Tab on every non-InputField
// focusable in the panel. InputFields handle Tab/Backtab via their
// SetDoneFunc (see commitOnDone); Tables, DropDowns, and Checkboxes
// don't, so we capture before their default input handlers and route
// to focusNextField / CycleSection ourselves.
//
// Call once from New, after every primitive is constructed and after
// the panel's root is set.
func (w *Widget) installFieldCycling() {
	capture := w.tabCaptureFn()
	for _, env := range []*envelopeFields{&w.dca, &w.dcf} {
		if env.stages != nil {
			env.stages.SetInputCapture(capture)
		}
		if env.sustain != nil {
			env.sustain.SetInputCapture(capture)
		}
		if env.end != nil {
			env.end.SetInputCapture(capture)
		}
	}
	if w.lfoWave != nil {
		w.lfoWave.SetInputCapture(capture)
	}
	if w.lfoPhaseSync != nil {
		w.lfoPhaseSync.SetInputCapture(capture)
	}
	if w.playbackMode != nil {
		w.playbackMode.SetInputCapture(capture)
	}
}

// tabCaptureFn returns an input-capture handler for non-InputField
// focusables: Tab advances field-by-field; Shift+Tab cycles sections.
// All other keys pass through to the primitive's default handler.
func (w *Widget) tabCaptureFn() func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() { //nolint:exhaustive // only Tab/Backtab handled here
		case tcell.KeyTab:
			w.focusNextField()
			return nil
		case tcell.KeyBacktab:
			w.CycleSection(w.tApp)
			return nil
		}
		return event
	}
}

// applyPatches dispatches a slice of voice-header-relative patches to the
// model, surfacing the first error via the registered handler. We continue
// applying subsequent patches on error so a partial batch doesn't leave the
// widget in an inconsistent state (the model itself is atomic per patch).
func (w *Widget) applyPatches(patches []voiceedit.Patch, err error) {
	if err != nil {
		w.reportError(err)
		return
	}
	for _, p := range patches {
		if e := w.m.ApplyVoicePatch(w.slot, p); e != nil {
			w.reportError(e)
			return
		}
	}
}

// commitName builds a name patch from the InputField's text and applies it.
func (w *Widget) commitName() {
	patches, err := voiceedit.BuildNamePatch(w.nameField.GetText())
	w.applyPatches(patches, err)
}

// commitPlaybackMode applies the currently-selected playback mode.
func (w *Widget) commitPlaybackMode() {
	_, mode := w.playbackMode.GetCurrentOption()
	patches, err := voiceedit.BuildPlaybackModePatch(mode)
	w.applyPatches(patches, err)
}

// commitFilter builds cutoff + resonance patches and applies them. Both
// fields are committed together so partial input doesn't leave the model in
// a half-state.
func (w *Widget) commitFilter() {
	cutoff, err := parseIntOrUnchanged(w.cutoff.GetText())
	if err != nil {
		w.reportError(fmt.Errorf("voicedetails: cutoff: %w", err))
		return
	}
	resonance, err := parseIntOrUnchanged(w.resonance.GetText())
	if err != nil {
		w.reportError(fmt.Errorf("voicedetails: resonance: %w", err))
		return
	}
	patches, err := voiceedit.BuildFilterPatches(cutoff, resonance)
	w.applyPatches(patches, err)
}

// commitResonanceVelocity applies the vel_dcq_kf field via the modulation
// builder (no other modulation fields change, hence the Unchanged sentinels).
func (w *Widget) commitResonanceVelocity() {
	v, err := parseIntOrUnchanged(w.resVel.GetText())
	if err != nil {
		w.reportError(fmt.Errorf("voicedetails: resonance velocity: %w", err))
		return
	}
	patches, err := voiceedit.BuildModulationPatches(
		voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged,
		voiceedit.Unchanged, voiceedit.Unchanged, v,
		voiceedit.Unchanged, voiceedit.Unchanged,
	)
	w.applyPatches(patches, err)
}

// commitEnvelopeModulation applies the envelope's KF/VF fields via the
// modulation builder. Only the values for the envelope (DCA or DCF) being
// edited are written; the other envelope's fields are left untouched.
func (w *Widget) commitEnvelopeModulation(env *envelopeFields) {
	levelKF, lkerr := parseIntOrUnchanged(env.levelKF.GetText())
	levelVF, lverr := parseIntOrUnchanged(env.levelVF.GetText())
	rateKF, rkerr := parseIntOrUnchanged(env.rateKF.GetText())
	rateVF, rverr := parseIntOrUnchanged(env.rateVF.GetText())
	for _, e := range []error{lkerr, lverr, rkerr, rverr} {
		if e != nil {
			w.reportError(fmt.Errorf("voicedetails: %s modulation: %w", env.kind, e))
			return
		}
	}
	var patches []voiceedit.Patch
	var err error
	switch env.kind {
	case envKindDCA:
		patches, err = voiceedit.BuildModulationPatches(
			levelKF, rateKF, voiceedit.Unchanged, voiceedit.Unchanged,
			levelVF, voiceedit.Unchanged, voiceedit.Unchanged,
			rateVF, voiceedit.Unchanged,
		)
	case envKindDCF:
		patches, err = voiceedit.BuildModulationPatches(
			voiceedit.Unchanged, voiceedit.Unchanged, levelKF, rateKF,
			voiceedit.Unchanged, levelVF, voiceedit.Unchanged,
			voiceedit.Unchanged, rateVF,
		)
	}
	w.applyPatches(patches, err)
}

// commitEnvelopeSustainEnd applies the current Sustain / End DropDown
// selections. Sustain's "none" option maps to disk.NoSustainLoop (8).
func (w *Widget) commitEnvelopeSustainEnd(env *envelopeFields) {
	susIdx, _ := env.sustain.GetCurrentOption()
	endIdx, _ := env.end.GetCurrentOption()
	sustain := susIdx
	if susIdx >= disk.EnvelopeStages {
		sustain = disk.NoSustainLoop
	}

	// Build full rate/stop arrays of "unchanged" sentinels; we only want
	// to write the sustain/end bytes here.
	var rates, stops [disk.EnvelopeStages]int
	var origRates [disk.EnvelopeStages]uint8
	for i := range rates {
		rates[i] = voiceedit.Unchanged
		stops[i] = voiceedit.Unchanged
	}

	// Source origRates from the live voice header (read-only access to
	// pre-edit bytes via m.Voice).
	v, err := w.m.Voice(w.slot)
	if err == nil && v != nil {
		switch env.kind {
		case envKindDCA:
			origRates = v.DCARates
		case envKindDCF:
			origRates = v.DCFRates
		}
	}

	var patches []voiceedit.Patch
	switch env.kind {
	case envKindDCA:
		patches, err = voiceedit.BuildDCAPatches(sustain, endIdx, rates, stops, origRates)
	case envKindDCF:
		patches, err = voiceedit.BuildDCFPatches(sustain, endIdx, rates, stops, origRates)
	}
	w.applyPatches(patches, err)
	w.refreshEnvelopeTable(env)
}

// commitEnvelopeStage applies a stage-rate or stage-level change for the
// currently-selected stage of env. Both inputs are read together so a
// single Tab through both fields produces one consistent patch batch.
func (w *Widget) commitEnvelopeStage(env *envelopeFields) {
	stageRate, srerr := parseIntOrUnchanged(env.stageRate.GetText())
	stageLvl, slerr := parseIntOrUnchanged(env.stageLvl.GetText())
	for _, e := range []error{srerr, slerr} {
		if e != nil {
			w.reportError(fmt.Errorf("voicedetails: %s stage: %w", env.kind, e))
			return
		}
	}

	var rates, stops [disk.EnvelopeStages]int
	var origRates [disk.EnvelopeStages]uint8
	for i := range rates {
		rates[i] = voiceedit.Unchanged
		stops[i] = voiceedit.Unchanged
	}
	rates[env.selectedStage] = stageRate
	stops[env.selectedStage] = stageLvl

	v, err := w.m.Voice(w.slot)
	if err == nil && v != nil {
		switch env.kind {
		case envKindDCA:
			origRates = v.DCARates
		case envKindDCF:
			origRates = v.DCFRates
		}
	}

	var patches []voiceedit.Patch
	switch env.kind {
	case envKindDCA:
		patches, err = voiceedit.BuildDCAPatches(voiceedit.Unchanged, voiceedit.Unchanged, rates, stops, origRates)
	case envKindDCF:
		patches, err = voiceedit.BuildDCFPatches(voiceedit.Unchanged, voiceedit.Unchanged, rates, stops, origRates)
	}
	w.applyPatches(patches, err)
	w.refreshEnvelopeTable(env)
}

// commitLFOWaveform applies the current waveform + phase-sync selection.
// The phase-sync flag occupies bit 7 of the lfo_name byte; BuildLFOPatches
// preserves it from origLFOName for non-waveform commits, but for a wave
// change we must compute the new combined byte ourselves so the
// SetSelectedFunc / SetChangedFunc handler covers both axes.
func (w *Widget) commitLFOWaveform() {
	waveIdx, _ := w.lfoWave.GetCurrentOption()
	phase := uint8(0)
	if w.lfoPhaseSync.IsChecked() {
		phase = disk.LFOPhaseFlag
	}
	patches, err := voiceedit.BuildLFOPatches(
		waveIdx,
		voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged,
		voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged,
		phase,
	)
	w.applyPatches(patches, err)
}

// commitLFO applies the LFO numeric InputFields. Waveform / phase sync are
// not committed here; they have their own immediate-commit handler.
func (w *Widget) commitLFO() {
	delay, derr := parseIntOrUnchanged(w.lfoDelay.GetText())
	rate, rerr := parseIntOrUnchanged(w.lfoRate.GetText())
	attack, aerr := parseIntOrUnchanged(w.lfoAttack.GetText())
	pitch, perr := parseIntOrUnchanged(w.lfoDepthPitch.GetText())
	amp, aperr := parseIntOrUnchanged(w.lfoDepthAmp.GetText())
	filter, ferr := parseIntOrUnchanged(w.lfoDepthFilter.GetText())
	q, qerr := parseIntOrUnchanged(w.lfoDepthQ.GetText())
	for _, e := range []error{derr, rerr, aerr, perr, aperr, ferr, qerr} {
		if e != nil {
			w.reportError(fmt.Errorf("voicedetails: lfo: %w", e))
			return
		}
	}
	// origLFOName: read the pre-edit byte so BuildLFOPatches preserves the
	// phase-sync flag when only numeric fields change. We don't commit a
	// waveform here, so the byte's exact value only matters for the wave
	// branch which isn't taken; passing 0 is safe.
	patches, err := voiceedit.BuildLFOPatches(
		voiceedit.Unchanged,
		rate, delay, attack, pitch, amp, filter, q,
		0,
	)
	w.applyPatches(patches, err)
}

// parseIntOrUnchanged returns the integer parsed from s, or
// voiceedit.Unchanged when s is empty / a partial sign-only input. The
// Unchanged sentinel makes the surrounding patch a no-op so a Tab through
// an un-touched field doesn't clobber the model. Unparseable non-empty
// strings return an error.
func parseIntOrUnchanged(s string) (int, error) {
	if s == "" || s == "-" || s == "+" {
		return voiceedit.Unchanged, nil
	}
	return strconv.Atoi(s)
}

// --- Widget factories ----------------------------------------------------

// makeUnsignedField builds an InputField wired to AcceptUnsigned(hi).
func makeUnsignedField(label string, width, hi int) *tview.InputField {
	return tview.NewInputField().
		SetLabel(label).
		SetFieldWidth(width).
		SetAcceptanceFunc(helpers.AcceptUnsigned(hi))
}

// makeSignedField builds an InputField wired to AcceptSigned(lo, hi).
func makeSignedField(label string, width, lo, hi int) *tview.InputField {
	return tview.NewInputField().
		SetLabel(label).
		SetFieldWidth(width).
		SetAcceptanceFunc(helpers.AcceptSigned(lo, hi))
}
