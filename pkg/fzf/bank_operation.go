package fzf

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/container"
	"github.com/philipcunningham/fizzle/pkg/disk"
)

var instrumentOwnedBankRanges = [][2]int{
	{disk.BankTotalWaveOffset, 4},
	{disk.BankEffectOffset, disk.EffectDataSize},
}

// AddBank appends or replaces one bank mapping while retaining the document's
// voice slots, audio, and instrument-owned fields in bank sector zero.
func (d *Document) AddBank(fzb []byte, slot int) (OperationResult, error) {
	if len(fzb) < disk.SectorSize {
		return OperationResult{}, fmt.Errorf("fzf: bank data is shorter than one sector")
	}
	if slot < 0 || slot >= disk.MaxBanks || slot > d.layout.BankCount() {
		return OperationResult{}, &IndexError{Err: ErrBankIndexOutOfRange, Index: slot, Limit: min(d.layout.BankCount()+1, disk.MaxBanks)}
	}
	incoming := int(binary.LittleEndian.Uint16(fzb[disk.BankVoiceCountOffset:]))
	if incoming < 1 || incoming > disk.MaxVoices {
		return OperationResult{}, fmt.Errorf("fzf: bank area count %d outside 1..%d", incoming, disk.MaxVoices)
	}
	replaced := 0
	if slot < d.layout.BankCount() {
		bank, _ := d.Bank(slot)
		replaced = bank.AreaCount()
	}
	voiceLimit := d.layout.VoiceCount() + incoming - replaced
	if d.diskMode {
		voiceLimit = d.layout.VoiceCount()
	}
	for area := range incoming {
		offset := disk.BankVoiceNumOffset + area*disk.VPEntrySize
		voice := int(binary.LittleEndian.Uint16(fzb[offset : offset+disk.VPEntrySize]))
		if voice >= voiceLimit {
			return OperationResult{}, &AreaVoiceError{Area: area, Voice: voice, VoiceCount: voiceLimit}
		}
	}

	params := d.resizeParams(0, -1)
	working := bytes.Clone(d.data)
	bankCount := d.layout.BankCount()
	voiceStart := d.layout.VoiceStart()
	audioStart := d.layout.AudioStart()
	if slot == bankCount {
		var grown int
		working, grown = container.GrowBanks(working, bankCount, slot+1)
		bankCount++
		voiceStart += grown
		audioStart += grown
	}
	offset := slot * disk.SectorSize
	old := bytes.Clone(working[offset : offset+disk.SectorSize])
	sector := make([]byte, disk.SectorSize)
	copy(sector, fzb[:disk.SectorSize])
	if slot == 0 {
		keepInstrumentBankFields(sector, old)
	}
	copy(working[offset:offset+disk.SectorSize], sector)

	params.BankCount = bankCount
	params.VoiceStart = voiceStart
	params.AudioStart = audioStart
	resized, err := container.ResizeVoiceAreaOwned(working, params)
	if err != nil {
		return OperationResult{}, err
	}
	d.stampMarker(resized.Data, resized.VoiceCount)
	return structuralAreaOperation(d.data, resized.Data, resized.VoiceCount, resized.AudioStart), nil
}

func keepInstrumentBankFields(sector, replaced []byte) {
	for _, field := range instrumentOwnedBankRanges {
		offset, size := field[0], field[1]
		copy(sector[offset:offset+size], replaced[offset:offset+size])
	}
}
