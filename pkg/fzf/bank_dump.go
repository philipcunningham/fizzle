package fzf

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// BankDumpLayout is the immutable resolved geometry of an FZB bank dump.
type BankDumpLayout struct {
	voiceCount    int
	voiceStart    int
	voiceAreaEnd  int
	countFromWalk bool
}

func (l BankDumpLayout) VoiceCount() int     { return l.voiceCount }
func (l BankDumpLayout) VoiceStart() int     { return l.voiceStart }
func (l BankDumpLayout) VoiceAreaEnd() int   { return l.voiceAreaEnd }
func (l BankDumpLayout) CountFromWalk() bool { return l.countFromWalk }

// BankDump owns an FZB's bytes and resolved layout.
type BankDump struct {
	data   []byte
	layout BankDumpLayout
}

// BankDumpVoice is the shared bank and voice metadata for one FZB slot.
type BankDumpVoice struct {
	Index        int
	Name         string
	PlaybackMode uint16
	RootKey      byte
	KeyLow       byte
	KeyHigh      byte
	VelocityLow  byte
	VelocityHigh byte
	MIDIChannel  int
	Output       string
	Volume       byte
}

// NewBankDump resolves a single-bank FZB by validating its bank sector and
// walking its bounded voice area. A plausible stored bstep is retained only
// when it agrees with the walk.
func NewBankDump(data []byte) (*BankDump, error) {
	if len(data) < disk.SectorSize {
		return nil, fmt.Errorf("fzf: bank dump is too small (%d bytes, need at least %d)", len(data), disk.SectorSize)
	}
	owned := bytes.Clone(data)
	stored := int(binary.LittleEndian.Uint16(owned[disk.BankVoiceCountOffset : disk.BankVoiceCountOffset+2]))
	upper := stored
	if upper < 1 || upper > disk.MaxVoices {
		upper = disk.MaxVoices
	}
	count := inferVoiceCount(owned, disk.SectorSize, upper)
	if count == 0 {
		return nil, fmt.Errorf("fzf: bank dump has no valid voice headers (bstep=%d)", stored)
	}
	end := disk.SectorSize + disk.VoiceAreaSectors(count)*disk.SectorSize
	if len(owned) < end {
		return nil, fmt.Errorf("fzf: bank dump is truncated (need %d bytes for voice headers, have %d)", end, len(owned))
	}
	return &BankDump{
		data: owned,
		layout: BankDumpLayout{
			voiceCount:    count,
			voiceStart:    disk.SectorSize,
			voiceAreaEnd:  end,
			countFromWalk: stored != count,
		},
	}, nil
}

func inferVoiceCount(data []byte, voiceStart, upper int) int {
	upper = min(upper, disk.MaxVoices)
	for slot := range upper {
		offset := disk.VoiceSlotOffset(voiceStart, slot)
		if offset+disk.VoiceHeaderUsed > len(data) ||
			!disk.IsActiveOrEmptyVoiceSlot(data[offset:offset+disk.VoiceHeaderUsed]) {
			return slot
		}
	}
	return upper
}

func (d *BankDump) Bytes() []byte          { return bytes.Clone(d.data) }
func (d *BankDump) Layout() BankDumpLayout { return d.layout }
func (d *BankDump) Bank() BankView {
	return BankView{data: d.data[:disk.SectorSize], voiceCount: d.layout.voiceCount}
}
func (d *BankDump) Voice(index int) (VoiceView, error) {
	if index < 0 || index >= d.layout.voiceCount {
		return VoiceView{}, &IndexError{Err: ErrVoiceIndexOutOfRange, Index: index, Limit: d.layout.voiceCount}
	}
	offset := disk.VoiceSlotOffset(d.layout.voiceStart, index)
	if offset+disk.VoiceHeaderUsed > d.layout.voiceAreaEnd {
		return VoiceView{}, fmt.Errorf("%w: slot %d", ErrVoiceHeaderBounds, index)
	}
	return VoiceView{data: d.data[offset : offset+disk.VoiceHeaderUsed], index: index, offset: offset}, nil
}

// MappedVoice returns one FZB slot's voice header and bank-area metadata.
// The boolean is false for the format's no-sound placeholder slots.
func (d *BankDump) MappedVoice(index int) (BankDumpVoice, bool, error) {
	voice, err := d.Voice(index)
	if err != nil {
		return BankDumpVoice{}, false, err
	}
	mode := voice.PlaybackMode()
	if mode == disk.PlaybackModeNoSound {
		return BankDumpVoice{Index: index + 1, PlaybackMode: mode}, false, nil
	}
	area, err := d.Bank().Area(index)
	if err != nil {
		return BankDumpVoice{}, false, err
	}
	name := voice.Name()
	if name == "" || !disk.IsPrintableName([]byte(name)) {
		name = fmt.Sprintf("VOICE %d", index+1)
	}
	return BankDumpVoice{
		Index:        index + 1,
		Name:         name,
		PlaybackMode: mode,
		RootKey:      area.RootKey(),
		KeyLow:       area.KeyLow(),
		KeyHigh:      area.KeyHigh(),
		VelocityLow:  area.VelocityLow(),
		VelocityHigh: area.VelocityHigh(),
		MIDIChannel:  area.MIDIChannel(),
		Output:       area.Output(),
		Volume:       area.Volume(),
	}, true, nil
}
