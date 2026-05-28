// Package modal implements a stack-based modal manager for fizzle studio.
//
// The Stack wraps a tview.Pages instance with push/pop semantics so the
// app shell can layer modals (save confirm, info overlay, help overlay,
// terminal-too-small) on top of the main layout without scattering
// AddPage / RemovePage calls.
//
// The pattern is borrowed from k9s (~/Code/k9s/internal/ui/pages.go).
// Pages that exist on the underlying tview.Pages before the first Push
// are not managed by Stack; only modals layered on top via Push/Pop.
//
// Stack is not safe for concurrent use. All mutations must run on the
// tview main goroutine.
package modal

import "github.com/rivo/tview"

// Stack is a push/pop wrapper around a tview.Pages. The stack carries the
// names of pages pushed via Push so Pop can remove them in reverse order.
// The zero value is not usable; construct via NewStack.
type Stack struct {
	pages *tview.Pages
	stack []string
}

// NewStack returns a Stack wrapping pages. Pages that already exist on
// pages at construction time are not managed by the Stack.
func NewStack(pages *tview.Pages) *Stack {
	return &Stack{pages: pages}
}

// Push adds p to the underlying tview.Pages under name and gives it
// focus by making it the visible page. Subsequent Push calls layer
// more modals on top; tview renders only the topmost page in the
// Pages primitive but all underlying pages still receive resize
// events.
//
// If name collides with an existing page (managed or not), the
// existing page is removed before the new one is added; Push always
// produces a visible new modal at the top of the stack.
func (s *Stack) Push(name string, p tview.Primitive) {
	if s.pages.HasPage(name) {
		s.pages.RemovePage(name)
	}
	s.pages.AddPage(name, p, true, true)
	s.stack = append(s.stack, name)
}

// Pop removes the topmost pushed page (if any) and returns focus to
// the previous topmost page (or the page underneath the stack if the
// stack is now empty). Pop on an empty stack is a no-op.
func (s *Stack) Pop() {
	n := len(s.stack)
	if n == 0 {
		return
	}
	name := s.stack[n-1]
	s.stack = s.stack[:n-1]
	s.pages.RemovePage(name)
}

// Top returns the name of the topmost pushed page, or "" if the stack
// is empty.
func (s *Stack) Top() string {
	n := len(s.stack)
	if n == 0 {
		return ""
	}
	return s.stack[n-1]
}

// Depth returns the number of pages currently on the stack.
func (s *Stack) Depth() int { return len(s.stack) }

// Has reports whether name is currently on the stack (i.e. was pushed
// via Push and not yet popped).
func (s *Stack) Has(name string) bool {
	for _, n := range s.stack {
		if n == name {
			return true
		}
	}
	return false
}
