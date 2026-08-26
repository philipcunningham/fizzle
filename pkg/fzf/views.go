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

// TotalWaveSectors returns the optional whole-instrument wave-sector marker.
// Only bank zero carries this marker; callers interpreting split dumps should
// read it from Document.Bank(0).
func (v BankView) TotalWaveSectors() int {
	return int(binary.LittleEndian.Uint32(v.data[disk.BankTotalWaveOffset : disk.BankTotalWaveOffset+4]))
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

// Area returns a bounded view of one bank key area.
func (v BankView) Area(index int) (AreaView, error) {
	if index < 0 || index >= v.AreaCount() {
		return AreaView{}, &IndexError{Err: ErrAreaIndexOutOfRange, Index: index, Limit: v.AreaCount()}
	}
	voice, err := v.VoiceSlot(index)
	if err != nil {
		return AreaView{}, err
	}
	return AreaView{bank: v.data, index: index, voiceSlot: voice}, nil
}

// ShowsVelocity reports whether any area carries a non-default velocity
// range, including the unreachable zero-to-zero range.
func (v BankView) ShowsVelocity() bool {
	for area := range v.AreaCount() {
		low := v.data[disk.BankVelLowOffset+area]
		high := v.data[disk.BankVelHighOffset+area]
		if low != disk.DefaultVelLow || high != disk.DefaultVelHigh {
			return true
		}
	}
	return false
}

// ShowsVolume reports whether any area carries a non-default bank volume.
func (v BankView) ShowsVolume() bool {
	for area := range v.AreaCount() {
		if v.data[disk.BankVolumeOffset+area] != disk.DefaultBankVolume {
			return true
		}
	}
	return false
}

// AreaView is a bounded, zero-copy view of one bank key area.
type AreaView struct {
	bank      []byte
	index     int
	voiceSlot int
}

func (v AreaView) Index() int     { return v.index }
func (v AreaView) VoiceSlot() int { return v.voiceSlot }
func (v AreaView) KeyLow() byte   { return min(v.bank[disk.BankKeyLowOffset+v.index], disk.MaxMIDINote) }
func (v AreaView) KeyHigh() byte {
	return min(v.bank[disk.BankKeyHighOffset+v.index], disk.MaxMIDINote)
}
func (v AreaView) RootKey() byte {
	return min(v.bank[disk.BankKeyCentOffset+v.index], disk.MaxMIDINote)
}
func (v AreaView) VelocityLow() byte  { return v.bank[disk.BankVelLowOffset+v.index] }
func (v AreaView) VelocityHigh() byte { return v.bank[disk.BankVelHighOffset+v.index] }
func (v AreaView) MIDIChannel() int   { return int(v.bank[disk.BankMIDIRecvChanOffset+v.index]) + 1 }
func (v AreaView) Output() string {
	return disk.FormatAudioOut(v.bank[disk.BankAudioOutOffset+v.index])
}

// OutputValue returns the raw output bitmask stored for the area.
func (v AreaView) OutputValue() int {
	return int(v.bank[disk.BankAudioOutOffset+v.index])
}
func (v AreaView) Volume() byte { return v.bank[disk.BankVolumeOffset+v.index] }

// NamePatch returns a stale-safe patch that changes the bank's display name.
// Applying it invalidates a standalone voice-count marker; re-stamp the marker
// after applying the complete patch batch.
func (v BankView) NamePatch(name string) (model.Patch, error) {
	return namePatch(v.data, v.offset, disk.BankNameOffset, disk.LabelSize, name)
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
		return VoiceView{}, &IndexError{Err: ErrVoiceIndexOutOfRange, Index: index, Limit: d.layout.VoiceCount()}
	}
	offset := disk.VoiceSlotOffset(d.layout.VoiceStart(), index)
	if offset < 0 || offset+disk.VoiceHeaderUsed > len(d.data) {
		return VoiceView{}, fmt.Errorf("%w: slot %d", ErrVoiceHeaderBounds, index)
	}
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

// SampleRateIndex returns the stored sample-rate table index.
func (v VoiceView) SampleRateIndex() byte { return v.data[disk.VoiceSampOffset] }

// HasActiveLoop reports whether the selected sustain loop has a non-empty
// address range after masking the format's flag bits.
func (v VoiceView) HasActiveLoop() bool {
	selected := v.data[disk.VoiceLoopSusOffset]
	if selected >= disk.NoSustainLoop {
		return false
	}
	startOffset := disk.VoiceLoopSt0Offset + int(selected)*4
	endOffset := disk.VoiceLoopEd0Offset + int(selected)*4
	start := binary.LittleEndian.Uint32(v.data[startOffset : startOffset+4])
	end := binary.LittleEndian.Uint32(v.data[endOffset : endOffset+4])
	return disk.LoopStartAddress(start) < disk.LoopEndAddress(end)
}

// RootKey returns the voice's raw key-center byte.
func (v VoiceView) RootKey() byte { return v.data[disk.VoiceKeyCentOffset] }

// HeaderBytes returns an owned copy of the voice's used header bytes.
func (v VoiceView) HeaderBytes() []byte { return bytes.Clone(v.data) }

// NamePatch returns a stale-safe patch that changes the voice's display name.
// Applying it invalidates a standalone voice-count marker; re-stamp the marker
// after applying the complete patch batch.
func (v VoiceView) NamePatch(name string) (model.Patch, error) {
	return namePatch(v.data, v.offset, disk.VoiceNameOffset, disk.VoiceNameFieldSize, name)
}

// DISView is a bounded, zero-copy view of one data-information sector.
type DISView struct {
	data   []byte
	offset int
}

// NewDISView validates sector and returns a view over its first sector.
// offset is the sector's absolute byte offset in the buffer that will receive
// patches produced by the view.
func NewDISView(sector []byte, offset int) (DISView, error) {
	if offset < 0 {
		return DISView{}, fmt.Errorf("fzf: DIS offset %d must not be negative", offset)
	}
	if _, err := disk.DecodeDisSector(sector); err != nil {
		return DISView{}, err
	}
	return DISView{data: sector[:disk.SectorSize], offset: offset}, nil
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
	return model.Patch{Offset: v.offset + disk.DisVoiceCountOffset, Old: old, New: updated}, nil
}

func namePatch(data []byte, base, relative, width int, name string) (model.Patch, error) {
	if len(name) > disk.LabelSize || !disk.IsPrintableName([]byte(name)) {
		return model.Patch{}, fmt.Errorf("fzf: name must be at most %d printable ASCII characters", disk.LabelSize)
	}
	label := disk.PadLabel(name)
	old := bytes.Clone(data[relative : relative+width])
	updated := make([]byte, width)
	copy(updated, label[:])
	return model.Patch{Offset: base + relative, Old: old, New: updated}, nil
}
