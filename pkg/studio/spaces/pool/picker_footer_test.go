package pool

import (
	"strings"
	"testing"
)

// TestView_PickerFooterShowsAssignAndCancel pins N-03: in picker mode
// the footer leads with the two primary actions, Enter (assign) and Esc
// (cancel), so the modal whose purpose is "pick one" says which key
// picks. Outside picker mode those tokens are absent.
func TestView_PickerFooterShowsAssignAndCancel(t *testing.T) {
	m := New()
	m.AddFromAreaVoice("AMEN", "bank 1", make([]byte, 256))
	m.SetPickerTarget("Bank 1 / Area 1")
	picker := m.View(120, 30)
	if !strings.Contains(picker, "enter to assign") {
		t.Errorf("picker footer does not show 'enter to assign':\n%s", picker)
	}
	if !strings.Contains(picker, "esc to cancel") {
		t.Errorf("picker footer does not show 'esc to cancel':\n%s", picker)
	}

	m.SetPickerTarget("")
	m.AddFromAreaVoice("AMEN", "bank 1", make([]byte, 256))
	browse := m.View(120, 30)
	if strings.Contains(browse, "enter to assign") {
		t.Errorf("non-picker footer should not advertise assign")
	}
}
