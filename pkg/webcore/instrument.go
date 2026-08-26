package webcore

import (
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	fzfmodel "github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzfeffects"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/model"
)

// rebaseLoops converts a slot detail's absolute loop addresses to
// voice-relative frames, clamped to the voice's extent. Loose files
// start at zero so they never pass through here.
func rebaseLoops(detail *VoiceDetail, base uint32) {
	for i := range detail.Loops {
		l := &detail.Loops[i]
		l.Start = clampInt(l.Start-int(base), 0, detail.Frames)
		l.End = clampInt(l.End-int(base), 0, detail.Frames)
	}
}

// AreaSnapshot is one key split in a bank: the fields R12 makes
// editable. MidiChannel speaks the display scale (1 to 16) like the
// CLI; Output carries the raw gchn bitmask plus its rendered label.
type AreaSnapshot struct {
	VoiceSlot   int    `json:"voiceSlot"`
	VoiceName   string `json:"voiceName"`
	KeyLow      int    `json:"keyLow"`
	KeyHigh     int    `json:"keyHigh"`
	Root        int    `json:"root"`
	VelLow      int    `json:"velLow"`
	VelHigh     int    `json:"velHigh"`
	MidiChannel int    `json:"midiChannel"`
	Output      int    `json:"output"`
	OutputLabel string `json:"outputLabel"`
	Volume      int    `json:"volume"`
}

// BankSnapshot is one bank sector: its name and its areas in vp order.
type BankSnapshot struct {
	Name  string         `json:"name"`
	Areas []AreaSnapshot `json:"areas"`
}

// InstrumentVoice is one audible voice slot with its R13 flag, plus
// the editable surface the voice editor renders: schema params and
// the bespoke detail (loops, envelopes), parsed from the slot's
// header bytes. Loop positions arrive voice-relative; the dump's
// absolute addresses rebase here, mirroring the slot setters.
type InstrumentVoice struct {
	Slot       int            `json:"slot"`
	Name       string         `json:"name"`
	Referenced bool           `json:"referenced"`
	Params     map[string]any `json:"params,omitempty"`
	Voice      *VoiceDetail   `json:"voice,omitempty"`
	// SharesAudio marks a slot whose audio is an earlier slot's: the
	// velocity switch clones a voice header rather than the samples,
	// so it costs no disk space. The UI says so instead of charging
	// the user twice for one sound.
	SharesAudio bool `json:"sharesAudio,omitempty"`
	// AudioKey identifies the samples this slot plays: where they
	// start and how many there are. It changes only when the audio
	// changes, so a caller can cache decoded PCM and peaks against it
	// instead of re-decoding on every parameter edit.
	AudioKey string `json:"audioKey,omitempty"`
}

// EffectsSnapshot is the instrument's global effect block (R19):
// pitch bend range plus the 3 by 7 controller modulation matrix.
// Rows are mod wheel, foot pedal, aftertouch; columns are LFO pitch,
// LFO amp, LFO filter, LFO resonance, DCA offset, DCF offset, and DCQ
// offset, in R19's order.
type EffectsSnapshot struct {
	BendRange int     `json:"bendRange"`
	Matrix    [][]int `json:"matrix"`
}

// InstrumentSnapshot is the disk's full dump as the UI edits it:
// banks of areas plus the voice list with unreferenced voices flagged.
type InstrumentSnapshot struct {
	FileName string            `json:"fileName"`
	Banks    []BankSnapshot    `json:"banks"`
	Voices   []InstrumentVoice `json:"voices"`
	Effects  *EffectsSnapshot  `json:"effects,omitempty"`
}

// instrumentFrom parses a full dump's banks and voice list. It uses
// the same per-area reader as fzfinfo and fzbinfo, so the Web UI and
// the CLI agree on every field. disVN is the DIS-mode count, 0 walks.
func instrumentFrom(fileName string, fzfData []byte, disVN int) (*InstrumentSnapshot, error) {
	var doc *fzfmodel.Document
	var err error
	if disVN > 0 {
		doc, err = fzfmodel.NewDiskFile(fzfData, disVN)
	} else {
		doc, err = fzfmodel.NewStandalone(fzfData)
	}
	if err != nil {
		return nil, fmt.Errorf("webcore: %w", err)
	}
	layout := doc.Layout()

	referenced := make(map[int]bool)
	banks := make([]BankSnapshot, 0, layout.BankCount())
	for b := range layout.BankCount() {
		bank, berr := doc.Bank(b)
		if berr != nil {
			return nil, fmt.Errorf("webcore: %w", berr)
		}
		snapshot := BankSnapshot{
			Name:  bank.Name(),
			Areas: make([]AreaSnapshot, 0, bank.AreaCount()),
		}
		for i := range bank.AreaCount() {
			areaView, aerr := bank.Area(i)
			if aerr != nil {
				return nil, fmt.Errorf("webcore: %w", aerr)
			}
			slot := areaView.VoiceSlot()
			referenced[slot] = true
			voiceName := fmt.Sprintf("VOICE %d", slot+1)
			if voice, verr := doc.Voice(slot); verr == nil {
				name := voice.Name()
				if name != "" && disk.IsPrintableName([]byte(name)) {
					voiceName = name
				}
			}
			area := AreaSnapshot{
				VoiceSlot:   slot,
				VoiceName:   voiceName,
				KeyLow:      int(areaView.KeyLow()),
				KeyHigh:     int(areaView.KeyHigh()),
				Root:        int(areaView.RootKey()),
				VelLow:      int(areaView.VelocityLow()),
				VelHigh:     int(areaView.VelocityHigh()),
				MidiChannel: areaView.MIDIChannel(),
				Output:      areaView.OutputValue(),
				OutputLabel: areaView.Output(),
				Volume:      disk.AreaLevelFromByte(areaView.Volume()),
			}
			snapshot.Areas = append(snapshot.Areas, area)
		}
		banks = append(banks, snapshot)
	}

	// The first slot to claim a wave start owns that audio; later slots
	// pointing at the same address are clones sharing it.
	audioOwners := make(map[uint32]int, layout.VoiceCount())
	voices := make([]InstrumentVoice, 0, layout.VoiceCount())
	for slot := range layout.VoiceCount() {
		voiceView, verr := doc.Voice(slot)
		if verr != nil {
			return nil, fmt.Errorf("webcore: %w", verr)
		}
		mode := voiceView.PlaybackMode()
		if mode == disk.PlaybackModeNoSound {
			continue
		}
		name := voiceView.Name()
		if name == "" || !disk.IsPrintableName([]byte(name)) {
			name = fmt.Sprintf("VOICE %d", slot+1)
		}
		voice := InstrumentVoice{Slot: slot, Name: name, Referenced: referenced[slot]}
		waveStart := voiceView.WaveStart()
		if seenAt, ok := audioOwners[waveStart]; ok && seenAt != slot {
			voice.SharesAudio = true
		} else {
			audioOwners[waveStart] = slot
		}
		waveEnd := voiceView.WaveEnd()
		voice.AudioKey = fmt.Sprintf("%d:%d", waveStart, waveEnd)
		// Header-only enrichment: pad the slot header to the sector the
		// parser expects; no audio is copied. A slot that fails to parse
		// simply carries no editable surface.
		padded := make([]byte, disk.SectorSize)
		copy(padded, voiceView.HeaderBytes())
		if vp, err := fzvinfo.ParseBytes(padded); err == nil {
			voice.Params = voiceParams(vp, padded)
			detail := voiceDetailFrom(vp)
			rebaseLoops(detail, waveStart)
			voice.Voice = detail
		}
		voices = append(voices, voice)
	}

	inst := &InstrumentSnapshot{FileName: fileName, Banks: banks, Voices: voices}
	if params, err := fzfeffects.ParseBytes(fzfData); err == nil {
		inst.Effects = &EffectsSnapshot{
			BendRange: params.BendRange,
			Matrix: [][]int{
				{params.ModLFP, params.ModLFA, params.ModLFF, params.ModLFQ, params.ModDCA, params.ModDCF, params.ModDCQ},
				{params.FotLFP, params.FotLFA, params.FotLFF, params.FotLFQ, params.FotDCA, params.FotDCF, params.FotDCQ},
				{params.AftLFP, params.AftLFA, params.AftLFF, params.AftLFQ, params.AftDCA, params.AftDCF, params.AftDCQ},
			},
		}
	}
	return inst, nil
}

// SetEffectCell writes one modulation matrix cell (R19): controller 0
// to 2 (mod wheel, foot pedal, aftertouch) to target 0 to 6 in R19's
// destination order, clamped to 0..127.
func (s *Session) SetEffectCell(controller, target, value int) (Snapshot, *Error) {
	if controller < 0 || controller > 2 {
		return s.Snapshot(), errf(codeInvalidValue, "controller must be 0 to 2, got %d", controller)
	}
	if target < 0 || target > 6 {
		return s.Snapshot(), errf(codeInvalidValue, "target must be 0 to 6, got %d", target)
	}
	value = clampInt(value, 0, 127)
	return s.setEffects(func(p *fzfeffects.SetParams) {
		cells := [3][7]*int{
			{&p.ModLFP, &p.ModLFA, &p.ModLFF, &p.ModLFQ, &p.ModDCA, &p.ModDCF, &p.ModDCQ},
			{&p.FotLFP, &p.FotLFA, &p.FotLFF, &p.FotLFQ, &p.FotDCA, &p.FotDCF, &p.FotDCQ},
			{&p.AftLFP, &p.AftLFA, &p.AftLFF, &p.AftLFQ, &p.AftDCA, &p.AftDCF, &p.AftDCQ},
		}
		*cells[controller][target] = value
	})
}

// SetBendRange writes the pitch bend range, clamped to the block's
// 0..127 byte.
func (s *Session) SetBendRange(value int) (Snapshot, *Error) {
	value = clampInt(value, 0, disk.MaxBendRange)
	return s.setEffects(func(p *fzfeffects.SetParams) {
		p.BendRange = value
	})
}

// setEffects applies one effect-block write through the CLI's own
// setter on the disk's full dump.
func (s *Session) setEffects(fill func(*fzfeffects.SetParams)) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		params := fzfeffects.Unchanged()
		fill(&params)
		if _, err := fzfeffects.SetBytes(d.fzf, params); err != nil {
			return nil, errf(codeInvalidValue, "%v", err)
		}
		return nil, nil
	})
}
