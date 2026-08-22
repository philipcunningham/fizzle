package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
)

// The flags are stringly typed, so deleting one leaves any call site
// reading it compiling and quietly yielding Unchanged. Nothing else
// notices, which is why this asserts on the flag list itself.
func TestRetiredLFOFlagsAreGone(t *testing.T) {
	names := map[string]bool{}
	for _, f := range editFlags() {
		for _, n := range f.Names() {
			names[n] = true
		}
	}

	// The panel has no independent attack row: its DELAY row derives
	// the attack and writes it. It has no resonance depth row at all.
	for _, gone := range []string{"lfo-attack", "lfo-q"} {
		if names[gone] {
			t.Errorf("flag --%s still exists; the panel has no control for it", gone)
		}
	}

	// The panel's LFO SYNC row does earn a flag.
	if !names["lfo-sync"] {
		t.Error("flag --lfo-sync missing; the panel's LFO SYNC row needs one")
	}
}

// Both surfaces speak the panel's scale, so the usage strings have to
// say what the panel says rather than what the byte holds.
func TestEditFlagUsageNamesThePanelScale(t *testing.T) {
	usage := map[string]string{}
	for _, f := range editFlags() {
		if df, ok := f.(interface {
			Names() []string
			GetUsage() string
		}); ok {
			usage[df.Names()[0]] = df.GetUsage()
		}
	}

	if got := usage["tune"]; got == "" {
		t.Fatal("the tune flag has no usage string")
	} else if !strings.Contains(got, "cents") {
		t.Errorf("tune usage = %q, want it to name cents", got)
	}
	if got := usage["vel-dcq-kf"]; got == "" {
		t.Fatal("the vel-dcq-kf flag has no usage string")
	} else if strings.Contains(got, "-127") {
		t.Errorf("vel-dcq-kf usage = %q, but the panel's row is unsigned", got)
	}
	// The panel's DELAY row stops at 127. Advertising the stored word's
	// range invites a value the flag then refuses.
	if got := usage["lfo-delay"]; got == "" {
		t.Fatal("the lfo-delay flag has no usage string")
	} else if strings.Contains(got, "65535") {
		t.Errorf("lfo-delay usage = %q, but the panel's row stops at 127", got)
	}
}

// The tune flag speaks the panel's cents, so it has to convert before
// storing. A missing conversion is silent: it writes a plausible word
// that reads back as a different, ordinary-looking number.
func TestTuneFlagConvertsCentsToTheStoredWord(t *testing.T) {
	for _, c := range []struct {
		cents int
		word  uint16
	}{
		{0, 0},
		{50, 127},
		{100, 255},
		{-100, 0xFF01}, // -255 as a two's complement word
	} {
		cmd := &cli.Command{Flags: editFlags()}
		if err := cmd.Set("tune", strconv.Itoa(c.cents)); err != nil {
			t.Fatalf("set tune=%d: %v", c.cents, err)
		}
		patches, err := collectMetaPatches(cmd)
		if err != nil {
			t.Fatalf("collectMetaPatches: %v", err)
		}
		var found bool
		for _, p := range patches {
			if p.Offset == disk.VoiceDCPOffset {
				found = true
				if p.Value != c.word {
					t.Errorf("--tune %d stores %#04x, want %#04x", c.cents, p.Value, c.word)
				}
			}
		}
		if !found {
			t.Errorf("--tune %d produced no patch at the dcp offset", c.cents)
		}
	}
}

// --lfo-sync shares its byte with the waveform, so it has to keep the
// waveform bits. Reconstructing the byte from the sync flag alone
// silently resets the waveform to sine.
func TestLFOSyncFlagKeepsTheWaveform(t *testing.T) {
	cmd := &cli.Command{Flags: editFlags()}
	if err := cmd.Set("lfo-sync", "on"); err != nil {
		t.Fatalf("set lfo-sync: %v", err)
	}
	// A voice already carrying triangle (index 3) with sync off.
	params := &fzvinfo.VoiceParams{LFOName: 3}

	patches, err := collectLFOPatches(cmd, params)
	if err != nil {
		t.Fatalf("collectLFOPatches: %v", err)
	}
	var found bool
	for _, p := range patches {
		if p.Offset == disk.VoiceLFONameOffset {
			found = true
			if p.Value != 0x83 {
				t.Errorf("--lfo-sync on wrote %#02x, want 0x83 (triangle kept, sync set)", p.Value)
			}
		}
	}
	if !found {
		t.Error("--lfo-sync on produced no patch at the lfo_name offset")
	}
}

// Both flags write the same byte, so setting them together has to yield
// one patch carrying both halves. Two patches at one offset means the
// last one wins and the other edit vanishes.
func TestLFOWaveAndSyncTogetherKeepBoth(t *testing.T) {
	cmd := &cli.Command{Flags: editFlags()}
	if err := cmd.Set("lfo-wave", "triangle"); err != nil {
		t.Fatalf("set lfo-wave: %v", err)
	}
	if err := cmd.Set("lfo-sync", "on"); err != nil {
		t.Fatalf("set lfo-sync: %v", err)
	}
	// A voice on sine with sync off.
	params := &fzvinfo.VoiceParams{LFOName: 0}

	patches, err := collectLFOPatches(cmd, params)
	if err != nil {
		t.Fatalf("collectLFOPatches: %v", err)
	}
	var atName []uint16
	for _, p := range patches {
		if p.Offset == disk.VoiceLFONameOffset {
			atName = append(atName, p.Value)
		}
	}
	if len(atName) != 1 {
		t.Fatalf("got %d patches at the lfo_name offset, want 1: %v", len(atName), atName)
	}
	if atName[0] != 0x83 {
		t.Errorf("wave and sync together wrote %#02x, want 0x83 (triangle with sync on)", atName[0])
	}
}
