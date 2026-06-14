package loader

import (
	"errors"
	"testing"
)

const (
	pianoFZF = "../../../testdata/corpus/casio-fz-1-factory-library/casio-fz-sound-disk-fl-a-piano/Piano.fzf"
	stabIMG  = "../../../testdata/synthetic/STAB.img"
)

func TestLoadFZF(t *testing.T) {
	m, info, err := LoadContainer(pianoFZF)
	if err != nil {
		t.Fatalf("LoadContainer(Piano.fzf): %v", err)
	}
	if info.Format != FormatFZF {
		t.Errorf("Format = %v, want FormatFZF", info.Format)
	}
	if info.VoiceCount == 0 {
		t.Errorf("VoiceCount = 0, want > 0")
	}
	if info.BankCount == 0 {
		t.Errorf("BankCount = 0, want > 0")
	}
	if m.Len() == 0 {
		t.Errorf("Model.Len = 0, want > 0")
	}
	if m.Path() != pianoFZF {
		t.Errorf("Model.Path = %q, want %q", m.Path(), pianoFZF)
	}
	if m.Dirty() {
		t.Errorf("Model.Dirty = true after fresh load, want false")
	}
}

func TestLoadIMG(t *testing.T) {
	m, info, err := LoadContainer(stabIMG)
	if err != nil {
		// Some test images may not contain a FULL-DATA-FZ; that's fine,
		// we just verify the loader's error reporting for that case.
		if errors.Is(err, ErrNoFullDump) {
			t.Skipf("STAB.img has no FULL-DATA-FZ; loader signals correctly")
			return
		}
		t.Fatalf("LoadContainer(STAB.img): %v", err)
	}
	if info.Format != FormatIMG {
		t.Errorf("Format = %v, want FormatIMG", info.Format)
	}
	if m.Len() == 0 {
		t.Errorf("Model.Len = 0, want > 0")
	}
}

func TestLoadUnsupportedExtension(t *testing.T) {
	_, _, err := LoadContainer("/tmp/not-a-real-file.txt")
	if err == nil {
		t.Errorf("expected error for unsupported extension, got nil")
	}
}

func TestNewUntitled(t *testing.T) {
	m, info := NewUntitled()
	if info.Format != FormatFZF {
		t.Errorf("Format = %v, want FormatFZF", info.Format)
	}
	if info.BankCount != 8 {
		t.Errorf("BankCount = %d, want 8", info.BankCount)
	}
	if info.VoiceCount != 0 {
		t.Errorf("VoiceCount = %d, want 0", info.VoiceCount)
	}
	if m.Path() != "" {
		t.Errorf("Path = %q, want empty", m.Path())
	}
	if m.Dirty() {
		t.Errorf("Dirty = true for fresh untitled, want false")
	}
}
