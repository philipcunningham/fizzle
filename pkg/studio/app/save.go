package app

import (
	"fmt"
	"path/filepath"

	"github.com/rivo/tview"
)

// saveConfirmText is the prompt rendered inside the save-confirmation
// modal. Spec §5.1 word-for-word.
func (a *App) saveConfirmText() string {
	return fmt.Sprintf("Save changes to %s? [y/N]", filepath.Base(a.m.Path()))
}

// quitConfirmText is the prompt for the discard-changes-on-quit modal.
// Spec §5.3 / §3.4.
const quitConfirmText = "Discard unsaved changes? [y/N]"

// showSaveConfirm pushes the save-confirmation modal onto the stack.
// On "Save", calls m.Save() and reports success or failure via the
// status line. On Cancel / Escape, dismisses with no change.
func (a *App) showSaveConfirm() {
	modal := tview.NewModal()
	modal.SetText(a.saveConfirmText())
	modal.AddButtons([]string{"Save", "Cancel"})
	modal.SetDoneFunc(func(_ int, label string) {
		a.stack.Pop()
		if label == "Save" {
			a.performSave()
		}
	})
	a.stack.Push(pageSaveConfirm, modal)
}

// performSave runs m.Save and updates the status line with the result.
// Errors are also surfaced as a modal so the user can read the full
// failure message (the status line is one row tall and may truncate).
func (a *App) performSave() {
	if err := a.m.Save(); err != nil {
		a.showSaveError(err)
		a.status.SetError(fmt.Sprintf("Save failed: %v", err))
		return
	}
	_, totalKB := a.header.byteCounts()
	a.status.SetInfo(fmt.Sprintf("Saved %s (%d KB)", filepath.Base(a.m.Path()), totalKB))
}

// showSaveError pushes a modal explaining a save failure. Esc / OK
// dismisses.
func (a *App) showSaveError(err error) {
	modal := tview.NewModal()
	modal.SetText(fmt.Sprintf("Save failed:\n\n%v", err))
	modal.AddButtons([]string{"OK"})
	modal.SetDoneFunc(func(_ int, _ string) {
		a.stack.Pop()
	})
	a.stack.Push(pageSaveError, modal)
}

// showQuitConfirm pushes the discard-changes-on-quit modal. On Discard,
// stops the app; on Cancel, dismisses. If the model is not dirty, the
// caller should stop the app directly without prompting.
func (a *App) showQuitConfirm() {
	modal := tview.NewModal()
	modal.SetText(quitConfirmText)
	modal.AddButtons([]string{"Discard", "Cancel"})
	modal.SetDoneFunc(func(_ int, label string) {
		a.stack.Pop()
		if label == "Discard" {
			a.tApp.Stop()
		}
	})
	a.stack.Push(pageQuitConfirm, modal)
}

// quit either stops the app immediately (clean state) or prompts the
// user via the discard-changes modal.
func (a *App) quit() {
	if a.m.IsDirty() {
		a.showQuitConfirm()
		return
	}
	a.tApp.Stop()
}
