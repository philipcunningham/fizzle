package voicedetails

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

const testVoiceAlpha = "ALPHA"

func newTestModel(t *testing.T, names []string) *model.Model {
	t.Helper()
	_, path := fzfbuilder.MakeTestFZF(t, names)
	m, err := model.New(path)
	if err != nil {
		t.Fatalf("model.New: %v", err)
	}
	return m
}

// TestBindPopulatesFooter verifies that after Bind, the footer voice label
// and Name InputField match the in-memory voice.
func TestBindPopulatesFooter(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)
	if got := w.nameField.GetText(); got != "ALPHA" {
		t.Errorf("Name = %q, want ALPHA", got)
	}
	if got := w.voiceLabel.GetText(false); got == "" {
		t.Errorf("Voice label empty after Bind")
	}
}

// TestCommitNameUpdatesModel verifies that an Enter on the Name field
// builds a name patch and applies it through the model, marking dirty.
func TestCommitNameUpdatesModel(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	w.nameField.SetText("RENAMED")
	w.commitName()

	v, err := m.Voice(0)
	if err != nil {
		t.Fatalf("Voice(0): %v", err)
	}
	if v.Name != "RENAMED" {
		t.Errorf("Voice.Name = %q, want RENAMED", v.Name)
	}
	if !m.IsDirty() {
		t.Errorf("model should be dirty after name commit")
	}
}

// TestCommitNameOnDoneFunc verifies the InputField's SetDoneFunc path works
// when invoked with Enter. This is the real flow tview uses.
func TestCommitNameOnDoneFunc(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	// Wire-level: invoke the done callback registered by commitOnDone.
	w.nameField.SetText("VIADONE")
	// Re-use the package's commitOnDone shape by calling the registered
	// done func via tview's internal field. tview exposes no getter, so we
	// re-create the wrapper inline; this also exercises the Enter branch.
	commitOnDone(w, w.commitName)(tcell.KeyEnter)

	v, _ := m.Voice(0)
	if v.Name != "VIADONE" {
		t.Errorf("Name after Enter = %q, want VIADONE", v.Name)
	}
}

// TestCommitNameInvalidLengthSurfacesError verifies that a name longer than
// disk.LabelSize is refused by the builder and routed to the error
// callback. (The acceptance function would normally prevent this at the
// keystroke level, but SetText bypasses it.)
func TestCommitNameInvalidLengthSurfacesError(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	var gotErr error
	w.SetOnError(func(e error) { gotErr = e })

	w.nameField.SetText("THIS-IS-WAY-TOO-LONG-FOR-A-VOICE-NAME")
	w.commitName()
	if gotErr == nil {
		t.Errorf("commitName: expected error for over-long name")
	}
}

// TestCommitFilterUpdatesModel verifies committing the Cutoff field writes
// the dcf byte at the voice's filter offset.
func TestCommitFilterUpdatesModel(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	w.cutoff.SetText("64")
	w.resonance.SetText("0")
	w.commitFilter()

	v, _ := m.Voice(0)
	if v.FilterCutoff != 64 {
		t.Errorf("FilterCutoff = %d, want 64", v.FilterCutoff)
	}
}

// TestCommitLFOUpdatesModel verifies that committing LFO numeric fields
// changes the corresponding voice-header bytes.
func TestCommitLFOUpdatesModel(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	w.lfoRate.SetText("42")
	w.lfoDelay.SetText("1000")
	w.lfoAttack.SetText("100")
	w.lfoDepthPitch.SetText("10")
	w.lfoDepthAmp.SetText("11")
	w.lfoDepthFilter.SetText("12")
	w.lfoDepthQ.SetText("13")
	w.commitLFO()

	v, _ := m.Voice(0)
	if v.LFORate != 42 {
		t.Errorf("LFORate = %d, want 42", v.LFORate)
	}
	if v.LFODelay != 1000 {
		t.Errorf("LFODelay = %d, want 1000", v.LFODelay)
	}
	if v.LFOAttack != 100 {
		t.Errorf("LFOAttack = %d, want 100", v.LFOAttack)
	}
	if v.LFODepthPitch != 10 || v.LFODepthAmp != 11 || v.LFODepthFilter != 12 || v.LFODepthQ != 13 {
		t.Errorf("LFO depths = (%d,%d,%d,%d), want (10,11,12,13)",
			v.LFODepthPitch, v.LFODepthAmp, v.LFODepthFilter, v.LFODepthQ)
	}
}

// TestCommitEnvelopeStageDCA verifies a stage-rate / stage-level commit on
// the DCA envelope writes the right bytes.
func TestCommitEnvelopeStageDCA(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	// Stage 1 (selectedStage=0) is the default selection after Bind.
	w.dca.stageRate.SetText("50")
	w.dca.stageLvl.SetText("75")
	w.commitEnvelopeStage(&w.dca)

	v, _ := m.Voice(0)
	if got := disk.RateByteToDisplay(v.DCARates[0]); got != 50 {
		t.Errorf("DCA stage 1 rate display = %d, want 50", got)
	}
	if got := disk.StopByteToDisplay(v.DCAStops[0]); got != 75 {
		t.Errorf("DCA stage 1 level display = %d, want 75", got)
	}
}

// TestCommitEnvelopeModulationDCA verifies that DCA Level KF / Rate KF /
// Level VF / Rate VF commits write the matching modulation bytes.
func TestCommitEnvelopeModulationDCA(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	w.dca.levelKF.SetText("5")
	w.dca.rateKF.SetText("-3")
	w.dca.levelVF.SetText("64")
	w.dca.rateVF.SetText("-30")
	w.commitEnvelopeModulation(&w.dca)

	v, _ := m.Voice(0)
	if got := disk.KFByteToDisplay(uint8(v.DCALevelKF)); got != 5 { //nolint:gosec // intentional int8->uint8 reinterpretation
		t.Errorf("DCALevelKF display = %d, want 5", got)
	}
	if got := disk.KFByteToDisplay(uint8(v.DCARateKF)); got != -3 { //nolint:gosec // intentional int8->uint8 reinterpretation
		t.Errorf("DCARateKF display = %d, want -3", got)
	}
	if int(v.VelDCAKF) != 64 {
		t.Errorf("VelDCAKF = %d, want 64", v.VelDCAKF)
	}
	if int(v.VelDCARS) != -30 {
		t.Errorf("VelDCARS = %d, want -30", v.VelDCARS)
	}
}

// TestCommitResonanceVelocity verifies that the Resonance Velocity field
// writes to vel_dcq_kf via the modulation builder.
func TestCommitResonanceVelocity(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	w.resVel.SetText("-50")
	w.commitResonanceVelocity()

	v, _ := m.Voice(0)
	if int(v.VelDCQKF) != -50 {
		t.Errorf("VelDCQKF = %d, want -50", v.VelDCQKF)
	}
}

// TestBindRefreshesAfterModelChange verifies that calling Bind a second
// time after a model edit pulls the new value into the widget.
func TestBindRefreshesAfterModelChange(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	// External edit (simulating an undo / different widget).
	w.nameField.SetText("FIRST")
	w.commitName()

	// Re-bind to refresh from model.
	w.Bind(0)
	if got := w.nameField.GetText(); got != "FIRST" {
		t.Errorf("Name after re-bind = %q, want FIRST", got)
	}
}

// TestCommitPlaybackMode verifies that selecting a playback mode writes the
// matching loop_mode bytes.
func TestCommitPlaybackMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	idx, _ := indexOf(playbackOptions, "reverse")
	w.playbackMode.SetCurrentOption(idx)
	w.commitPlaybackMode()

	v, _ := m.Voice(0)
	if v.PlaybackMode != "reverse" {
		t.Errorf("PlaybackMode = %q, want reverse", v.PlaybackMode)
	}
}

// TestEmptyInputDoesNotMutate verifies that committing with empty
// InputField text uses the Unchanged sentinel so the model is unaffected.
// This protects against a Tab through an untouched form clobbering values.
func TestEmptyInputDoesNotMutate(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	// Capture pre-edit cutoff (model default).
	preV, _ := m.Voice(0)
	preCutoff := preV.FilterCutoff

	w.cutoff.SetText("")
	w.resonance.SetText("")
	w.commitFilter()

	postV, _ := m.Voice(0)
	if postV.FilterCutoff != preCutoff {
		t.Errorf("FilterCutoff changed on empty commit: pre=%d post=%d", preCutoff, postV.FilterCutoff)
	}
	if m.IsDirty() {
		t.Errorf("empty commit dirtied the model")
	}
}

// TestStageSelectionUpdatesEditors verifies that programmatically selecting
// a different stage row repoints the stage-rate / stage-level editors at
// that stage's current value.
func TestStageSelectionUpdatesEditors(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()
	w.Bind(0)

	// Write distinct values into stage 1 and stage 3 via the model so we
	// have something to read back.
	w.dca.stageRate.SetText("10")
	w.dca.stageLvl.SetText("20")
	w.commitEnvelopeStage(&w.dca)

	// Manually move selection to stage 3 (index 2).
	w.dca.selectedStage = 2
	w.dca.stageRate.SetText("30")
	w.dca.stageLvl.SetText("40")
	w.commitEnvelopeStage(&w.dca)

	// Re-bind clears edit state and re-reads from the model.
	w.Bind(0)

	// Stage 1 by default after Bind.
	if got := w.dca.stageRate.GetText(); got != "10" {
		t.Errorf("stage 1 rate editor = %q, want 10", got)
	}
	if got := w.dca.stageLvl.GetText(); got != "20" {
		t.Errorf("stage 1 level editor = %q, want 20", got)
	}

	// Move selection to stage 3 and refresh editors from the table.
	w.dca.selectedStage = 2
	w.refreshStageEditors(&w.dca)
	if got := w.dca.stageRate.GetText(); got != "30" {
		t.Errorf("stage 3 rate editor = %q, want 30", got)
	}
	if got := w.dca.stageLvl.GetText(); got != "40" {
		t.Errorf("stage 3 level editor = %q, want 40", got)
	}
}

// TestParseIntOrUnchanged sanity-checks the empty-string handling we rely
// on across every commit helper. Empty / sign-only input returns the
// voiceedit.Unchanged sentinel so a partial commit is a no-op.
func TestParseIntOrUnchanged(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int
		err  bool
	}{
		{"", voiceeditUnchanged, false},
		{"-", voiceeditUnchanged, false},
		{"+", voiceeditUnchanged, false},
		{"42", 42, false},
		{"-12", -12, false},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := parseIntOrUnchanged(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseIntOrUnchanged(%q): want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseIntOrUnchanged(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseIntOrUnchanged(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// voiceeditUnchanged mirrors voiceedit.Unchanged so the table-driven test
// reads naturally without an import-only dependency wedge. Kept in sync
// with the upstream sentinel.
const voiceeditUnchanged = -1000

// TestNumericLabelsIncludeRange sanity-checks the spec requirement that
// every editable label embeds its valid range.
func TestNumericLabelsIncludeRange(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	defer w.Close()

	checks := map[string]string{
		w.cutoff.GetLabel():        "0-127",
		w.resonance.GetLabel():     "0-127",
		w.resVel.GetLabel():        "-127..+127",
		w.lfoDelay.GetLabel():      "0-65535",
		w.lfoAttack.GetLabel():     "1-127",
		w.dca.levelKF.GetLabel():   "-15..+15",
		w.dca.stageRate.GetLabel(): "0-99",
		w.dca.stageLvl.GetLabel():  "0-99",
	}
	for label, want := range checks {
		if !containsSubstr(label, want) {
			t.Errorf("label %q missing range %q", label, want)
		}
	}
}

// containsSubstr is a tiny dependency-free substring check so the test
// doesn't pull in strings.Contains via a separate import line.
func containsSubstr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestStageTableRefreshesOnCommit verifies that after the user commits a
// stage rate/level or sustain/end change, the envelope stage table
// re-renders to show the new values. Without this, the table cells
// stay frozen at their Bind-time state, the input fields (which
// re-read from the table on stage selection change) go stale, and
// users see "their edit didn't apply", even though the model itself
// is correct.
func TestStageTableRefreshesOnCommit(t *testing.T) {
	t.Parallel()

	// envHeaderRows is the column-title row at the top of the stage
	// table; stage N (0-indexed) lives at row N + envHeaderRows.
	stageRow := func(stage int) int { return stage + envHeaderRows }

	t.Run("A: stage Rate cell reflects committed value", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t, []string{testVoiceAlpha})
		w := New(m)
		defer w.Close()
		w.Bind(0)

		w.dca.selectedStage = 2 // stage 3
		w.dca.stageRate.SetText("25")
		w.commitEnvelopeStage(&w.dca)

		got := w.dca.stages.GetCell(stageRow(2), 1).Text
		if got != "25" {
			t.Errorf("stage 3 Rate cell = %q, want \"25\"", got)
		}
	})

	t.Run("B: stage Level cell reflects committed value", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t, []string{testVoiceAlpha})
		w := New(m)
		defer w.Close()
		w.Bind(0)

		w.dca.selectedStage = 4 // stage 5
		w.dca.stageLvl.SetText("60")
		w.commitEnvelopeStage(&w.dca)

		got := w.dca.stages.GetCell(stageRow(4), 2).Text
		if got != "60" {
			t.Errorf("stage 5 Level cell = %q, want \"60\"", got)
		}
	})

	t.Run("C: sustain marker moves to new sustain row", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t, []string{testVoiceAlpha})
		w := New(m)
		defer w.Close()
		w.Bind(0)

		// SetCurrentOption fires the DropDown's SelectedFunc which
		// calls commitEnvelopeSustainEnd. The test exercises the
		// real user-visible commit path.
		w.dca.sustain.SetCurrentOption(4) // sustain at stage 5

		got := w.dca.stages.GetCell(stageRow(4), 3).Text
		if !contains(got, "SUS") {
			t.Errorf("stage 5 mark cell = %q, want to contain \"SUS\"", got)
		}
	})

	t.Run("D: end marker moves to new end row", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t, []string{testVoiceAlpha})
		w := New(m)
		defer w.Close()
		w.Bind(0)

		w.dca.end.SetCurrentOption(6) // end at stage 7

		got := w.dca.stages.GetCell(stageRow(6), 3).Text
		if !contains(got, "END") {
			t.Errorf("stage 7 mark cell = %q, want to contain \"END\"", got)
		}
	})

	t.Run("E: regression, navigate to another stage and back restores new value", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t, []string{testVoiceAlpha})
		w := New(m)
		defer w.Close()
		w.Bind(0)

		// Edit stage 3 to a known rate.
		w.dca.selectedStage = 2
		w.refreshStageEditors(&w.dca)
		w.dca.stageRate.SetText("33")
		w.commitEnvelopeStage(&w.dca)

		// Navigate to stage 6 (different value), then back to stage 3.
		w.dca.selectedStage = 5
		w.refreshStageEditors(&w.dca)
		w.dca.selectedStage = 2
		w.refreshStageEditors(&w.dca)

		if got := w.dca.stageRate.GetText(); got != "33" {
			t.Errorf("stage 3 Rate editor after round-trip = %q, want \"33\" (stale table cell)", got)
		}
	})

	t.Run("F: DCF commits also refresh DCF stage table", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t, []string{testVoiceAlpha})
		w := New(m)
		defer w.Close()
		w.Bind(0)

		w.dcf.selectedStage = 1 // stage 2
		w.dcf.stageRate.SetText("75")
		w.commitEnvelopeStage(&w.dcf)

		got := w.dcf.stages.GetCell(stageRow(1), 1).Text
		if got != "75" {
			t.Errorf("DCF stage 2 Rate cell = %q, want \"75\"", got)
		}
	})
}

// contains is a tiny string-contains so this test file doesn't pull in
// the strings package just for one check.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
