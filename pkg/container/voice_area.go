package container

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

var (
	// ErrVoiceLimit means the requested resize exceeds the format limit.
	ErrVoiceLimit = errors.New("container: voice limit reached")
	// ErrMinimumArea means the resize would leave no playable area.
	ErrMinimumArea = errors.New("container: instrument needs an area")
	// ErrSpareVoice means shrinking would discard an unreferenced named voice.
	ErrSpareVoice = errors.New("container: unreferenced named voice blocks resize")
	// ErrNoSpareVoice means every candidate slot remains referenced.
	ErrNoSpareVoice = errors.New("container: no voice slot can be removed")
	// ErrInvalidVoiceArea means the resolved geometry exceeds the buffer.
	ErrInvalidVoiceArea = errors.New("container: invalid voice area")
)

// VoiceAreaError carries the values an application boundary needs to explain
// why a voice-area resize was refused.
type VoiceAreaError struct {
	Err        error
	VoiceCount int
	Extra      int
	Slot       int
	Name       string
}

func (e *VoiceAreaError) Error() string {
	return fmt.Sprintf("%v: voices=%d extra=%d slot=%d name=%q", e.Err, e.VoiceCount, e.Extra, e.Slot, e.Name)
}

func (e *VoiceAreaError) Unwrap() error { return e.Err }

// VoiceAreaResizeParams describes the resolved dump geometry before a bank
// edit. BStepDelta is the change the bytes do not carry yet. FreedSlot is the
// slot released by a removed area, or -1.
type VoiceAreaResizeParams struct {
	BankCount, VoiceCount, VoiceStart, AudioStart int
	WalkBound, BStepDelta, FreedSlot              int
	DiskMode                                      bool
}

// VoiceAreaResize is the rebuilt dump and its resolved voice geometry.
type VoiceAreaResize struct {
	Data                   []byte
	VoiceCount, AudioStart int
}

// ResizeVoiceAreaOwned resizes a dump buffer whose ownership is transferred.
func ResizeVoiceAreaOwned(data []byte, p VoiceAreaResizeParams) (VoiceAreaResize, error) {
	return resizeVoiceAreaOwned(data, p, p.VoiceCount, false)
}

// ResizeVoiceAreaToOwned resizes a transferred buffer to an explicit voice
// count instead of deriving the count from bank areas.
func ResizeVoiceAreaToOwned(data []byte, p VoiceAreaResizeParams, target int) (VoiceAreaResize, error) {
	return resizeVoiceAreaOwned(data, p, target, true)
}

func resizeVoiceAreaOwned(data []byte, p VoiceAreaResizeParams, target int, explicit bool) (VoiceAreaResize, error) {
	if !explicit && !p.DiskMode {
		sum := bankAreaSum(data, p.BankCount) + p.BStepDelta
		switch {
		case sum < p.VoiceCount:
			target = sum
		case p.VoiceCount == p.WalkBound:
			if sum > disk.MaxVoices {
				return VoiceAreaResize{}, &VoiceAreaError{Err: ErrVoiceLimit, VoiceCount: p.VoiceCount, Extra: sum - disk.MaxVoices}
			}
			target = sum
		}
	}
	if target < 1 {
		return VoiceAreaResize{}, &VoiceAreaError{Err: ErrMinimumArea, VoiceCount: p.VoiceCount}
	}

	state := voiceAreaState{data: data, params: p, audioStart: p.AudioStart}
	switch {
	case target > p.VoiceCount:
		if err := state.grow(target); err != nil {
			return VoiceAreaResize{}, err
		}
	case target < p.VoiceCount:
		if err := state.shrink(target); err != nil {
			return VoiceAreaResize{}, err
		}
	}
	return VoiceAreaResize{Data: state.data, VoiceCount: target, AudioStart: state.audioStart}, nil
}

type voiceAreaState struct {
	data       []byte
	params     VoiceAreaResizeParams
	audioStart int
}

func (s *voiceAreaState) grow(target int) error {
	sectors := (s.audioStart - s.params.VoiceStart) / disk.SectorSize
	if grow := disk.VoiceAreaSectors(target) - sectors; grow > 0 {
		growBytes := grow * disk.SectorSize
		grown := make([]byte, len(s.data)+growBytes)
		copy(grown[:s.audioStart], s.data[:s.audioStart])
		copy(grown[s.audioStart+growBytes:], s.data[s.audioStart:])
		s.data = grown
		s.audioStart += growBytes
	}
	for slot := s.params.VoiceCount; slot < target; slot++ {
		off := disk.VoiceSlotOffset(s.params.VoiceStart, slot)
		if off+disk.VoicePackSize > s.audioStart {
			return &VoiceAreaError{Err: ErrInvalidVoiceArea, Slot: slot}
		}
		clear(s.data[off : off+disk.VoicePackSize])
	}
	return nil
}

func (s *voiceAreaState) shrink(target int) error {
	sectors := (s.audioStart - s.params.VoiceStart) / disk.SectorSize
	capacity := sectors * disk.VoicesPerSector
	freed := s.params.FreedSlot
	for count := s.params.VoiceCount; count > target; count-- {
		if !voiceSlotReferenced(s.data, s.params.BankCount, count-1) {
			continue
		}
		drop, err := s.spareSlot(freed, count-1)
		if err != nil {
			return err
		}
		s.compactSlot(drop, capacity)
		switch {
		case freed == drop:
			freed = -1
		case freed > drop:
			freed--
		}
	}
	// Bytes belonging to surviving slots, including their unknown 64 byte
	// tails, move verbatim. Capacity past target is different: a parser can
	// reinterpret a plausible stale header there as a live voice when a later
	// bank edit raises the walk bound. Clear only that retired capacity so the
	// resolved voice count remains stable; bank bytes, live slots, and audio
	// remain byte preserving.
	if tail := disk.VoiceSlotOffset(s.params.VoiceStart, target); tail < s.audioStart {
		clear(s.data[tail:s.audioStart])
	}
	if shrink := (sectors - disk.VoiceAreaSectors(target)) * disk.SectorSize; shrink > 0 {
		s.data = append(s.data[:s.audioStart-shrink], s.data[s.audioStart:]...)
		s.audioStart -= shrink
	}
	return nil
}

func (s *voiceAreaState) spareSlot(freed, limit int) (int, error) {
	if freed >= 0 && freed < limit && !voiceSlotReferenced(s.data, s.params.BankCount, freed) {
		return freed, nil
	}
	named := -1
	for slot := limit - 1; slot >= 0; slot-- {
		if voiceSlotReferenced(s.data, s.params.BankCount, slot) {
			continue
		}
		if voiceSlotIsPlaceholder(s.data, s.params.VoiceStart, slot) {
			return slot, nil
		}
		if named < 0 {
			named = slot
		}
	}
	if named >= 0 {
		return 0, &VoiceAreaError{Err: ErrSpareVoice, Slot: named, Name: voiceSlotName(s.data, s.params.VoiceStart, named)}
	}
	return 0, &VoiceAreaError{Err: ErrNoSpareVoice, VoiceCount: s.params.VoiceCount}
}

func (s *voiceAreaState) compactSlot(slot, count int) {
	end := s.params.VoiceStart + disk.VoiceAreaSectors(count)*disk.SectorSize
	if end > len(s.data) {
		end = len(s.data)
	}
	from := disk.VoiceSlotOffset(s.params.VoiceStart, slot)
	if from+disk.VoicePackSize > end {
		return
	}
	copy(s.data[from:], s.data[from+disk.VoicePackSize:end])
	clear(s.data[end-disk.VoicePackSize : end])
	for bank := 0; bank < s.params.BankCount; bank++ {
		base := bank * disk.SectorSize
		for area := 0; area < bankAreaCount(s.data, bank); area++ {
			off := base + disk.BankVoiceNumOffset + area*disk.VPEntrySize
			if off+disk.VPEntrySize > len(s.data) {
				break
			}
			if voice := int(binary.LittleEndian.Uint16(s.data[off:])); voice > slot {
				binary.LittleEndian.PutUint16(s.data[off:], uint16(voice-1)) //nolint:gosec // bounded slot index
			}
		}
	}
}

func bankAreaSum(data []byte, banks int) int {
	total := 0
	for bank := 0; bank < banks; bank++ {
		total += bankAreaCount(data, bank)
	}
	return total
}

func bankAreaCount(data []byte, bank int) int {
	off := bank*disk.SectorSize + disk.BankVoiceCountOffset
	if off+2 > len(data) {
		return 0
	}
	return int(binary.LittleEndian.Uint16(data[off : off+2]))
}

func voiceSlotReferenced(data []byte, banks, slot int) bool {
	for bank := 0; bank < banks; bank++ {
		base := bank * disk.SectorSize
		for area := 0; area < bankAreaCount(data, bank); area++ {
			off := base + disk.BankVoiceNumOffset + area*disk.VPEntrySize
			if off+2 <= len(data) && int(binary.LittleEndian.Uint16(data[off:off+2])) == slot {
				return true
			}
		}
	}
	return false
}

func voiceSlotIsPlaceholder(data []byte, voiceStart, slot int) bool {
	off := disk.VoiceSlotOffset(voiceStart, slot)
	if off+disk.VoiceHeaderUsed > len(data) {
		return false
	}
	header := data[off : off+disk.VoiceHeaderUsed]
	return binary.LittleEndian.Uint16(header[disk.VoiceLoopModeOffset:]) == disk.PlaybackModeNoSound &&
		binary.LittleEndian.Uint32(header[disk.VoiceWaveStartOffset:]) == binary.LittleEndian.Uint32(header[disk.VoiceWaveEndOffset:])
}

func voiceSlotName(data []byte, voiceStart, slot int) string {
	off := disk.VoiceSlotOffset(voiceStart, slot) + disk.VoiceNameOffset
	if off+disk.LabelSize > len(data) {
		return ""
	}
	return disk.TrimPadded(data[off : off+disk.LabelSize])
}
