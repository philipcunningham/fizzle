package sfz

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSFZ(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.sfz")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Syntax tests.

func TestLineComment(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
// Created with something
<region>
sample=snare.wav lokey=38 hikey=38 pitch_keycenter=38
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
}

func TestBlockComment(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
/* this is a block comment
   spanning multiple lines */
<region>
sample=kick.wav /* inline block */ lokey=36 hikey=36 pitch_keycenter=36
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
}

func TestStripCommentsUnterminated(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"hello /* world", "hello "},
		{"/* unterminated", ""},
		{"before /* middle \n after", "before "},
	}
	for _, tt := range tests {
		got := stripComments(tt.in)
		if got != tt.want {
			t.Errorf("stripComments(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOpcodesSameLine(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region> sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36 lovel=1 hivel=127
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
	r := regions[0]
	if r.LoKey != 36 || r.HiKey != 36 || r.PitchKeycenter != 36 {
		t.Errorf("keys: got lokey=%d hikey=%d cent=%d", r.LoKey, r.HiKey, r.PitchKeycenter)
	}
}

func TestSamplePathWithSpaces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzPath := filepath.Join(dir, "test.sfz")
	content := "<region>\nsample=JUNGLE Samples/AMEN 01.wav\nlokey=24 hikey=24 pitch_keycenter=24\n"
	if err := os.WriteFile(sfzPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	regions, _, err := Parse(sfzPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(regions[0].Sample, "AMEN 01.wav") {
		t.Errorf("sample path with spaces: got %q", regions[0].Sample)
	}
	if !filepath.IsAbs(regions[0].Sample) {
		t.Errorf("expected absolute path, got %q", regions[0].Sample)
	}
}

// Note name parsing.

func TestParseKeyValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int
	}{
		{"60", 60},
		{"0", 0},
		{"127", 127},
		{"c4", 60},
		{"C4", 60},
		{"c#4", 61},
		{"db4", 61},
		{"a4", 69},
		{"c-1", 0},
		{"g9", 127},
		{"c5", 72},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseKeyValue(tt.in)
			if err != nil {
				t.Fatalf("parseKeyValue(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseKeyValue(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestKeyShorthand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		keyVal  string
		wantKey uint8
	}{
		{"integer", "36", 36},
		{"note_name", "c4", 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sfz := writeSFZ(t, fmt.Sprintf("<region>\nsample=kick.wav\nkey=%s\n", tt.keyVal))
			regions, _, err := Parse(sfz)
			if err != nil {
				t.Fatal(err)
			}
			r := regions[0]
			if r.LoKey != tt.wantKey || r.HiKey != tt.wantKey || r.PitchKeycenter != tt.wantKey {
				t.Errorf("key=%s: lokey=%d hikey=%d cent=%d, want all %d", tt.keyVal, r.LoKey, r.HiKey, r.PitchKeycenter, tt.wantKey)
			}
		})
	}
}

// Inheritance.

func TestGroupInheritance(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<group>
lovel=1 hivel=64
<region>
sample=a.wav lokey=36 hikey=36 pitch_keycenter=36
<region>
sample=b.wav lokey=36 hikey=36 pitch_keycenter=36
lovel=65 hivel=127
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 {
		t.Fatalf("expected 2 regions, got %d", len(regions))
	}
	if regions[0].LoVel != 1 || regions[0].HiVel != 64 {
		t.Errorf("region 1 vel from group: got %d-%d, want 1-64", regions[0].LoVel, regions[0].HiVel)
	}
	if regions[1].LoVel != 65 || regions[1].HiVel != 127 {
		t.Errorf("region 2 vel override: got %d-%d, want 65-127", regions[1].LoVel, regions[1].HiVel)
	}
}

func TestGlobalInheritance(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<global>
lovel=1 hivel=127
<group>
<region>
sample=a.wav lokey=36 hikey=36 pitch_keycenter=36
<group>
hivel=64
<region>
sample=b.wav lokey=48 hikey=48 pitch_keycenter=48
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 {
		t.Fatalf("expected 2 regions, got %d", len(regions))
	}
	if regions[0].HiVel != 127 {
		t.Errorf("region 1 hivel from global: got %d, want 127", regions[0].HiVel)
	}
	if regions[1].HiVel != 64 {
		t.Errorf("region 2 hivel from group: got %d, want 64", regions[1].HiVel)
	}
}

func TestMuteGroup(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=drums.wav lokey=36 hikey=36 pitch_keycenter=36
mutegroup=1
<region>
sample=bass.wav lokey=48 hikey=60 pitch_keycenter=48
mutegroup=2
<region>
sample=pad.wav lokey=72 hikey=84 pitch_keycenter=72
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].MuteGroup != 1 {
		t.Errorf("region 1: mutegroup=%d, want 1", regions[0].MuteGroup)
	}
	if regions[1].MuteGroup != 2 {
		t.Errorf("region 2: mutegroup=%d, want 2", regions[1].MuteGroup)
	}
	if regions[2].MuteGroup != 0 {
		t.Errorf("region 3: mutegroup=%d, want 0 (polyphonic)", regions[2].MuteGroup)
	}
}

func TestMuteGroupInheritedFromGroupHeader(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<group>
mutegroup=3
<region>
sample=a.wav lokey=36 hikey=36 pitch_keycenter=36
<region>
sample=b.wav lokey=37 hikey=37 pitch_keycenter=37
mutegroup=4
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].MuteGroup != 3 {
		t.Errorf("region 1 should inherit mutegroup=3 from group header, got %d", regions[0].MuteGroup)
	}
	if regions[1].MuteGroup != 4 {
		t.Errorf("region 2 should override to mutegroup=4, got %d", regions[1].MuteGroup)
	}
}

func TestMuteGroupZeroIsPolyphonic(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
mutegroup=0
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].MuteGroup != 0 {
		t.Errorf("mutegroup=0 should be polyphonic, got %d", regions[0].MuteGroup)
	}
}

func TestLoopMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opcode  string
		oneShot bool
	}{
		{"one_shot", "loop_mode=one_shot", true},
		{"ONE_SHOT_case_insensitive", "loop_mode=ONE_SHOT", true},
		{"no_loop_mode", "", false},
		{"loop_sustain", "loop_mode=loop_sustain", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := "<region>\nsample=kick.wav lokey=36 hikey=36 pitch_keycenter=36\n"
			if tt.opcode != "" {
				content += tt.opcode + "\n"
			}
			sfz := writeSFZ(t, content)
			regions, _, err := Parse(sfz)
			if err != nil {
				t.Fatal(err)
			}
			if regions[0].OneShot != tt.oneShot {
				t.Errorf("OneShot = %v, want %v", regions[0].OneShot, tt.oneShot)
			}
		})
	}
}

func TestMasterInheritance(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<global>
lovel=1
<master>
hivel=100
<group>
<region>
sample=a.wav lokey=60 hikey=60 pitch_keycenter=60
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].LoVel != 1 {
		t.Errorf("lovel from global: got %d, want 1", regions[0].LoVel)
	}
	if regions[0].HiVel != 100 {
		t.Errorf("hivel from master: got %d, want 100", regions[0].HiVel)
	}
}

// Control header.

func TestDefaultPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzPath := filepath.Join(dir, "test.sfz")
	content := `<control>
default_path=Samples/
<region>
sample=kick.wav
lokey=36 hikey=36 pitch_keycenter=36
`
	if err := os.WriteFile(sfzPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	regions, _, err := Parse(sfzPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(regions[0].Sample, "Samples") {
		t.Errorf("default_path not applied: %q", regions[0].Sample)
	}
	if !strings.HasSuffix(regions[0].Sample, "kick.wav") {
		t.Errorf("sample name missing: %q", regions[0].Sample)
	}
}

func TestDefine(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
#define $ROOT 60
<region>
sample=piano.wav
lokey=58 hikey=62 pitch_keycenter=60
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
}

func TestInclude(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	incPath := filepath.Join(dir, "region.sfz")
	if err := os.WriteFile(incPath, []byte("<region>\nsample=snare.wav\nlokey=38 hikey=38 pitch_keycenter=38\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.sfz")
	content := `#include "region.sfz"
<region>
sample=kick.wav
lokey=36 hikey=36 pitch_keycenter=36
`
	if err := os.WriteFile(mainPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	regions, _, err := Parse(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 {
		t.Fatalf("expected 2 regions from include, got %d", len(regions))
	}
}

func TestUnknownHeaderSkipped(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<curve>
v000=0 v127=1
<region>
sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region (curve skipped), got %d", len(regions))
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "<curve>") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning naming <curve>, got %+v", warns)
	}
}

func TestIncludeCycleWarns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.sfz")
	b := filepath.Join(dir, "b.sfz")
	if err := os.WriteFile(a, []byte(`#include "b.sfz"`+"\n<region>\nsample=x.wav lokey=36 hikey=36 pitch_keycenter=36\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`#include "a.sfz"`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Parse(a)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "a.sfz") && strings.Contains(w.Message, "include") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected include-cycle warning, got %+v", warns)
	}
}

// Warnings.

func TestMissingKeycentreDefaultsTo60(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=bass.wav lokey=48 hikey=59
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].PitchKeycenter != defaultKeycenter {
		t.Errorf("keycenter default: got %d, want %d", regions[0].PitchKeycenter, defaultKeycenter)
	}
}

func TestUnsupportedOpcodeWarns(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
pan=50
`)
	_, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "pan") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning for unsupported 'pan' opcode")
	}
}

func TestTranspose(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=amen.wav lokey=32 hikey=32 pitch_keycenter=32
transpose=1
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].Transpose != 1 {
		t.Errorf("transpose: got %d, want 1", regions[0].Transpose)
	}
}

// TestTransposeAndTuneClampedPositive guards against silent int16 wrap in the
// FZ-1 dcp field: SFZ values beyond ±127 semitones (or ±100 cents) are
// clamped and a warning is emitted.
func TestTransposeAndTuneClampedPositive(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
transpose=200 tune=200
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].Transpose != MaxTranspose {
		t.Errorf("transpose clamp: got %d, want %d", regions[0].Transpose, MaxTranspose)
	}
	if regions[0].Tune != MaxTune {
		t.Errorf("tune clamp: got %d, want %d", regions[0].Tune, MaxTune)
	}
	var sawTranspose, sawTune bool
	for _, w := range warns {
		if strings.Contains(w.Message, "transpose") {
			sawTranspose = true
		}
		if strings.Contains(w.Message, "tune") {
			sawTune = true
		}
	}
	if !sawTranspose {
		t.Errorf("expected transpose-out-of-range warning, got %+v", warns)
	}
	if !sawTune {
		t.Errorf("expected tune-out-of-range warning, got %+v", warns)
	}
}

// TestTransposeClampedNegative verifies the lower-bound clamp also warns.
func TestTransposeClampedNegative(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
transpose=-200
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].Transpose != MinTranspose {
		t.Errorf("transpose clamp: got %d, want %d", regions[0].Transpose, MinTranspose)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "transpose") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected transpose-out-of-range warning, got %+v", warns)
	}
}

// TestTransposeAndTuneInRangeNoWarning verifies in-range values pass through
// unchanged and emit no clamping warning.
func TestTransposeAndTuneInRangeNoWarning(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
transpose=12 tune=50
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].Transpose != 12 {
		t.Errorf("transpose: got %d, want 12", regions[0].Transpose)
	}
	if regions[0].Tune != 50 {
		t.Errorf("tune: got %d, want 50", regions[0].Tune)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "out of range") {
			t.Errorf("unexpected out-of-range warning for in-range values: %s", w.Message)
		}
	}
}

// Limits.

func TestTooManyRegions(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for range 65 {
		sb.WriteString("<region>\n")
		sb.WriteString("sample=x.wav\n")
		sb.WriteString("lokey=36 hikey=36 pitch_keycenter=36\n")
	}
	sfz := writeSFZ(t, sb.String())
	_, _, err := Parse(sfz)
	if err == nil {
		t.Error("expected error for >64 regions")
	}
}

func TestNoRegions(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, "// nothing here\n<global>\nlovel=1\n")
	_, _, err := Parse(sfz)
	if err == nil {
		t.Error("expected error for file with no regions")
	}
}

// Real-world SFZ.

func TestJunglismSFZ(t *testing.T) {
	t.Parallel()
	sfzPath := "../../testdata/synthetic/JUNGLISM.sfz"
	regions, warns, err := Parse(sfzPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 26 {
		t.Errorf("expected 26 regions, got %d", len(regions))
	}
	// First region: amen 01, key 0, mutegroup=0.
	if regions[0].LoKey != 0 || regions[0].HiKey != 0 || regions[0].PitchKeycenter != 0 {
		t.Errorf("region 1: lokey=%d hikey=%d cent=%d, want 0 0 0",
			regions[0].LoKey, regions[0].HiKey, regions[0].PitchKeycenter)
	}
	if regions[0].MuteGroup != 0 {
		t.Errorf("region 1 (amen): mutegroup=%d, want 0", regions[0].MuteGroup)
	}
	if !regions[0].OneShot {
		t.Error("region 1 (amen): expected OneShot=true")
	}
	// 808 (region index 17) has mutegroup=2, transpose=-7.
	if regions[17].MuteGroup != 2 {
		t.Errorf("808 mutegroup: got %d, want 2", regions[17].MuteGroup)
	}
	if regions[17].Transpose != -7 {
		t.Errorf("808 transpose: got %d, want -7", regions[17].Transpose)
	}
	if regions[17].LoKey != 24 || regions[17].HiKey != 35 || regions[17].PitchKeycenter != 24 {
		t.Errorf("808 keys: lokey=%d hikey=%d cent=%d, want 24 35 24",
			regions[17].LoKey, regions[17].HiKey, regions[17].PitchKeycenter)
	}
	// reese (region index 18) has mutegroup=2, no transpose.
	if regions[18].MuteGroup != 2 {
		t.Errorf("reese mutegroup: got %d, want 2", regions[18].MuteGroup)
	}
	if regions[18].Transpose != 0 {
		t.Errorf("reese transpose: got %d, want 0", regions[18].Transpose)
	}
	if regions[18].LoKey != 36 || regions[18].HiKey != 59 || regions[18].PitchKeycenter != 48 {
		t.Errorf("reese keys: lokey=%d hikey=%d cent=%d, want 36 59 48",
			regions[18].LoKey, regions[18].HiKey, regions[18].PitchKeycenter)
	}
	// pad 1 (region index 19) has no pitch_keycenter; parser defaults to 60.
	if regions[19].PitchKeycenter != defaultKeycenter {
		t.Errorf("pad 1 keycenter: got %d, want %d (default)", regions[19].PitchKeycenter, defaultKeycenter)
	}
	if regions[19].LoKey != 60 || regions[19].HiKey != 71 {
		t.Errorf("pad 1 keys: lokey=%d hikey=%d, want 60 71",
			regions[19].LoKey, regions[19].HiKey)
	}
	if regions[19].MuteGroup != 3 {
		t.Errorf("pad 1 mutegroup: got %d, want 3", regions[19].MuteGroup)
	}
	_ = warns
	// ragga 1 (region index 24): lokey=118, pitch_keycenter=118, mutegroup=6, transpose=-2.
	if regions[24].LoKey != 118 || regions[24].HiKey != 118 || regions[24].PitchKeycenter != 118 {
		t.Errorf("ragga 1 keys: lokey=%d hikey=%d cent=%d, want 118 118 118",
			regions[24].LoKey, regions[24].HiKey, regions[24].PitchKeycenter)
	}
	if regions[24].MuteGroup != 6 {
		t.Errorf("ragga 1 mutegroup: got %d, want 6", regions[24].MuteGroup)
	}
	if regions[24].Transpose != -2 {
		t.Errorf("ragga 1 transpose: got %d, want -2", regions[24].Transpose)
	}
	// ragga 2 (region index 25, last region): lokey=119, pitch_keycenter=119, mutegroup=6, transpose=-2.
	if regions[25].LoKey != 119 || regions[25].HiKey != 119 || regions[25].PitchKeycenter != 119 {
		t.Errorf("ragga 2 keys: lokey=%d hikey=%d cent=%d, want 119 119 119",
			regions[25].LoKey, regions[25].HiKey, regions[25].PitchKeycenter)
	}
	if regions[25].MuteGroup != 6 {
		t.Errorf("ragga 2 mutegroup: got %d, want 6", regions[25].MuteGroup)
	}
	if regions[25].Transpose != -2 {
		t.Errorf("ragga 2 transpose: got %d, want -2", regions[25].Transpose)
	}
	// All sample paths should be absolute.
	for i, r := range regions {
		if !filepath.IsAbs(r.Sample) {
			t.Errorf("region %d sample not absolute: %q", i+1, r.Sample)
		}
	}
}

func TestParseOnlyComments(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `// just a comment
// another comment
`)
	_, _, err := Parse(sfz)
	if err == nil {
		t.Error("expected error for SFZ with only comments and no regions")
	}
}

func TestParseRegionMissingSample(t *testing.T) {
	t.Parallel()
	// A region with no sample opcode is skipped with a warning.
	// Parse should succeed (not error) since a <region> tag was present.
	sfz := writeSFZ(t, `
<region>
lokey=36 hikey=36 pitch_keycenter=36
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatalf("Parse should not error when region is skipped: %v", err)
	}
	if len(regions) != 0 {
		t.Errorf("expected 0 valid regions, got %d", len(regions))
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "sample") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about missing sample opcode")
	}
}

func TestParseVelocityBoundaries(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
lovel=1 hivel=127
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].LoVel != 1 || regions[0].HiVel != 127 {
		t.Errorf("velocity: got %d-%d, want 1-127", regions[0].LoVel, regions[0].HiVel)
	}
}

// TestParseVelocityZeroRoundTrip verifies that an SFZ region with
// lovel=0 hivel=0 (as emitted by sfzexport for silenced FZ-1 voices)
// parses back to (0, 0) rather than being clamped to DefaultLoVel=1.
// Without this, FZ-1 voices exported with velocity (0, 0) would change
// to (1, 1) on re-import.
func TestParseVelocityZeroRoundTrip(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
lovel=0 hivel=0
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
	if regions[0].LoVel != 0 || regions[0].HiVel != 0 {
		t.Errorf("velocity round-trip: got %d-%d, want 0-0", regions[0].LoVel, regions[0].HiVel)
	}
}

// TestParseVelocityZeroWarnsSilent verifies that lovel=0 hivel=0 emits a
// warning explaining the voice will be silent on hardware (FZ-1 velocity
// range is 1-127 per spec §1-5).
func TestParseVelocityZeroWarnsSilent(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
lovel=0 hivel=0
`)
	_, warnings, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w.Message, "silent") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected silent-voice warning, got: %v", warnings)
	}
}

// TestParseVelocityDefaultsWhenAbsent verifies that omitted lovel/hivel
// opcodes fall back to the SFZ defaults of 1..127, not 0..127.
func TestParseVelocityDefaultsWhenAbsent(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].LoVel != DefaultLoVel || regions[0].HiVel != DefaultHiVel {
		t.Errorf("velocity defaults: got %d-%d, want %d-%d",
			regions[0].LoVel, regions[0].HiVel, DefaultLoVel, DefaultHiVel)
	}
}

// TestParseVelocityInvertedRangeWarns verifies that hivel < lovel emits
// a warning since the region would never trigger.
func TestParseVelocityInvertedRangeWarns(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
lovel=100 hivel=50
`)
	_, warnings, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w.Message, "hivel") && strings.Contains(w.Message, "lovel") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning for inverted velocity range, got %v", warnings)
	}
}

func TestCircularInclude(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.sfz")
	b := filepath.Join(dir, "b.sfz")
	if err := os.WriteFile(a, []byte(`#include "b.sfz"
<region> sample=test.wav key=60`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`#include "a.sfz"`), 0644); err != nil {
		t.Fatal(err)
	}
	regions, _, err := Parse(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
}

func TestUnterminatedBlockComment(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `/* unterminated block comment
<region>
sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
`)
	_, _, err := Parse(sfz)
	if err == nil {
		t.Fatal("expected error when unterminated block comment swallows all content")
	}
}

func TestMalformedKeyValueUsesDefault(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=kick.wav lokey=abc hikey=xyz pitch_keycenter=36
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].LoKey != 0 {
		t.Errorf("LoKey = %d, want 0 (default for unparseable value)", regions[0].LoKey)
	}
	if regions[0].HiKey != 127 {
		t.Errorf("HiKey = %d, want 127 (default for unparseable value)", regions[0].HiKey)
	}
}

func TestDefaultPathIgnoredUnderGlobal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzPath := filepath.Join(dir, "test.sfz")
	content := `<global>
default_path=ShouldBeIgnored/
<region>
sample=kick.wav
lokey=36 hikey=36 pitch_keycenter=36
`
	if err := os.WriteFile(sfzPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	regions, _, err := Parse(sfzPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(regions[0].Sample, "ShouldBeIgnored") {
		t.Errorf("default_path under <global> should be ignored, but sample path is %q", regions[0].Sample)
	}
}

func TestDefaultPathIgnoredUnderGroup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzPath := filepath.Join(dir, "test.sfz")
	content := `<group>
default_path=ShouldBeIgnored/
<region>
sample=kick.wav
lokey=36 hikey=36 pitch_keycenter=36
`
	if err := os.WriteFile(sfzPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	regions, _, err := Parse(sfzPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(regions[0].Sample, "ShouldBeIgnored") {
		t.Errorf("default_path under <group> should be ignored, but sample path is %q", regions[0].Sample)
	}
}

func TestParseCutoffResonance(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
cutoff=80 resonance=50
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
	if regions[0].Cutoff != 80 {
		t.Errorf("Cutoff = %d, want 80", regions[0].Cutoff)
	}
	if regions[0].Resonance != 50 {
		t.Errorf("Resonance = %d, want 50", regions[0].Resonance)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "cutoff") || strings.Contains(w.Message, "resonance") {
			t.Errorf("unexpected warning for cutoff/resonance: %s", w.Message)
		}
	}
}

func TestParseCutoffResonanceDefaults(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].Cutoff != -1 {
		t.Errorf("Cutoff default = %d, want -1", regions[0].Cutoff)
	}
	if regions[0].Resonance != -1 {
		t.Errorf("Resonance default = %d, want -1", regions[0].Resonance)
	}
}

func TestParseCutoffClamped(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
cutoff=200 resonance=200
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].Cutoff != 127 {
		t.Errorf("Cutoff = %d, want 127 (clamped)", regions[0].Cutoff)
	}
	if regions[0].Resonance != 127 {
		t.Errorf("Resonance = %d, want 127 (clamped)", regions[0].Resonance)
	}
}

func TestParseLoopStartEnd(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
loop_start=100 loop_end=4900
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
	if regions[0].LoopStart != 100 {
		t.Errorf("LoopStart = %d, want 100", regions[0].LoopStart)
	}
	if regions[0].LoopEnd != 4900 {
		t.Errorf("LoopEnd = %d, want 4900", regions[0].LoopEnd)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "loop_start") || strings.Contains(w.Message, "loop_end") {
			t.Errorf("unexpected warning for loop_start/loop_end: %s", w.Message)
		}
	}
}

func TestParseLoopStartEndDefaults(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].LoopStart != -1 {
		t.Errorf("LoopStart default = %d, want -1", regions[0].LoopStart)
	}
	if regions[0].LoopEnd != -1 {
		t.Errorf("LoopEnd default = %d, want -1", regions[0].LoopEnd)
	}
}

func TestParseMuteGroupZeroDistinctFromAbsent(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=a.wav lokey=36 hikey=36 pitch_keycenter=36
mutegroup=0
<region>
sample=b.wav lokey=37 hikey=37 pitch_keycenter=37
`)
	regions, _, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	// Region 0 has mutegroup=0 explicitly: HasMuteGroup=true.
	if !regions[0].HasMuteGroup {
		t.Error("region 0: HasMuteGroup should be true for mutegroup=0")
	}
	// Region 1 has no mutegroup: HasMuteGroup=false.
	if regions[1].HasMuteGroup {
		t.Error("region 1: HasMuteGroup should be false when opcode absent")
	}
}

// TestParseIntMalformedWarns guards a regression where a malformed integer
// opcode (e.g. transpose=foo) was silently swallowed by parseInt. The parser
// now emits a warning so the user can locate the bad opcode.
func TestParseIntMalformedWarns(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
transpose=foo
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	// Default value should be used.
	if regions[0].Transpose != 0 {
		t.Errorf("transpose default on malformed input: got %d, want 0", regions[0].Transpose)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "malformed") && strings.Contains(w.Message, "transpose") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected malformed-transpose warning, got %+v", warns)
	}
}

// TestParseIntMalformedTuneWarns checks parseInt also warns for other opcodes.
func TestParseIntMalformedTuneWarns(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
tune=bar
`)
	_, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "malformed") && strings.Contains(w.Message, "tune") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected malformed-tune warning, got %+v", warns)
	}
}

// TestLovelClampWarns verifies lovel values outside [0, 127] are clamped
// and emit a warning. SFZ spec velocity range is 0-127, so lovel=200 is
// malformed input that previously was silently clamped.
func TestLovelClampWarns(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
lovel=200 hivel=127
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].LoVel != 127 {
		t.Errorf("lovel clamp: got %d, want 127", regions[0].LoVel)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "lovel") && strings.Contains(w.Message, "clamped") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected lovel clamp warning, got %+v", warns)
	}
}

// TestHivelClampWarns mirrors TestLovelClampWarns for hivel.
func TestHivelClampWarns(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
lovel=1 hivel=255
`)
	regions, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if regions[0].HiVel != 127 {
		t.Errorf("hivel clamp: got %d, want 127", regions[0].HiVel)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "hivel") && strings.Contains(w.Message, "clamped") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected hivel clamp warning, got %+v", warns)
	}
}

// TestLovelHivelInRangeNoWarn verifies in-range velocity values do not
// trigger the clamp warning.
func TestLovelHivelInRangeNoWarn(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=x.wav lokey=60 hikey=60 pitch_keycenter=60
lovel=1 hivel=127
`)
	_, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "clamped") && (strings.Contains(w.Message, "lovel") || strings.Contains(w.Message, "hivel")) {
			t.Errorf("unexpected clamp warning for in-range velocity: %s", w.Message)
		}
	}
}

// Path confinement tests.

// hasWarning reports whether any warning's message contains every substring.
func hasWarning(warns []Warning, substrs ...string) bool {
	for _, w := range warns {
		matched := true
		for _, s := range substrs {
			if !strings.Contains(w.Message, s) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func TestIncludeOutsideRootWarns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "pack")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// outside.sfz lives in dir, not in root, so the include escapes root.
	outside := filepath.Join(dir, "outside.sfz")
	if err := os.WriteFile(outside, []byte("<region>\nsample=kick.wav lokey=36 hikey=36 pitch_keycenter=36\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main.sfz")
	if err := os.WriteFile(main, []byte(`#include "../outside.sfz"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Parse(main)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(warns, "#include", "outside the SFZ root") {
		t.Errorf("expected include-outside-root warning, got %+v", warns)
	}
}

func TestIncludeInsideRootDoesNotWarn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	inc := filepath.Join(sub, "inside.sfz")
	if err := os.WriteFile(inc, []byte("<region>\nsample=kick.wav lokey=36 hikey=36 pitch_keycenter=36\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.sfz")
	if err := os.WriteFile(main, []byte(`#include "sub/inside.sfz"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Parse(main)
	if err != nil {
		t.Fatal(err)
	}
	if hasWarning(warns, "outside the SFZ root") {
		t.Errorf("did not expect outside-root warning for nested include, got %+v", warns)
	}
}

func TestNestedIncludeEscapingRootWarns(t *testing.T) {
	// The root is locked to the top-level SFZ's directory. An #include
	// inside a nested SFZ that escapes via "../" should still warn even if
	// the path is relative to its own (already-nested) directory.
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "pack")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// The "escaped" SFZ lives at dir level (outside root).
	escaped := filepath.Join(dir, "escaped.sfz")
	if err := os.WriteFile(escaped, []byte("<region>\nsample=x.wav lokey=36 hikey=36 pitch_keycenter=36\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nested include inside root/sub references ../../escaped.sfz, which
	// is *relative to sub* still inside root only if root's parent is the
	// root; but our rootDir is filepath.Dir(top-level). So this escapes.
	nested := filepath.Join(sub, "nested.sfz")
	if err := os.WriteFile(nested, []byte(`#include "../../escaped.sfz"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main.sfz")
	if err := os.WriteFile(main, []byte(`#include "sub/nested.sfz"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Parse(main)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(warns, "#include", "outside the SFZ root") {
		t.Errorf("expected outside-root warning for nested escaping include, got %+v", warns)
	}
}

func TestSampleAbsolutePathOutsideRootWarns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "pack")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// Use an absolute path that points outside root.
	absSample := filepath.Join(dir, "stranger.wav")
	main := filepath.Join(root, "main.sfz")
	content := fmt.Sprintf("<region>\nsample=%s\nlokey=36 hikey=36 pitch_keycenter=36\n", absSample)
	if err := os.WriteFile(main, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Parse(main)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(warns, "sample=", "outside the SFZ root") {
		t.Errorf("expected outside-root warning for absolute sample path, got %+v", warns)
	}
}

func TestSampleRelativeInsideRootDoesNotWarn(t *testing.T) {
	t.Parallel()
	sfz := writeSFZ(t, `
<region>
sample=Samples/kick.wav lokey=36 hikey=36 pitch_keycenter=36
`)
	_, warns, err := Parse(sfz)
	if err != nil {
		t.Fatal(err)
	}
	if hasWarning(warns, "outside the SFZ root") {
		t.Errorf("did not expect outside-root warning for nested sample, got %+v", warns)
	}
}

func TestSampleParentTraversalOutsideRootWarns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "pack")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main.sfz")
	// "../shared/kick.wav" escapes the pack root.
	content := "<region>\nsample=../shared/kick.wav lokey=36 hikey=36 pitch_keycenter=36\n"
	if err := os.WriteFile(main, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Parse(main)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(warns, "sample=", "outside the SFZ root") {
		t.Errorf("expected outside-root warning for ../ sample path, got %+v", warns)
	}
}

func TestDefaultPathOutsideRootWarns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "pack")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main.sfz")
	content := `<control>
default_path=../shared/
<region>
sample=kick.wav
lokey=36 hikey=36 pitch_keycenter=36
`
	if err := os.WriteFile(main, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Parse(main)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(warns, "default_path", "outside the SFZ root") {
		t.Errorf("expected outside-root warning for default_path=../, got %+v", warns)
	}
}

func TestDefaultPathInsideRootDoesNotWarn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzPath := filepath.Join(dir, "test.sfz")
	content := `<control>
default_path=Samples/
<region>
sample=kick.wav
lokey=36 hikey=36 pitch_keycenter=36
`
	if err := os.WriteFile(sfzPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Parse(sfzPath)
	if err != nil {
		t.Fatal(err)
	}
	if hasWarning(warns, "outside the SFZ root") {
		t.Errorf("did not expect outside-root warning for nested default_path, got %+v", warns)
	}
}
