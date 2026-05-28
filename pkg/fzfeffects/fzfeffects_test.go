package fzfeffects

import (
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

func readEffectByte(t *testing.T, path string, fieldOffset int) byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data[disk.BankEffectOffset+fieldOffset]
}

// TestEffectOffsetsMatchSpec pins every effect-block field offset used by
// fzfeffects to the offsets given in the FZ-1 Data Structures spec section
// 2-3 (`struct effectdata`). Originally a regression test for an off-by-one
// where aft_lfp was incorrectly mapped to byte 18 (aft_lfa) instead of 17;
// now also covers the 18 additional routings exposed by Tier 2L.
func TestEffectOffsetsMatchSpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"bend", disk.EffectBendOffset, 0x00},
		{"mvol", disk.EffectMVolOffset, 0x01},
		{"suss", disk.EffectSusSOffset, 0x02},
		{"mod_lfp", disk.EffectModLFPOffset, 0x03},
		{"mod_lfa", disk.EffectModLFAOffset, 0x04},
		{"mod_lff", disk.EffectModLFFOffset, 0x05},
		{"mod_lfq", disk.EffectModLFQOffset, 0x06},
		{"mod_dcf", disk.EffectModDCFOffset, 0x07},
		{"mod_dca", disk.EffectModDCAOffset, 0x08},
		{"mod_dcq", disk.EffectModDCQOffset, 0x09},
		{"fot_lfp", disk.EffectFotLFPOffset, 0x0A},
		{"fot_lfa", disk.EffectFotLFAOffset, 0x0B},
		{"fot_lff", disk.EffectFotLFFOffset, 0x0C},
		{"fot_lfq", disk.EffectFotLFQOffset, 0x0D},
		{"fot_dca", disk.EffectFotDCAOffset, 0x0E},
		{"fot_dcf", disk.EffectFotDCFOffset, 0x0F},
		{"fot_dcq", disk.EffectFotDCQOffset, 0x10},
		{"aft_lfp", disk.EffectAftLFPOffset, 0x11},
		{"aft_lfa", disk.EffectAftLFAOffset, 0x12},
		{"aft_lff", disk.EffectAftLFFOffset, 0x13},
		{"aft_lfq", disk.EffectAftLFQOffset, 0x14},
		{"aft_dca", disk.EffectAftDCAOffset, 0x15},
		{"aft_dcf", disk.EffectAftDCFOffset, 0x16},
		{"aft_dcq", disk.EffectAftDCQOffset, 0x17},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("Effect%sOffset = 0x%02x, want 0x%02x (spec §2-3)", c.name, c.got, c.want)
		}
	}

	// Cross-check the absolute byte position in a freshly built FZF: the
	// "aft_lfp=8" default in voicebuild.defaultEffectData must land at
	// bank offset 0x3c0 + 0x11 = 0x3d1, not 0x3d2.
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	if got := readEffectByte(t, p, disk.EffectAftLFPOffset); got != 0x08 {
		t.Errorf("byte at aft_lfp offset = 0x%02x, want 0x08 (default aftertouch-LFP)", got)
	}
	// And the byte at offset 0x12 (aft_lfa) must NOT be 0x08; that would
	// mean we're still writing the value at the wrong position.
	if got := readEffectByte(t, p, 0x12); got != 0x00 {
		t.Errorf("byte at aft_lfa offset = 0x%02x, want 0x00 (the default 8 belongs at 0x11, not 0x12)", got)
	}
}

func TestParseDefaults(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A", "B"})
	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if params.BendRange != 0x18 {
		t.Errorf("BendRange: got %d, want %d", params.BendRange, 0x18)
	}
	if params.ModLFP != 0x0f {
		t.Errorf("ModLFP: got %d, want %d", params.ModLFP, 0x0f)
	}
	if params.FotDCA != 0x40 {
		t.Errorf("FotDCA: got %d, want %d", params.FotDCA, 0x40)
	}
	if params.AftLFP != 0x08 {
		t.Errorf("AftLFP: got %d, want %d", params.AftLFP, 0x08)
	}
}

func TestSetBendRange(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	sp := Unchanged()
	sp.BendRange = 48
	res, err := Set(p, sp)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("expected Changed=true")
	}
	if res.After.BendRange != 48 {
		t.Errorf("After.BendRange: got %d, want 48", res.After.BendRange)
	}
	if readEffectByte(t, p, disk.EffectBendOffset) != 48 {
		t.Errorf("raw byte: got %d, want 48", readEffectByte(t, p, disk.EffectBendOffset))
	}
}

func TestSetModLFP(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	sp := Unchanged()
	sp.ModLFP = 100
	res, err := Set(p, sp)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("expected Changed=true")
	}
	if res.After.ModLFP != 100 {
		t.Errorf("After.ModLFP: got %d, want 100", res.After.ModLFP)
	}
}

func TestSetAllFields(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	sp := Unchanged()
	sp.BendRange = 96
	sp.ModLFP = 50
	sp.FotDCA = 80
	sp.AftLFP = 30
	res, err := Set(p, sp)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("expected Changed=true")
	}
	if res.After.BendRange != 96 || res.After.ModLFP != 50 || res.After.FotDCA != 80 || res.After.AftLFP != 30 {
		t.Errorf("unexpected After: %+v", res.After)
	}
}

// TestSetExtendedRoutings exercises the 18 routing fields beyond the
// original four (bend, mod_lfp, fot_dca, aft_lfp). It writes one byte per
// routing, reads back, and confirms the per-field offsets match the spec
// layout.
func TestSetExtendedRoutings(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})

	sp := Unchanged()
	sp.ModLFA = 11
	sp.ModLFF = 12
	sp.ModLFQ = 13
	sp.ModDCF = 14
	sp.ModDCA = 15
	sp.ModDCQ = 16
	sp.FotLFP = 21
	sp.FotLFA = 22
	sp.FotLFF = 23
	sp.FotLFQ = 24
	sp.FotDCF = 25
	sp.FotDCQ = 26
	sp.AftLFA = 31
	sp.AftLFF = 32
	sp.AftLFQ = 33
	sp.AftDCA = 34
	sp.AftDCF = 35
	sp.AftDCQ = 36

	res, err := Set(p, sp)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("expected Changed=true")
	}

	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"ModLFA", params.ModLFA, 11},
		{"ModLFF", params.ModLFF, 12},
		{"ModLFQ", params.ModLFQ, 13},
		{"ModDCF", params.ModDCF, 14},
		{"ModDCA", params.ModDCA, 15},
		{"ModDCQ", params.ModDCQ, 16},
		{"FotLFP", params.FotLFP, 21},
		{"FotLFA", params.FotLFA, 22},
		{"FotLFF", params.FotLFF, 23},
		{"FotLFQ", params.FotLFQ, 24},
		{"FotDCF", params.FotDCF, 25},
		{"FotDCQ", params.FotDCQ, 26},
		{"AftLFA", params.AftLFA, 31},
		{"AftLFF", params.AftLFF, 32},
		{"AftLFQ", params.AftLFQ, 33},
		{"AftDCA", params.AftDCA, 34},
		{"AftDCF", params.AftDCF, 35},
		{"AftDCQ", params.AftDCQ, 36},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestSetNoChange(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	sp := Unchanged()
	res, err := Set(p, sp)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("expected Changed=false when nothing modified")
	}
}

func TestSetNoChangeSkipsWrite(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	before, _ := os.ReadFile(p)
	sp := Unchanged()
	_, err := Set(p, sp)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Error("file should not change when no parameters modified")
	}
}

func TestSetInvalidBendRange(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	sp := Unchanged()
	sp.BendRange = 200
	_, err := Set(p, sp)
	if err == nil {
		t.Error("expected error for bend range > 127")
	}
}

func TestSetInvalidNegative(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	sp := Unchanged()
	sp.ModLFP = -5
	_, err := Set(p, sp)
	if err == nil {
		t.Error("expected error for negative value")
	}
}

func TestSetRoundTrip(t *testing.T) {
	t.Parallel()
	_, p := fzfbuilder.MakeTestFZF(t, []string{"A"})
	sp := SetParams{BendRange: 32, ModLFP: 64, FotDCA: 100, AftLFP: 50}
	if _, err := Set(p, sp); err != nil {
		t.Fatal(err)
	}
	params, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if params.BendRange != 32 || params.ModLFP != 64 || params.FotDCA != 100 || params.AftLFP != 50 {
		t.Errorf("round-trip mismatch: %+v", params)
	}
}

func TestSetMissingFile(t *testing.T) {
	t.Parallel()
	sp := Unchanged()
	sp.BendRange = 24
	_, err := Set("/nonexistent/path.fzf", sp)
	if err == nil {
		t.Error("expected error for missing file")
	}
}
