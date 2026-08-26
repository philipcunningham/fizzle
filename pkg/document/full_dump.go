package document

import (
	"bytes"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	"github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

// ErrSplitNeedsEmptyDisk reports loose files that prevent a split instrument
// from occupying all usable sectors on disk 1.
type ErrSplitNeedsEmptyDisk struct{ Files int }

func (e *ErrSplitNeedsEmptyDisk) Error() string {
	return fmt.Sprintf("document: a two disk instrument needs an empty disk 1; found %d other files", e.Files)
}

// ReplaceFullDump atomically returns a state whose full dump is data. It
// collapses a pair when data fits one disk and creates or updates disk 2 when
// the dump must be split. The receiver is unchanged on every failure.
func (s State) ReplaceFullDump(data []byte, voiceCount int, authority Authority) (State, error) {
	img, err := disk.ReadImage(bytes.NewReader(s.image1))
	if err != nil {
		return State{}, fmt.Errorf("document: reading disk 1: %w", err)
	}
	if len(data) <= voicebuild.MaxDiskFileBytes {
		if err := putFullDump(img, data, 0, nil, voiceCount); err != nil {
			return State{}, err
		}
		return NewState(img.Bytes(), nil, authority)
	}

	var dump *fzf.Document
	if voiceCount > 0 {
		dump, err = fzf.NewDiskFile(data, voiceCount)
	} else {
		dump, err = fzf.NewStandalone(data)
	}
	if err != nil {
		return State{}, fmt.Errorf("document: resolving full dump: %w", err)
	}
	split, err := voicebuild.SplitDocument(dump)
	if err != nil {
		return State{}, err
	}
	return s.PlaceSplit(split, authority)
}

// PlaceSplit atomically stores an already split full dump on two images.
func (s State) PlaceSplit(split voicebuild.MultiDiskResult, authority Authority) (State, error) {
	if len(split.Disks) != 2 {
		return State{}, fmt.Errorf("document: split result has %d disks, want 2", len(split.Disks))
	}
	current, err := disk.ReadImage(bytes.NewReader(s.image1))
	if err != nil {
		return State{}, fmt.Errorf("document: reading disk 1: %w", err)
	}
	if count := looseFileCount(current); count > 0 {
		return State{}, &ErrSplitNeedsEmptyDisk{Files: count}
	}
	img1, err := formattedImage(current.Label())
	if err != nil {
		return State{}, err
	}
	if err := putFullDump(img1, split.Disks[0], 0, &split, 0); err != nil {
		return State{}, err
	}
	img2, err := s.secondImage(current.Label())
	if err != nil {
		return State{}, err
	}
	if err := putFullDump(img2, split.Disks[1], 1, &split, 0); err != nil {
		return State{}, err
	}
	return NewState(img1.Bytes(), img2.Bytes(), authority)
}

func putFullDump(img *disk.Image, data []byte, diskNumber uint8, split *voicebuild.MultiDiskResult, voiceCount int) error {
	var file diskfs.File
	var err error
	if split == nil {
		file, err = diskfs.FullDump(data, diskNumber, voiceCount)
	} else {
		file = diskfs.File{
			Name: disk.PadLabel(disk.FullDumpName), Type: disk.TypeFullDump,
			DiskNumber: diskNumber, Banks: split.BankCount, Voices: split.VoiceCount, Waves: split.WaveCount,
		}
	}
	if err != nil {
		return err
	}
	if hasFile(img, disk.FullDumpName) {
		return diskfs.Replace(img, disk.FullDumpName, data, file)
	}
	return diskfs.Add(img, data, file)
}

func (s State) secondImage(label string) (*disk.Image, error) {
	if s.image2 != nil {
		img, err := disk.ReadImage(bytes.NewReader(s.image2))
		if err != nil {
			return nil, fmt.Errorf("document: reading disk 2: %w", err)
		}
		return img, nil
	}
	if len(label)+2 > disk.LabelSize {
		label = label[:disk.LabelSize-2]
	}
	return formattedImage(label + " 2")
}

func formattedImage(label string) (*disk.Image, error) {
	data, err := diskformat.BuildImage(label)
	if err != nil {
		data, err = diskformat.BuildImage("FZ DISK")
		if err != nil {
			return nil, fmt.Errorf("document: formatting image: %w", err)
		}
	}
	img, err := disk.ReadImage(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("document: reading formatted image: %w", err)
	}
	return img, nil
}

func looseFileCount(img *disk.Image) int {
	entries, err := img.Directory()
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.NameString() != disk.FullDumpName {
			count++
		}
	}
	return count
}

func hasFile(img *disk.Image, name string) bool {
	entries, err := img.Directory()
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.NameString() == name {
			return true
		}
	}
	return false
}
