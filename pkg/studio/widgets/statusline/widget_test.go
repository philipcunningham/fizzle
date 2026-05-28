package statusline

import (
	"strings"
	"testing"
)

func TestNewClearText(t *testing.T) {
	t.Parallel()
	w := New()
	if got := w.Text(); got != "" {
		t.Errorf("fresh status line Text = %q, want empty", got)
	}
}

func TestSetErrorIncludesRedTag(t *testing.T) {
	t.Parallel()
	w := New()
	w.SetError("save failed")
	got := w.Text()
	if !strings.Contains(got, "[red]") {
		t.Errorf("SetError text %q missing [red] tag", got)
	}
	if !strings.Contains(got, "save failed") {
		t.Errorf("SetError text %q missing message", got)
	}
	if !strings.Contains(got, "[-]") {
		t.Errorf("SetError text %q missing reset tag", got)
	}
}

func TestSetWarningIncludesYellowTag(t *testing.T) {
	t.Parallel()
	w := New()
	w.SetWarning("watch out")
	got := w.Text()
	if !strings.Contains(got, "[yellow]") {
		t.Errorf("SetWarning text %q missing [yellow] tag", got)
	}
	if !strings.Contains(got, "watch out") {
		t.Errorf("SetWarning text %q missing message", got)
	}
}

func TestSetInfoUsesInfoTag(t *testing.T) {
	t.Parallel()
	w := New()
	w.SetInfo("ready")
	got := w.Text()
	if !strings.Contains(got, "[white]") {
		t.Errorf("SetInfo text %q missing [white] tag", got)
	}
	if !strings.Contains(got, "ready") {
		t.Errorf("SetInfo text %q missing message", got)
	}
}

func TestClearEmptiesText(t *testing.T) {
	t.Parallel()
	w := New()
	w.SetError("something")
	w.Clear()
	if got := w.Text(); got != "" {
		t.Errorf("Text after Clear = %q, want empty", got)
	}
}

func TestSequentialCallsReplace(t *testing.T) {
	t.Parallel()
	w := New()
	w.SetError("first")
	w.SetInfo("second")
	got := w.Text()
	if strings.Contains(got, "first") {
		t.Errorf("SetInfo did not replace prior SetError; text = %q", got)
	}
	if !strings.Contains(got, "second") {
		t.Errorf("Text missing latest message; text = %q", got)
	}
}

func TestPrimitiveReturnsNonNil(t *testing.T) {
	t.Parallel()
	w := New()
	if w.Primitive() == nil {
		t.Fatal("Primitive returned nil")
	}
}
