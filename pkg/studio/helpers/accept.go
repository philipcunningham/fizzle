package helpers

import (
	"strconv"
	"strings"
)

// AcceptUnsigned returns a tview.InputField acceptance function (see
// SetAcceptanceFunc) that accepts only integer text in the range 0..hi.
// Empty text is always accepted so the user can clear the field; any
// other text must parse as a non-negative integer not exceeding hi.
func AcceptUnsigned(hi int) func(text string, ch rune) bool {
	return func(text string, _ rune) bool {
		if text == "" {
			return true
		}
		v, err := strconv.Atoi(text)
		if err != nil {
			return false
		}
		return v >= 0 && v <= hi
	}
}

// AcceptSigned returns an acceptance function for signed integer fields in
// the range lo..hi. Empty text, "-", and "+" are accepted as partial input
// so the user can start typing a negative number or clear the field; any
// other text must parse and be in range.
//
// Mirrors pkg/studio/editform.go::acceptSigned.
func AcceptSigned(lo, hi int) func(text string, ch rune) bool {
	return func(text string, _ rune) bool {
		if text == "" || text == "-" || text == "+" {
			return true
		}
		v, err := strconv.Atoi(text)
		if err != nil {
			return false
		}
		return v >= lo && v <= hi
	}
}

// AcceptNote returns an acceptance function for MIDI-note input. The user
// may type either a raw MIDI integer (0..127) or a note name (C4, c4, C 4,
// F#3, Bb2). Partial input is accepted so the user can build the value one
// keystroke at a time: empty string, a single letter, a letter + accidental,
// a letter + accidental + signed octave prefix, etc.
//
// Any string that fully resolves via ParseNote is also accepted, so a final
// commit step can rely on ParseNote alone.
func AcceptNote() func(text string, ch rune) bool {
	return func(text string, _ rune) bool {
		if text == "" {
			return true
		}
		// Numeric / partially-numeric input. Allow a leading run of digits
		// up to 3 chars (MIDI 0..127 is at most 3 digits); the final value
		// is bound-checked when complete.
		if _, err := strconv.Atoi(text); err == nil {
			n, _ := strconv.Atoi(text)
			return n >= 0 && n <= 127
		}
		// Partial digit prefix on the way to a full integer (e.g. user
		// types "1" then "2" then "8" -> reject at "128"). strconv.Atoi
		// covers complete integers above; we don't need a separate
		// partial-digit branch.

		// Note-name input. Try the full parser first; that covers any
		// commit-ready string and lets the accept-function double as a
		// final-value gate.
		if _, err := ParseNote(text); err == nil {
			return true
		}

		// Partial note input. Accept strings that look like a prefix of a
		// valid note name so the user can keep typing.
		t := strings.ReplaceAll(text, " ", "")
		if t == "" {
			return true
		}
		letter := strings.ToUpper(t[:1])
		if letter < "A" || letter > "G" {
			return false
		}
		// One letter alone: always a valid prefix.
		if len(t) == 1 {
			return true
		}
		// Optional accidental.
		rest := t[1:]
		if rest[0] == '#' || rest[0] == 'b' || rest[0] == 'B' {
			rest = rest[1:]
			if len(rest) == 0 {
				return true
			}
		}
		// Remainder must be an integer prefix for the octave. Allow a
		// leading '-' for negative octaves like C-1. We avoid accepting
		// strings whose final parse would push MIDI out of range; that
		// check is handled by ParseNote on the commit-ready value above.
		if rest == "-" || rest == "+" {
			return true
		}
		if _, err := strconv.Atoi(rest); err != nil {
			return false
		}
		// Numeric-suffix accepted as a prefix; final MIDI bound is enforced
		// by ParseNote (which the branch above already tried).
		return false
	}
}

// AcceptName returns an acceptance function for free-text name fields
// (e.g. voice name, bank name). Any character is accepted; the keystroke is
// only refused once the field has reached maxLen characters.
func AcceptName(maxLen int) func(text string, ch rune) bool {
	return func(text string, _ rune) bool {
		return len(text) <= maxLen
	}
}
