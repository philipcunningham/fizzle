package model

import (
	"bytes"
	"testing"
)

func TestApply(t *testing.T) {
	data := []byte{1, 2, 3, 4}
	if err := Apply(data, []Patch{
		{Offset: 0, Old: []byte{1}, New: []byte{8}},
		{Offset: 2, Old: []byte{3, 4}, New: []byte{6, 7}},
	}); err != nil {
		t.Fatal(err)
	}
	if want := []byte{8, 2, 6, 7}; !bytes.Equal(data, want) {
		t.Fatalf("got %v, want %v", data, want)
	}
}

func TestApplyRejectsInvalidBatchWithoutChangingData(t *testing.T) {
	tests := []struct {
		name    string
		patches []Patch
	}{
		{"negative offset", []Patch{{Offset: -1, Old: []byte{1}, New: []byte{8}}}},
		{"offset beyond buffer", []Patch{{Offset: 5, Old: nil, New: nil}}},
		{"old range beyond buffer", []Patch{{Offset: 3, Old: []byte{4, 5}, New: []byte{8, 9}}}},
		{"length changing", []Patch{{Offset: 1, Old: []byte{2}, New: []byte{8, 9}}}},
		{"stale first patch", []Patch{{Offset: 0, Old: []byte{9}, New: []byte{8}}}},
		{"stale later patch", []Patch{
			{Offset: 0, Old: []byte{1}, New: []byte{8}},
			{Offset: 2, Old: []byte{9}, New: []byte{8}},
		}},
		{"overlapping patches", []Patch{
			{Offset: 0, Old: []byte{1, 2}, New: []byte{8, 9}},
			{Offset: 1, Old: []byte{2, 3}, New: []byte{7, 6}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte{1, 2, 3, 4}
			before := bytes.Clone(data)
			if err := Apply(data, tt.patches); err == nil {
				t.Fatal("Apply succeeded; want error")
			}
			if !bytes.Equal(data, before) {
				t.Fatalf("Apply changed data on error: got %v, want %v", data, before)
			}
		})
	}
}

func TestApplyAllowsEmptyPatchWithinBuffer(t *testing.T) {
	data := []byte{1, 2, 3, 4}
	if err := Apply(data, []Patch{{Offset: len(data), Old: nil, New: nil}}); err != nil {
		t.Fatal(err)
	}
}
