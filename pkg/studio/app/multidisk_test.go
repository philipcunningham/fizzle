package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/studio/audio"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
)

// TestAudition_MultiDiskHalfRefusesGracefully drives a real two-disk
// JUNGLISM split, loads disk 1 (which carries the full voice table),
// drills into Layout's first bank, and triggers audition. The guard
// in handleAudition should set a clear "this is part of a 2-disk
// full dump" status rather than letting the audio engine fail with
// "empty gen range" from voice offsets that point at audio living
// on disk 2.
func TestAudition_MultiDiskHalfRefusesGracefully(t *testing.T) {
	sfz := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM.sfz")
	if _, err := os.Stat(sfz); err != nil {
		t.Skipf("missing JUNGLISM.sfz fixture: %v", err)
	}
	dir := t.TempDir()
	prefix := filepath.Join(dir, "JUNGLISM")
	if err := sfzconvert.ConvertMultiDisk(context.Background(), sfz, prefix, 36000); err != nil {
		t.Fatalf("ConvertMultiDisk: %v", err)
	}

	audio.InstallNoopForTest(t)
	fc := newFakeClock()
	a := New(dir)
	a.tick = fc.Tick
	a.toast.SetClock(fc.Tick)
	a.status.SetClock(fc.Tick)

	m, info, err := loader.LoadContainer(prefix + "-1.img")
	if err != nil {
		t.Fatalf("LoadContainer(disk 1): %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	a.current = minimap.Layout
	a.layout.Apply(nav.Confirm)

	updated, _ := a.handleAudition()
	a2, _ := updated.(App)
	got := stripANSI(a2.status.View())

	if strings.Contains(got, "empty gen range") {
		t.Errorf("status leaked low-level decode error: %q", got)
	}
	if !strings.Contains(got, "2-disk") && !strings.Contains(got, "multi-disk") {
		t.Errorf("expected multi-disk guard message, got: %q", got)
	}
}
