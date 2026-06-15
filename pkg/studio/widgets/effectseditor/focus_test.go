package effectseditor

import (
	"strings"
	"testing"
)

// TestView_FocusedMatrixCellBracketed pins N-02: a focused modulation
// matrix cell is marked with brackets, so focus reads on shape and not
// colour alone. The bend cell (field 0) uses a caret already; tabbing
// once moves into the matrix.
func TestView_FocusedMatrixCellBracketed(t *testing.T) {
	m := New()
	var seed SeedValues
	m.Open(0, seed)
	m.HandleKey("tab") // move focus from the bend cell into the matrix

	view := m.View()
	if !strings.Contains(view, "[") || !strings.Contains(view, "]") {
		t.Errorf("focused matrix cell is not bracketed:\n%s", view)
	}
}
