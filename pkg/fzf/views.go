package fzf

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/model"
)

// BankView is a bounded, zero-copy view of one bank sector in a Document.
type BankView struct {
	data       []byte
	index      int
	offset     int
	voiceCount int
}

// Bank returns the bank at index.
func (d *Document) Bank(index int) (BankView, error) {
	if index < 0 || index >= d.layout.BankCount() {
		return BankView{}, fmt.Errorf("fzf: bank index %d outside 0..%d", index, d.layout.BankCount()-1)
	}
	offset := index * disk.SectorSize
	return BankView{
		data:       d.data[offset : offset+disk.SectorSize],
		index:      index,
		offset:     offset,
		voiceCount: d.layout.VoiceCount(),
	}, nil
}

// Index returns the zero-based bank index.
func (v BankView) Index() int { return v.index }

// Name returns the bank's trimmed display name.
func (v BankView) Name() string {
	return disk.TrimPadded(v.data[disk.BankNameOffset : disk.BankNameOffset+disk.LabelSize])
}

// AreaCount returns the bank's key-area count.
func (v BankView) AreaCount() int {
	return int(binary.LittleEndian.Uint16(v.data[disk.BankVoiceCountOffset : disk.BankVoiceCountOffset+2]))
}

// VoiceSlot returns the file-level voice slot played by area.
func (v BankView) VoiceSlot(area int) (int, error) {
	if area < 0 || area >= v.AreaCount() {
		return 0, fmt.Errorf("fzf: area index %d outside 0..%d", area, v.AreaCount()-1)
	}
	offset := disk.BankVoiceNumOffset + area*disk.VPEntrySize
	slot := int(binary.LittleEndian.Uint16(v.data[offset : offset+disk.VPEntrySize]))
	if slot >= v.voiceCount {
		return 0, fmt.Errorf("fzf: area %d points to voice slot %d outside 0..%d", area, slot, v.voiceCount-1)
	}
	return slot, nil
}

// NamePatch returns a stale-safe patch that changes the bank's display name.
func (v BankView) NamePatch(name string) (model.Patch, error) {
	return labelPatch(v.data, v.offset, disk.BankNameOffset, name)
}

// VoiceView is a bounded, zero-copy view of one voice header in a Document.
type VoiceView struct {
	data   []byte
	index  int
	offset int
}

// Voice returns the voice slot at index.
func (d *Document) Voice(index int) (VoiceView, error) {
	if index < 0 || index >= d.layout.VoiceCount() {
		return VoiceView{}, fmt.Errorf("fzf: voice index %d outside 0..%d", index, d.layout.VoiceCount()-1)
	}
	offset := disk.VoiceSlotOffset(d.layout.VoiceStart(), index)
	return VoiceView{data: d.data[offset : offset+disk.VoiceHeaderUsed], index: index, offset: offset}, nil
}

// Index returns the zero-based file-level voice index.
func (v VoiceView) Index() int { return v.index }

// Name returns the voice's trimmed display name.
func (v VoiceView) Name() string {
	return disk.TrimPadded(v.data[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize])
}

// PlaybackMode returns the voice's raw playback-mode value.
func (v VoiceView) PlaybackMode() uint16 {
	return binary.LittleEndian.Uint16(v.data[disk.VoiceLoopModeOffset : disk.VoiceLoopModeOffset+2])
}

// WaveStart returns the voice's absolute start address in samples.
func (v VoiceView) WaveStart() uint32 {
	return binary.LittleEndian.Uint32(v.data[disk.VoiceWaveStartOffset : disk.VoiceWaveStartOffset+4])
}

// WaveEnd returns the voice's absolute end address in samples.
func (v VoiceView) WaveEnd() uint32 {
	return binary.LittleEndian.Uint32(v.data[disk.VoiceWaveEndOffset : disk.VoiceWaveEndOffset+4])
}

// NamePatch returns a stale-safe patch that changes the voice's display name.
func (v VoiceView) NamePatch(name string) (model.Patch, error) {
	return labelPatch(v.data, v.offset, disk.VoiceNameOffset, name)
}

// DISView is a bounded, zero-copy view of one data-information sector.
type DISView struct {
	data []byte
}

// NewDISView validates sector and returns a view over its first sector.
func NewDISView(sector []byte) (DISView, error) {
	if _, err := disk.DecodeDisSector(sector); err != nil {
		return DISView{}, err
	}
	return DISView{data: sector[:disk.SectorSize]}, nil
}

// BankCount returns the DIS tail's bank count.
func (v DISView) BankCount() int {
	return int(binary.LittleEndian.Uint16(v.data[disk.DisTailOffset:disk.DisVoiceCountOffset]))
}

// VoiceCount returns the DIS tail's voice count.
func (v DISView) VoiceCount() int {
	return int(binary.LittleEndian.Uint16(v.data[disk.DisVoiceCountOffset:disk.DisWaveCountOffset]))
}

// WaveCount returns the DIS tail's wave-sector count.
func (v DISView) WaveCount() int {
	return int(binary.LittleEndian.Uint16(v.data[disk.DisWaveCountOffset : disk.DisWaveCountOffset+2]))
}

// VoiceCountPatch returns a stale-safe patch for the DIS voice count.
func (v DISView) VoiceCountPatch(count int) (model.Patch, error) {
	if count < 0 || count > disk.MaxVoices {
		return model.Patch{}, fmt.Errorf("fzf: DIS voice count %d outside 0..%d", count, disk.MaxVoices)
	}
	old := bytes.Clone(v.data[disk.DisVoiceCountOffset:disk.DisWaveCountOffset])
	updated := make([]byte, 2)
	binary.LittleEndian.PutUint16(updated, uint16(count)) //nolint:gosec // count is bounded above
	return model.Patch{Offset: disk.DisVoiceCountOffset, Old: old, New: updated}, nil
}

func labelPatch(data []byte, base, relative int, name string) (model.Patch, error) {
	if len(name) > disk.LabelSize || !disk.IsPrintableName([]byte(name)) {
		return model.Patch{}, fmt.Errorf("fzf: name must be at most %d printable ASCII characters", disk.LabelSize)
	}
	label := disk.PadLabel(name)
	old := bytes.Clone(data[relative : relative+disk.LabelSize])
	return model.Patch{Offset: base + relative, Old: old, New: label[:]}, nil
}
