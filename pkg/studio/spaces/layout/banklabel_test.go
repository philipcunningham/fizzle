package layout

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// TestBankList_LabelsPerBankCountAsAreas pins N-08: the per-bank count
// in the bank list is a populated-Area count (bstep), not a distinct-
// voice count, so it must be labelled "areas" (column header and row).
// "voices" stays reserved for the distinct-voice total in the top header.
func TestBankList_LabelsPerBankCountAsAreas(t *testing.T) {
	data := make([]byte, 2*disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 3) // bank 0 bstep=3
	m := model.FromBytes("", data)

	lm := New()
	lm.SetContainer(m, loader.ContainerInfo{BankCount: 1, VoiceCount: 3})
	view := lm.View(140, 40)

	if !strings.Contains(view, "areas") {
		t.Errorf("bank list should label the per-bank count column 'areas':\n%s", view)
	}
	if !strings.Contains(view, "(3 areas)") {
		t.Errorf("per-bank count should render as '(3 areas)':\n%s", view)
	}
}

// TestBankList_EmptyBankReadsEmpty pins F-C: a materialised bank with
// zero areas reads "(empty)", the same as an unmaterialised bank, so the
// internal materialised/unmaterialised split isn't surfaced as "(0
// areas)" vs "(empty)".
func TestBankList_EmptyBankReadsEmpty(t *testing.T) {
	data := make([]byte, disk.SectorSize) // bank 0 materialised, bstep=0
	m := model.FromBytes("", data)

	lm := New()
	lm.SetContainer(m, loader.ContainerInfo{BankCount: 1})
	view := lm.View(140, 40)

	if !strings.Contains(view, "(empty)") {
		t.Errorf("a 0-area bank should read '(empty)':\n%s", view)
	}
	if strings.Contains(view, "(0 areas)") {
		t.Errorf("a 0-area bank should not read '(0 areas)':\n%s", view)
	}
}
