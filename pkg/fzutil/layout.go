package fzutil

import "github.com/philipcunningham/fizzle/pkg/disk"

// FZFLayout is the resolved structural layout of an FZF dump.
// Its fields are private so parsing decisions cannot be changed after resolution.
type FZFLayout struct {
	bankCount        int
	voiceCount       int
	bstep0           int
	voiceStart       int
	audioStart       int
	voiceCountSource VoiceCountSource
}

// BankCount returns the number of bank sectors at the start of the dump.
func (l FZFLayout) BankCount() int { return l.bankCount }

// VoiceCount returns the resolved number of voice slots.
func (l FZFLayout) VoiceCount() int { return l.voiceCount }

// BStep0 returns bank zero's raw key-split count.
func (l FZFLayout) BStep0() int { return l.bstep0 }

// VoiceStart returns the byte offset of the voice area.
func (l FZFLayout) VoiceStart() int { return l.voiceStart }

// AudioStart returns the byte offset immediately after the voice area.
func (l FZFLayout) AudioStart() int { return l.audioStart }

// VoiceCountSource returns the authority used for VoiceCount.
func (l FZFLayout) VoiceCountSource() VoiceCountSource { return l.voiceCountSource }

// ResolveDiskFZFLayout resolves a disk-backed dump under the DIS tail's vn.
func ResolveDiskFZFLayout(data []byte, disVN int) (FZFLayout, error) {
	return resolveFZFLayout(data, disVN, VoiceCountDIS)
}

// ResolveStandaloneFZFLayout resolves a standalone dump under its marker record.
func ResolveStandaloneFZFLayout(data []byte) (FZFLayout, error) {
	return resolveFZFLayout(data, MarkerVoiceCount(data), VoiceCountMarker)
}

func resolveFZFLayout(data []byte, candidate int, source VoiceCountSource) (FZFLayout, error) {
	header, walkErr := ParseFZFHeader(data)
	resolvedSource := VoiceCountWalk
	if candidate > 0 && (walkErr != nil || header.NVoice < candidate) {
		if candidateHeader, err := ParseFZFHeaderWithVoiceCount(data, candidate); err == nil {
			header = candidateHeader
			resolvedSource = source
			walkErr = nil
		}
	}
	if walkErr != nil {
		return FZFLayout{}, walkErr
	}
	return FZFLayout{
		bankCount:        header.NBankSectors,
		voiceCount:       header.NVoice,
		bstep0:           header.BStep0,
		voiceStart:       header.VoiceAreaStart,
		audioStart:       header.VoiceAreaStart + disk.VoiceAreaSectors(header.NVoice)*disk.SectorSize,
		voiceCountSource: resolvedSource,
	}, nil
}

func (l FZFLayout) compatibilityHeader() *FZFHeader {
	return &FZFHeader{
		NVoice:         l.voiceCount,
		BStep0:         l.bstep0,
		NBankSectors:   l.bankCount,
		VoiceAreaStart: l.voiceStart,
	}
}
