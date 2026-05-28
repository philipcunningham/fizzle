// Package fzfeffects implements the 'fizzle fzf effects' command. It reads
// and modifies the 24-byte global effect block (struct efectdata) in an FZF
// full dump's bank sector.
//
// The effect block controls how the sampler routes performance controllers
// (pitch bend, mod wheel, foot pedal, aftertouch) to the synthesis engine.
// The block lives at offset 0x3c0 in each bank sector; fizzle targets
// bank 0 (the first and usually only bank).
package fzfeffects

import (
	"fmt"
	"io"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/render"
)

const unchanged = -1

// Params holds the effect block fields exposed by fizzle. The bend field is
// the pitch-bender depth (1/8-semitone units, 0-127). MVol and SusS are the
// "unused, normally 0" mvol/suss bytes (spec §2-3); they are surfaced so
// non-default values become visible. All other fields are the 18 active
// controller -> target routings (0-127). The naming follows the FZ-1 spec
// matrix: <controller>_<target>, where controllers are mod/fot/aft (mod
// wheel, foot pedal, aftertouch) and targets are lfp/lfa/lff/lfq (LFO
// pitch/amp/filter/resonance) and dca/dcf/dcq (amp/filter/resonance
// offset).
type Params struct {
	BendRange int // pitch bend range in 1/8-semitone units (0-127)
	MVol      int // master volume (unused, normally 0)
	SusS      int // sustain switch (unused, normally 0)

	ModLFP int // mod wheel to LFO pitch depth
	ModLFA int // mod wheel to LFO amp depth
	ModLFF int // mod wheel to LFO filter depth
	ModLFQ int // mod wheel to LFO resonance depth
	ModDCF int // mod wheel to filter offset
	ModDCA int // mod wheel to amp offset
	ModDCQ int // mod wheel to resonance offset

	FotLFP int // foot pedal to LFO pitch depth
	FotLFA int // foot pedal to LFO amp depth
	FotLFF int // foot pedal to LFO filter depth
	FotLFQ int // foot pedal to LFO resonance depth
	FotDCA int // foot pedal to amp offset (volume)
	FotDCF int // foot pedal to filter offset
	FotDCQ int // foot pedal to resonance offset

	AftLFP int // aftertouch to LFO pitch depth
	AftLFA int // aftertouch to LFO amp depth
	AftLFF int // aftertouch to LFO filter depth
	AftLFQ int // aftertouch to LFO resonance depth
	AftDCA int // aftertouch to amp offset
	AftDCF int // aftertouch to filter offset
	AftDCQ int // aftertouch to resonance offset
}

// Parse reads the effect block from bank 0 of the FZF at path.
func Parse(path string) (*Params, error) {
	data, _, err := fzutil.ReadFZF(path)
	if err != nil {
		return nil, fmt.Errorf("fzfeffects: %w", err)
	}
	return parseBlock(data), nil
}

func parseBlock(data []byte) *Params {
	base := disk.BankEffectOffset
	return &Params{
		BendRange: int(data[base+disk.EffectBendOffset]),
		MVol:      int(data[base+disk.EffectMVolOffset]),
		SusS:      int(data[base+disk.EffectSusSOffset]),

		ModLFP: int(data[base+disk.EffectModLFPOffset]),
		ModLFA: int(data[base+disk.EffectModLFAOffset]),
		ModLFF: int(data[base+disk.EffectModLFFOffset]),
		ModLFQ: int(data[base+disk.EffectModLFQOffset]),
		ModDCF: int(data[base+disk.EffectModDCFOffset]),
		ModDCA: int(data[base+disk.EffectModDCAOffset]),
		ModDCQ: int(data[base+disk.EffectModDCQOffset]),

		FotLFP: int(data[base+disk.EffectFotLFPOffset]),
		FotLFA: int(data[base+disk.EffectFotLFAOffset]),
		FotLFF: int(data[base+disk.EffectFotLFFOffset]),
		FotLFQ: int(data[base+disk.EffectFotLFQOffset]),
		FotDCA: int(data[base+disk.EffectFotDCAOffset]),
		FotDCF: int(data[base+disk.EffectFotDCFOffset]),
		FotDCQ: int(data[base+disk.EffectFotDCQOffset]),

		AftLFP: int(data[base+disk.EffectAftLFPOffset]),
		AftLFA: int(data[base+disk.EffectAftLFAOffset]),
		AftLFF: int(data[base+disk.EffectAftLFFOffset]),
		AftLFQ: int(data[base+disk.EffectAftLFQOffset]),
		AftDCA: int(data[base+disk.EffectAftDCAOffset]),
		AftDCF: int(data[base+disk.EffectAftDCFOffset]),
		AftDCQ: int(data[base+disk.EffectAftDCQOffset]),
	}
}

// SetParams specifies which effect parameters to change. Use -1 for any
// field to leave it unchanged. The MVol and SusS fields are exposed so
// fizzle can preserve non-default bytes, but the spec marks them unused;
// callers normally leave them at -1.
type SetParams struct {
	BendRange int
	MVol      int
	SusS      int

	ModLFP int
	ModLFA int
	ModLFF int
	ModLFQ int
	ModDCF int
	ModDCA int
	ModDCQ int

	FotLFP int
	FotLFA int
	FotLFF int
	FotLFQ int
	FotDCA int
	FotDCF int
	FotDCQ int

	AftLFP int
	AftLFA int
	AftLFF int
	AftLFQ int
	AftDCA int
	AftDCF int
	AftDCQ int
}

// Unchanged returns a SetParams with all fields set to unchanged.
func Unchanged() SetParams {
	return SetParams{
		BendRange: unchanged,
		MVol:      unchanged,
		SusS:      unchanged,

		ModLFP: unchanged,
		ModLFA: unchanged,
		ModLFF: unchanged,
		ModLFQ: unchanged,
		ModDCF: unchanged,
		ModDCA: unchanged,
		ModDCQ: unchanged,

		FotLFP: unchanged,
		FotLFA: unchanged,
		FotLFF: unchanged,
		FotLFQ: unchanged,
		FotDCA: unchanged,
		FotDCF: unchanged,
		FotDCQ: unchanged,

		AftLFP: unchanged,
		AftLFA: unchanged,
		AftLFF: unchanged,
		AftLFQ: unchanged,
		AftDCA: unchanged,
		AftDCF: unchanged,
		AftDCQ: unchanged,
	}
}

// Result holds the outcome of a Set operation.
type Result struct {
	Before  Params
	After   Params
	Changed bool
}

// effectFieldSpec describes one settable byte in the effect block: the
// caller-facing flag name (for error messages), the byte offset (relative to
// BankEffectOffset), a pointer to the SetParams field, and the upper bound
// of the byte's valid range (always 127 for the routings; bend uses
// MaxBendRange which is also 127).
type effectFieldSpec struct {
	flagName string
	offset   int
	maxValue int
	value    *int
}

// effectFieldSpecs returns the table of settable effect-block bytes. The
// returned slice references &p.<field> so callers can iterate and apply each
// non-unchanged value uniformly.
func effectFieldSpecs(p *SetParams) []effectFieldSpec {
	return []effectFieldSpec{
		{"bend", disk.EffectBendOffset, disk.MaxBendRange, &p.BendRange},
		{"mvol", disk.EffectMVolOffset, 127, &p.MVol},
		{"suss", disk.EffectSusSOffset, 127, &p.SusS},

		{"mod-lfp", disk.EffectModLFPOffset, 127, &p.ModLFP},
		{"mod-lfa", disk.EffectModLFAOffset, 127, &p.ModLFA},
		{"mod-lff", disk.EffectModLFFOffset, 127, &p.ModLFF},
		{"mod-lfq", disk.EffectModLFQOffset, 127, &p.ModLFQ},
		{"mod-dcf", disk.EffectModDCFOffset, 127, &p.ModDCF},
		{"mod-dca", disk.EffectModDCAOffset, 127, &p.ModDCA},
		{"mod-dcq", disk.EffectModDCQOffset, 127, &p.ModDCQ},

		{"foot-lfp", disk.EffectFotLFPOffset, 127, &p.FotLFP},
		{"foot-lfa", disk.EffectFotLFAOffset, 127, &p.FotLFA},
		{"foot-lff", disk.EffectFotLFFOffset, 127, &p.FotLFF},
		{"foot-lfq", disk.EffectFotLFQOffset, 127, &p.FotLFQ},
		{"foot-dca", disk.EffectFotDCAOffset, 127, &p.FotDCA},
		{"foot-dcf", disk.EffectFotDCFOffset, 127, &p.FotDCF},
		{"foot-dcq", disk.EffectFotDCQOffset, 127, &p.FotDCQ},

		{"aftertouch-lfp", disk.EffectAftLFPOffset, 127, &p.AftLFP},
		{"aftertouch-lfa", disk.EffectAftLFAOffset, 127, &p.AftLFA},
		{"aftertouch-lff", disk.EffectAftLFFOffset, 127, &p.AftLFF},
		{"aftertouch-lfq", disk.EffectAftLFQOffset, 127, &p.AftLFQ},
		{"aftertouch-dca", disk.EffectAftDCAOffset, 127, &p.AftDCA},
		{"aftertouch-dcf", disk.EffectAftDCFOffset, 127, &p.AftDCF},
		{"aftertouch-dcq", disk.EffectAftDCQOffset, 127, &p.AftDCQ},
	}
}

// Set modifies the effect block in the FZF at path. Fields set to -1 are
// left unchanged. The file is written atomically only if at least one field
// differs from the current value.
func Set(path string, p SetParams) (Result, error) {
	data, _, err := fzutil.ReadFZF(path)
	if err != nil {
		return Result{}, fmt.Errorf("fzfeffects: %w", err)
	}

	before := *parseBlock(data)
	base := disk.BankEffectOffset

	for _, spec := range effectFieldSpecs(&p) {
		v := *spec.value
		if v == unchanged {
			continue
		}
		if v < 0 || v > spec.maxValue {
			return Result{}, fmt.Errorf("fzfeffects: %s must be 0 to %d, got %d", spec.flagName, spec.maxValue, v)
		}
		data[base+spec.offset] = byte(v) //nolint:gosec // validated above
	}

	after := *parseBlock(data)
	changed := before != after

	if !changed {
		return Result{Before: before, After: after, Changed: false}, nil
	}

	if err := fileutil.WriteAtomic(path, data); err != nil {
		return Result{}, fmt.Errorf("fzfeffects: writing %q: %w", path, err)
	}

	return Result{Before: before, After: after, Changed: true}, nil
}

// renderField is one row in the human-readable matrix output. Pairing the
// label with the value lets Render emit the spec's mod/fot/aft routing
// matrix without repeating Printf calls for each cell.
type renderField struct {
	label string
	value int
}

// Effect-target label constants used in the rendered routing matrix. The
// labels match the spec §2-3 effectdata field-name suffixes so users
// scanning Render output can correlate with the spec or the CLI flag
// names (e.g. --mod-lfa, --aftertouch-dcq).
const (
	labelLFP = "lfp" // LFO pitch
	labelLFA = "lfa" // LFO amp
	labelLFF = "lff" // LFO filter
	labelLFQ = "lfq" // LFO resonance
	labelDCA = "dca" // amp offset
	labelDCF = "dcf" // filter offset
	labelDCQ = "dcq" // resonance offset
)

// Render writes a human-readable summary of the effect block to w.
func Render(w io.Writer, p *Params) {
	semitones := float64(p.BendRange) / 8.0
	render.Printf(w, "Effect parameters:\n")
	render.Printf(w, "  Bend range:    %d (%.1f semitones)\n", p.BendRange, semitones)
	if p.MVol != 0 || p.SusS != 0 {
		render.Printf(w, "  Master volume: %d (spec: unused)\n", p.MVol)
		render.Printf(w, "  Sustain sw:    %d (spec: unused)\n", p.SusS)
	}

	type row struct {
		header string
		fields []renderField
	}
	rows := []row{
		{"Mod wheel:    ", []renderField{
			{labelLFP, p.ModLFP}, {labelLFA, p.ModLFA}, {labelLFF, p.ModLFF}, {labelLFQ, p.ModLFQ},
			{labelDCA, p.ModDCA}, {labelDCF, p.ModDCF}, {labelDCQ, p.ModDCQ},
		}},
		{"Foot pedal:   ", []renderField{
			{labelLFP, p.FotLFP}, {labelLFA, p.FotLFA}, {labelLFF, p.FotLFF}, {labelLFQ, p.FotLFQ},
			{labelDCA, p.FotDCA}, {labelDCF, p.FotDCF}, {labelDCQ, p.FotDCQ},
		}},
		{"Aftertouch:   ", []renderField{
			{labelLFP, p.AftLFP}, {labelLFA, p.AftLFA}, {labelLFF, p.AftLFF}, {labelLFQ, p.AftLFQ},
			{labelDCA, p.AftDCA}, {labelDCF, p.AftDCF}, {labelDCQ, p.AftDCQ},
		}},
	}
	for _, r := range rows {
		render.Printf(w, "  %s", r.header)
		for i, f := range r.fields {
			if i > 0 {
				render.Printf(w, "  ")
			}
			render.Printf(w, "%s=%d", f.label, f.value)
		}
		render.Println(w)
	}
}

// renderResultDiff writes "label: before to after" lines for any field that
// changed between res.Before and res.After.
func renderResultDiff(w io.Writer, res Result) {
	type fieldDiff struct {
		label  string
		before int
		after  int
	}
	diffs := []fieldDiff{
		{"Bend range:     ", res.Before.BendRange, res.After.BendRange},
		{"Master volume:  ", res.Before.MVol, res.After.MVol},
		{"Sustain sw:     ", res.Before.SusS, res.After.SusS},

		{"Mod wheel lfp:  ", res.Before.ModLFP, res.After.ModLFP},
		{"Mod wheel lfa:  ", res.Before.ModLFA, res.After.ModLFA},
		{"Mod wheel lff:  ", res.Before.ModLFF, res.After.ModLFF},
		{"Mod wheel lfq:  ", res.Before.ModLFQ, res.After.ModLFQ},
		{"Mod wheel dca:  ", res.Before.ModDCA, res.After.ModDCA},
		{"Mod wheel dcf:  ", res.Before.ModDCF, res.After.ModDCF},
		{"Mod wheel dcq:  ", res.Before.ModDCQ, res.After.ModDCQ},

		{"Foot pedal lfp: ", res.Before.FotLFP, res.After.FotLFP},
		{"Foot pedal lfa: ", res.Before.FotLFA, res.After.FotLFA},
		{"Foot pedal lff: ", res.Before.FotLFF, res.After.FotLFF},
		{"Foot pedal lfq: ", res.Before.FotLFQ, res.After.FotLFQ},
		{"Foot pedal dca: ", res.Before.FotDCA, res.After.FotDCA},
		{"Foot pedal dcf: ", res.Before.FotDCF, res.After.FotDCF},
		{"Foot pedal dcq: ", res.Before.FotDCQ, res.After.FotDCQ},

		{"Aftertouch lfp: ", res.Before.AftLFP, res.After.AftLFP},
		{"Aftertouch lfa: ", res.Before.AftLFA, res.After.AftLFA},
		{"Aftertouch lff: ", res.Before.AftLFF, res.After.AftLFF},
		{"Aftertouch lfq: ", res.Before.AftLFQ, res.After.AftLFQ},
		{"Aftertouch dca: ", res.Before.AftDCA, res.After.AftDCA},
		{"Aftertouch dcf: ", res.Before.AftDCF, res.After.AftDCF},
		{"Aftertouch dcq: ", res.Before.AftDCQ, res.After.AftDCQ},
	}
	for _, d := range diffs {
		if d.before == d.after {
			continue
		}
		if d.label == "Bend range:     " {
			render.Printf(w, "  %s%d to %d (%.1f semitones)\n", d.label, d.before, d.after, float64(d.after)/8.0)
			continue
		}
		render.Printf(w, "  %s%d to %d\n", d.label, d.before, d.after)
	}
}

// RenderResult writes a before/after summary of a Set operation.
func RenderResult(w io.Writer, res Result) {
	if !res.Changed {
		render.Println(w, "No effect parameters changed.")
		return
	}
	render.Println(w, "Effect parameters updated:")
	renderResultDiff(w, res)
}
