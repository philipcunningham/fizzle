package main

import (
	"strconv"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/pkg/disk"
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
	} else if !contains(got, "cents") {
		t.Errorf("tune usage = %q, want it to name cents", got)
	}
	if got := usage["vel-dcq-kf"]; got == "" {
		t.Fatal("the vel-dcq-kf flag has no usage string")
	} else if contains(got, "-127") {
		t.Errorf("vel-dcq-kf usage = %q, but the panel's row is unsigned", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
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
