package document

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
)

// PairError reports images that are not the two halves of one instrument.
type PairError struct{ Reason string }

func (e *PairError) Error() string { return "document: " + e.Reason }

// OpenPair validates, orders, and owns a split instrument's two disk images.
func OpenPair(a, b []byte) (State, error) {
	for i, data := range [][]byte{a, b} {
		if len(data) != disk.ImageSize {
			return State{}, &PairError{Reason: fmt.Sprintf("image %d is %d bytes, want %d", i+1, len(data), disk.ImageSize)}
		}
	}
	imgA, err := disk.ReadImage(bytes.NewReader(a))
	if err != nil {
		return State{}, &PairError{Reason: fmt.Sprintf("image 1 is unreadable: %v", err)}
	}
	imgB, err := disk.ReadImage(bytes.NewReader(b))
	if err != nil {
		return State{}, &PairError{Reason: fmt.Sprintf("image 2 is unreadable: %v", err)}
	}
	numA, okA := fullDumpDiskNumber(imgA)
	numB, okB := fullDumpDiskNumber(imgB)
	if !okA || !okB {
		return State{}, &PairError{Reason: fmt.Sprintf("both images must carry a %s entry", disk.FullDumpName)}
	}
	first, second := imgA, imgB
	switch {
	case numA == 1 && numB == 0:
		first, second = imgB, imgA
	case numA == 0 && numB == 1:
	default:
		return State{}, &PairError{Reason: fmt.Sprintf("images are disks %d and %d; a pair is disks 1 and 2", numA+1, numB+1)}
	}
	one, err := diskfs.Extract(first, disk.FullDumpName)
	if err != nil {
		return State{}, &PairError{Reason: err.Error()}
	}
	if !needsContinuation(one) {
		return State{}, &PairError{Reason: "disk 1 contains a complete instrument and needs no continuation"}
	}
	two, err := diskfs.Extract(second, disk.FullDumpName)
	if err != nil {
		return State{}, &PairError{Reason: err.Error()}
	}
	stitched := append(bytes.Clone(one), two...)
	layout, err := fzutil.ResolveStandaloneFZFLayout(stitched)
	if err != nil {
		return State{}, &PairError{Reason: fmt.Sprintf("stitched dump does not parse: %v", err)}
	}
	if err := validateContinuation(stitched, layout); err != nil {
		return State{}, err
	}
	return NewState(first.Bytes(), second.Bytes(), AuthorityDIS)
}

// StitchedFullDump returns the full dump with disk 2's continuation appended.
func (s State) StitchedFullDump() ([]byte, error) {
	if !s.IsOpen() {
		return nil, fmt.Errorf("document: no disk is open")
	}
	first, err := disk.ReadImage(bytes.NewReader(s.image1))
	if err != nil {
		return nil, fmt.Errorf("document: reading disk 1: %w", err)
	}
	data, err := diskfs.Extract(first, disk.FullDumpName)
	if err != nil {
		return nil, err
	}
	if s.image2 == nil {
		return data, nil
	}
	second, err := disk.ReadImage(bytes.NewReader(s.image2))
	if err != nil {
		return nil, fmt.Errorf("document: reading disk 2: %w", err)
	}
	continuation, err := diskfs.Extract(second, disk.FullDumpName)
	if err != nil {
		return nil, err
	}
	return append(data, continuation...), nil
}

// MissingDisk reports the absent half of a lone split image, or zero.
func (s State) MissingDisk() int {
	if !s.IsOpen() || s.HasSecondDisk() {
		return 0
	}
	img, err := disk.ReadImage(bytes.NewReader(s.image1))
	if err != nil {
		return 0
	}
	number, ok := fullDumpDiskNumber(img)
	if !ok {
		return 0
	}
	if number == 1 {
		return 1
	}
	data, err := diskfs.Extract(img, disk.FullDumpName)
	if err == nil && needsContinuation(data) {
		return 2
	}
	return 0
}

func fullDumpDiskNumber(img *disk.Image) (int, bool) {
	entries, err := img.Directory()
	if err != nil {
		return 0, false
	}
	for _, entry := range entries {
		if entry.NameString() == disk.FullDumpName {
			return int(entry.DiskNum), true
		}
	}
	return 0, false
}

func needsContinuation(data []byte) bool {
	layout, err := fzutil.ResolveStandaloneFZFLayout(data)
	if err != nil {
		return false
	}
	localAudio := len(data) - layout.AudioStart()
	if localAudio < 0 {
		return false
	}
	for slot := range layout.VoiceCount() {
		offset := disk.VoiceSlotOffset(layout.VoiceStart(), slot)
		if offset+disk.VoiceHeaderUsed > len(data) {
			break
		}
		voice := data[offset : offset+disk.VoiceHeaderUsed]
		if !disk.IsPlausibleVoiceSlot(voice) {
			continue
		}
		end := int(binary.LittleEndian.Uint32(voice[disk.VoiceWaveEndOffset:])) * disk.BytesPerSample
		if end > localAudio {
			return true
		}
	}
	return false
}

func validateContinuation(data []byte, layout fzutil.FZFLayout) error {
	audioBytes := len(data) - layout.AudioStart()
	if audioBytes < 0 {
		return &PairError{Reason: "stitched dump is shorter than its resolved layout"}
	}
	maxEnd := 0
	for slot := range layout.VoiceCount() {
		offset := disk.VoiceSlotOffset(layout.VoiceStart(), slot)
		if offset+disk.VoiceHeaderUsed > len(data) {
			break
		}
		end := int(binary.LittleEndian.Uint32(data[offset+disk.VoiceWaveEndOffset:]))
		maxEnd = max(maxEnd, end)
	}
	if need := maxEnd * disk.BytesPerSample; audioBytes < need {
		return &PairError{Reason: fmt.Sprintf("stitched audio holds %d bytes; voices require %d", audioBytes, need)}
	}
	total := int(binary.LittleEndian.Uint32(data[disk.BankTotalWaveOffset : disk.BankTotalWaveOffset+4]))
	if total > 0 {
		got := audioBytes / disk.SectorSize
		if got < total || got > total+1 {
			return &PairError{Reason: fmt.Sprintf("stitched audio has %d sectors; instrument declares %d", got, total)}
		}
	}
	return nil
}
