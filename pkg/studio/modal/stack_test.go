package modal

import (
	"testing"

	"github.com/rivo/tview"
)

func TestStackPushAndTop(t *testing.T) {
	t.Parallel()
	pages := tview.NewPages()
	s := NewStack(pages)
	if got := s.Top(); got != "" {
		t.Errorf("Top on empty stack = %q, want \"\"", got)
	}

	s.Push("alpha", tview.NewBox())
	if got := s.Top(); got != "alpha" {
		t.Errorf("Top after one Push = %q, want %q", got, "alpha")
	}
	if !pages.HasPage("alpha") {
		t.Errorf("pages.HasPage(alpha) = false, want true")
	}
}

func TestStackPushPopRoundTrip(t *testing.T) {
	t.Parallel()
	pages := tview.NewPages()
	s := NewStack(pages)

	s.Push("a", tview.NewBox())
	s.Push("b", tview.NewBox())
	s.Push("c", tview.NewBox())

	if s.Depth() != 3 {
		t.Errorf("Depth after 3 pushes = %d, want 3", s.Depth())
	}
	if got := s.Top(); got != "c" {
		t.Errorf("Top = %q, want %q", got, "c")
	}

	s.Pop()
	if got := s.Top(); got != "b" {
		t.Errorf("Top after one Pop = %q, want %q", got, "b")
	}
	if pages.HasPage("c") {
		t.Errorf("page c should be removed after Pop")
	}

	s.Pop()
	s.Pop()
	if s.Depth() != 0 {
		t.Errorf("Depth after 3 Pops = %d, want 0", s.Depth())
	}
	if got := s.Top(); got != "" {
		t.Errorf("Top on emptied stack = %q, want \"\"", got)
	}
}

func TestStackPopEmptyIsNoop(t *testing.T) {
	t.Parallel()
	pages := tview.NewPages()
	s := NewStack(pages)
	// Should not panic and Top stays empty.
	s.Pop()
	s.Pop()
	if got := s.Top(); got != "" {
		t.Errorf("Top after pops on empty stack = %q", got)
	}
}

func TestStackPushReplacesCollision(t *testing.T) {
	t.Parallel()
	pages := tview.NewPages()
	s := NewStack(pages)

	box1 := tview.NewBox()
	box2 := tview.NewBox()
	s.Push("alpha", box1)
	s.Push("alpha", box2) // collision, replace.

	if s.Depth() != 2 {
		t.Errorf("Depth after collision-replace = %d, want 2 (collision still pushes onto stack)", s.Depth())
	}
	if !pages.HasPage("alpha") {
		t.Errorf("pages should still have alpha after collision-replace")
	}
	if got := s.Top(); got != "alpha" {
		t.Errorf("Top = %q, want %q", got, "alpha")
	}
}

func TestStackHas(t *testing.T) {
	t.Parallel()
	pages := tview.NewPages()
	s := NewStack(pages)
	if s.Has("nope") {
		t.Errorf("Has on empty stack returned true")
	}
	s.Push("alpha", tview.NewBox())
	s.Push("bravo", tview.NewBox())
	if !s.Has("alpha") {
		t.Errorf("Has(alpha) = false, want true")
	}
	if !s.Has("bravo") {
		t.Errorf("Has(bravo) = false, want true")
	}
	if s.Has("charlie") {
		t.Errorf("Has(charlie) = true, want false")
	}
	s.Pop()
	if s.Has("bravo") {
		t.Errorf("Has(bravo) after Pop = true, want false")
	}
}

func TestStackUnderlyingPagesNotManaged(t *testing.T) {
	t.Parallel()
	pages := tview.NewPages()
	// Pre-existing page (the main layout) should not be touched by the stack.
	pages.AddPage("main", tview.NewBox(), true, true)
	s := NewStack(pages)

	s.Push("modal", tview.NewBox())
	s.Pop()

	if !pages.HasPage("main") {
		t.Errorf("Stack.Pop removed the unmanaged main page")
	}
	if s.Depth() != 0 {
		t.Errorf("Depth after Pop = %d, want 0", s.Depth())
	}
}

func TestStackDeepStacking(t *testing.T) {
	t.Parallel()
	pages := tview.NewPages()
	s := NewStack(pages)

	const N = 10
	for i := 0; i < N; i++ {
		s.Push(pageName(i), tview.NewBox())
	}
	if s.Depth() != N {
		t.Errorf("Depth = %d, want %d", s.Depth(), N)
	}
	if got := s.Top(); got != pageName(N-1) {
		t.Errorf("Top = %q, want %q", got, pageName(N-1))
	}

	for i := N - 1; i >= 0; i-- {
		if got := s.Top(); got != pageName(i) {
			t.Errorf("Top at depth %d = %q, want %q", i, got, pageName(i))
		}
		s.Pop()
	}
	if s.Depth() != 0 {
		t.Errorf("final Depth = %d, want 0", s.Depth())
	}
}

func pageName(i int) string {
	//nolint:gosec // G115: i is bounded by the test loop (0..N-1).
	return "page-" + string(rune('a'+i))
}
