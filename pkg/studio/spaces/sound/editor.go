package sound

import (
	"encoding/binary"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// fieldKind classifies an editable field's input behaviour.
type fieldKind int

const (
	fieldUnsigned fieldKind = iota // 0..max
	fieldSigned                    // -max..max (display); two's-complement byte storage
	fieldEnum                      // index into options list
	fieldText                      // freeform string (FZ name set)
)

// field is one editable value on a Sound cell. The voice []byte
// argument passed to read / patch is the full container's bytes;
// every field closure has voiceOff baked in (the byte offset where
// this voice's 192-byte header sits in the container).
type field struct {
	label string
	kind  fieldKind

	min, max int      // for numeric kinds
	options  []string // for enum kind

	read      func(data []byte) int
	readText  func(data []byte) string
	patch     func(data []byte, value int) []model.Patch
	patchText func(data []byte, value string) []model.Patch

	noteName bool // numeric fields holding a MIDI note: show "49 (C#3)"
}

// cellFields returns the editable fields for the cell at (r, col)
// in a voice located at voiceOff in the container bytes.
func cellFields(r row, col, voiceOff int) []field {
	switch r {
	case rowDCA:
		return dcaCellFields(col, voiceOff)
	case rowDCF:
		return dcfCellFields(col, voiceOff)
	case rowLFO:
		return lfoCellFields(col, voiceOff)
	case rowSample:
		return sampleCellFields(col, voiceOff)
	case rowLoops:
		return loopsCellFields(col, voiceOff)
	case numRows:
		// numRows is a count sentinel, not a real row.
		return nil
	}
	return nil
}

// ---- field constructors ------------------------------------------------

// noteByteAt is unsignedByteAt for a field that holds a MIDI note number,
// so the display appends the note name (e.g. "49 (C#3)"), matching the
// Area editor's Key Orig.
//
//nolint:unparam // lo is kept symmetric with uint16At for callsite uniformity, even though all current callers pass 0
func noteByteAt(label string, voiceOff, fieldOff, lo, hi int) field {
	f := unsignedByteAt(label, voiceOff, fieldOff, lo, hi)
	f.noteName = true
	return f
}

// unsignedByteAt builds an unsigned 0..max byte field at the given
// (voiceOff + fieldOff) byte location.
func unsignedByteAt(label string, voiceOff, fieldOff, lo, hi int) field {
	abs := voiceOff + fieldOff
	return field{
		label: label,
		kind:  fieldUnsigned,
		min:   lo, max: hi,
		read: func(data []byte) int {
			return int(data[abs])
		},
		patch: func(data []byte, v int) []model.Patch {
			v = clampInt(v, lo, hi)
			b := uint8(v) //nolint:gosec // G115: v clamped to lo..hi (caller passes byte-bounded hi)
			existing := data[abs]
			if b == existing {
				return nil
			}
			return []model.Patch{{
				Offset: abs,
				Old:    []byte{existing},
				New:    []byte{b},
			}}
		},
	}
}

// signedByteAt builds a signed -127..127 (two's-complement byte)
// field at the given byte location.
func signedByteAt(label string, voiceOff, fieldOff int) field {
	abs := voiceOff + fieldOff
	return field{
		label: label,
		kind:  fieldSigned,
		min:   -127, max: 127,
		read: func(data []byte) int {
			return int(int8(data[abs])) //nolint:gosec // G115: byte to int8 reinterpret for two's-complement display
		},
		patch: func(data []byte, v int) []model.Patch {
			v = clampInt(v, -127, 127)
			b := uint8(int8(v)) //nolint:gosec // G115: v clamped to -127..127 above
			existing := data[abs]
			if b == existing {
				return nil
			}
			return []model.Patch{{
				Offset: abs,
				Old:    []byte{existing},
				New:    []byte{b},
			}}
		},
	}
}

// kfByteAt builds a keyboard / velocity follow field at the given
// byte location. The underlying byte stores a signed value (-127..127)
// but the FZ-1 LCD shows display = byte / 8 (-15..+15), so we expose
// the same compressed range here. Studio1 follows the same convention
// (see disk.MaxKFDisplay).
func kfByteAt(label string, voiceOff, fieldOff int) field {
	abs := voiceOff + fieldOff
	return field{
		label: label,
		kind:  fieldSigned,
		min:   disk.MinKFDisplay, max: disk.MaxKFDisplay,
		read: func(data []byte) int {
			return disk.KFByteToDisplay(data[abs])
		},
		patch: func(data []byte, v int) []model.Patch {
			v = clampInt(v, disk.MinKFDisplay, disk.MaxKFDisplay)
			b := disk.KFDisplayToByte(v)
			existing := data[abs]
			if b == existing {
				return nil
			}
			return []model.Patch{{
				Offset: abs,
				Old:    []byte{existing},
				New:    []byte{b},
			}}
		},
	}
}

// uint16At builds a little-endian uint16 field at the given location.
func uint16At(label string, voiceOff, fieldOff, lo, hi int) field {
	abs := voiceOff + fieldOff
	return field{
		label: label,
		kind:  fieldUnsigned,
		min:   lo, max: hi,
		read: func(data []byte) int {
			return int(binary.LittleEndian.Uint16(data[abs:]))
		},
		patch: func(data []byte, v int) []model.Patch {
			v = clampInt(v, lo, hi)
			var newBuf [2]byte
			binary.LittleEndian.PutUint16(newBuf[:], uint16(v)) //nolint:gosec // G115: v clamped to lo..hi (caller passes uint16-bounded hi)
			old := make([]byte, 2)
			copy(old, data[abs:abs+2])
			if newBuf[0] == old[0] && newBuf[1] == old[1] {
				return nil
			}
			return []model.Patch{{
				Offset: abs,
				Old:    old,
				New:    newBuf[:],
			}}
		},
	}
}

// enumByteAt builds an enum field backed by a single byte at the
// given (voiceOff + fieldOff) byte location. The byte value indexes
// directly into options.
func enumByteAt(label string, voiceOff, fieldOff int, options []string) field {
	abs := voiceOff + fieldOff
	hi := len(options) - 1
	return field{
		label:   label,
		kind:    fieldEnum,
		min:     0,
		max:     hi,
		options: append([]string(nil), options...),
		read: func(data []byte) int {
			v := int(data[abs])
			if v < 0 || v > hi {
				return 0
			}
			return v
		},
		patch: func(data []byte, v int) []model.Patch {
			v = clampInt(v, 0, hi)
			b := uint8(v) //nolint:gosec // G115: v clamped to 0..hi (hi = len(options)-1, always byte-bounded)
			existing := data[abs]
			if b == existing {
				return nil
			}
			return []model.Patch{{
				Offset: abs,
				Old:    []byte{existing},
				New:    []byte{b},
			}}
		},
	}
}

// waveStartAt edits wavst with a dynamic upper bound that reads
// waved at patch time. Without this clamp, a max-out value would
// produce wavst > waved which IsPlausibleVoiceSlot rejects, making
// the saved voice unloadable on real FZ-1 firmware.
func waveStartAt(voiceOff int) field {
	stAbs := voiceOff + disk.VoiceWaveStartOffset
	edAbs := voiceOff + disk.VoiceWaveEndOffset
	return field{
		label: "Wave start",
		kind:  fieldUnsigned,
		min:   0, max: 0x7FFFFFFF,
		read: func(data []byte) int {
			return int(binary.LittleEndian.Uint32(data[stAbs:]))
		},
		patch: func(data []byte, v int) []model.Patch {
			waved := int(binary.LittleEndian.Uint32(data[edAbs:]))
			if v < 0 {
				v = 0
			}
			if v > waved {
				v = waved
			}
			var newBuf [4]byte
			binary.LittleEndian.PutUint32(newBuf[:], uint32(v))
			old := make([]byte, 4)
			copy(old, data[stAbs:stAbs+4])
			if newBuf == [4]byte(old) {
				return nil
			}
			return []model.Patch{{Offset: stAbs, Old: old, New: newBuf[:]}}
		},
	}
}

// waveEndAt edits waved with a dynamic lower bound that reads
// wavst at patch time. Symmetric to waveStartAt; both preserve
// wavst <= waved.
func waveEndAt(voiceOff int) field {
	stAbs := voiceOff + disk.VoiceWaveStartOffset
	edAbs := voiceOff + disk.VoiceWaveEndOffset
	return field{
		label: "Wave end",
		kind:  fieldUnsigned,
		min:   0, max: 0x7FFFFFFF,
		read: func(data []byte) int {
			return int(binary.LittleEndian.Uint32(data[edAbs:]))
		},
		patch: func(data []byte, v int) []model.Patch {
			wavst := int(binary.LittleEndian.Uint32(data[stAbs:]))
			if v < wavst {
				v = wavst
			}
			if v > 0x7FFFFFFF {
				v = 0x7FFFFFFF
			}
			var newBuf [4]byte
			binary.LittleEndian.PutUint32(newBuf[:], uint32(v))
			old := make([]byte, 4)
			copy(old, data[edAbs:edAbs+4])
			if newBuf == [4]byte(old) {
				return nil
			}
			return []model.Patch{{Offset: edAbs, Old: old, New: newBuf[:]}}
		},
	}
}

// loopStartAddrAt edits the address portion (low 24 bits) of a
// 32-bit loopst[N] cell, preserving the fine byte (upper 8 bits).
// Without the mask, editing "start" would clobber the loop fine byte.
func loopStartAddrAt(label string, voiceOff, fieldOff int) field {
	abs := voiceOff + fieldOff
	hi := int(disk.LoopStartAddressMask)
	return field{
		label: label,
		kind:  fieldUnsigned,
		min:   0, max: hi,
		read: func(data []byte) int {
			return int(disk.LoopStartAddress(binary.LittleEndian.Uint32(data[abs:])))
		},
		patch: func(data []byte, v int) []model.Patch {
			lo, hiB := loopAddrBounds(data, voiceOff, hi)
			v = clampInt(v, lo, hiB)
			cur := binary.LittleEndian.Uint32(data[abs:])
			combined := (cur &^ disk.LoopStartAddressMask) | (uint32(v) & disk.LoopStartAddressMask) //nolint:gosec // G115: v clamped to 0..hi (hi = LoopStartAddressMask, uint32-bounded)
			if combined == cur {
				return nil
			}
			var newBuf [4]byte
			binary.LittleEndian.PutUint32(newBuf[:], combined)
			old := make([]byte, 4)
			copy(old, data[abs:abs+4])
			return []model.Patch{{Offset: abs, Old: old, New: newBuf[:]}}
		},
	}
}

// loopFineAt edits the fine byte (upper 8 bits) of a 32-bit
// loopst[N] cell, preserving the address bits.
func loopFineAt(label string, voiceOff, fieldOff int) field {
	abs := voiceOff + fieldOff
	return field{
		label: label,
		kind:  fieldUnsigned,
		min:   0, max: 255,
		read: func(data []byte) int {
			return int(disk.LoopFineBits(binary.LittleEndian.Uint32(data[abs:])))
		},
		patch: func(data []byte, v int) []model.Patch {
			v = clampInt(v, 0, 255)
			cur := binary.LittleEndian.Uint32(data[abs:])
			combined := (cur & disk.LoopStartAddressMask) | (uint32(v) << disk.LoopStartFineShift) //nolint:gosec // G115: v clamped to 0..255 above
			if combined == cur {
				return nil
			}
			var newBuf [4]byte
			binary.LittleEndian.PutUint32(newBuf[:], combined)
			old := make([]byte, 4)
			copy(old, data[abs:abs+4])
			return []model.Patch{{Offset: abs, Old: old, New: newBuf[:]}}
		},
	}
}

// loopEndAddrAt edits the address portion (low 31 bits) of a
// 32-bit looped[N] cell, preserving the skip flag (bit 31). Without
// the mask, editing "end" would clobber the skip flag.
func loopEndAddrAt(label string, voiceOff, fieldOff int) field {
	abs := voiceOff + fieldOff
	hi := int(disk.LoopEndAddressMask)
	return field{
		label: label,
		kind:  fieldUnsigned,
		min:   0, max: hi,
		read: func(data []byte) int {
			return int(disk.LoopEndAddress(binary.LittleEndian.Uint32(data[abs:])))
		},
		patch: func(data []byte, v int) []model.Patch {
			lo, hiB := loopAddrBounds(data, voiceOff, hi)
			v = clampInt(v, lo, hiB)
			cur := binary.LittleEndian.Uint32(data[abs:])
			combined := (cur & disk.LoopEndSkipMask) | (uint32(v) & disk.LoopEndAddressMask) //nolint:gosec // G115: v clamped to 0..hi (hi = LoopEndAddressMask, uint32-bounded)
			if combined == cur {
				return nil
			}
			var newBuf [4]byte
			binary.LittleEndian.PutUint32(newBuf[:], combined)
			old := make([]byte, 4)
			copy(old, data[abs:abs+4])
			return []model.Patch{{Offset: abs, Old: old, New: newBuf[:]}}
		},
	}
}

// loopNextAt exposes the per-loop skip flag (bit 31 of looped[N])
// as a Trace/Skip enum, matching studio1's "Next" dropdown.
func loopNextAt(label string, voiceOff, fieldOff int) field {
	abs := voiceOff + fieldOff
	options := []string{"Trace", "Skip"}
	return field{
		label: label,
		kind:  fieldEnum,
		min:   0, max: 1,
		options: options,
		read: func(data []byte) int {
			if disk.LoopSkipFlag(binary.LittleEndian.Uint32(data[abs:])) {
				return 1
			}
			return 0
		},
		patch: func(data []byte, v int) []model.Patch {
			cur := binary.LittleEndian.Uint32(data[abs:])
			combined := cur &^ disk.LoopEndSkipMask
			if v != 0 {
				combined |= disk.LoopEndSkipMask
			}
			if combined == cur {
				return nil
			}
			var newBuf [4]byte
			binary.LittleEndian.PutUint32(newBuf[:], combined)
			old := make([]byte, 4)
			copy(old, data[abs:abs+4])
			return []model.Patch{{Offset: abs, Old: old, New: newBuf[:]}}
		},
	}
}

// ---- DCA / DCF stage fields -------------------------------------------

// dcaStageFields returns Role, Rate (0..99 display), Stop Level
// (0..99 display) for the given DCA stage.
func dcaStageFields(voiceOff, stage int) []field {
	return envelopeStageFields(voiceOff, stage,
		disk.VoiceDCASusOffset, disk.VoiceDCAEndOffset,
		disk.VoiceDCARateOffset, disk.VoiceDCAStopOffset)
}

// dcfStageFields mirrors dcaStageFields for the DCF envelope.
func dcfStageFields(voiceOff, stage int) []field {
	return envelopeStageFields(voiceOff, stage,
		disk.VoiceDCFSusOffset, disk.VoiceDCFEndOffset,
		disk.VoiceDCFRateOffset, disk.VoiceDCFStopOffset)
}

// envelopeStageFields is the shared shape of a DCA / DCF stage cell.
// susFO, endFO are field offsets within the voice header for the
// Sus and End pointers. rateBaseFO, stopBaseFO are the bases of the
// 8-byte rate and stop arrays. stage is 0..7.
func envelopeStageFields(voiceOff, stage, susFO, endFO, rateBaseFO, stopBaseFO int) []field {
	susAbs := voiceOff + susFO
	endAbs := voiceOff + endFO
	rateAbs := voiceOff + rateBaseFO + stage
	stopAbs := voiceOff + stopBaseFO + stage

	return []field{
		{
			label: "Role",
			kind:  fieldEnum,
			// Match the Casio FZ-1 LCD: stages with no special role
			// render as "***" (the firmware's placeholder), and the
			// sus/end positions show "SUS" / "END".
			options: []string{"***", "SUS", "END"},
			read: func(data []byte) int {
				if data[susAbs] == byte(stage) { //nolint:gosec // G115: stage is 0..7 (envelope stage index)
					return 1
				}
				if data[endAbs] == byte(stage) { //nolint:gosec // G115: stage is 0..7 (envelope stage index)
					return 2
				}
				return 0
			},
			patch: func(data []byte, v int) []model.Patch {
				patches := []model.Patch{}
				switch v {
				case 0:
					// "Normal" is asymmetric: the FZ-1 envelope always
					// has a SUS and an END (valid stage indices 0..7).
					// There is no "no SUS" sentinel in the spec; writing
					// 0xFF here would put the voice past EnvelopeStages
					// and IsPlausibleVoiceSlot would reject it, corrupting
					// the saved file. Picking Normal on a stage that is
					// currently SUS or END is therefore a no-op: the user
					// changes SUS/END by picking SUS/END on a DIFFERENT
					// stage (which moves the pointer there).
				case 1: // SUS
					stageByte := byte(stage) //nolint:gosec // G115: stage is 0..7 (envelope stage index)
					if data[susAbs] != stageByte {
						patches = append(patches, model.Patch{
							Offset: susAbs,
							Old:    []byte{data[susAbs]},
							New:    []byte{stageByte},
						})
					}
				case 2: // END: auto-zero the stop level.
					stageByte := byte(stage) //nolint:gosec // G115: stage is 0..7 (envelope stage index)
					if data[endAbs] != stageByte {
						patches = append(patches, model.Patch{
							Offset: endAbs,
							Old:    []byte{data[endAbs]},
							New:    []byte{stageByte},
						})
					}
					if data[stopAbs] != 0 {
						patches = append(patches, model.Patch{
							Offset: stopAbs,
							Old:    []byte{data[stopAbs]},
							New:    []byte{0},
						})
					}
				}
				return patches
			},
		},
		{
			label: "Rate",
			kind:  fieldUnsigned,
			min:   0, max: 99,
			read: func(data []byte) int {
				return disk.RateByteToDisplay(data[rateAbs])
			},
			patch: func(data []byte, v int) []model.Patch {
				existing := data[rateAbs]
				// Preserve the direction sign bit; replace the magnitude.
				newByte := (existing & disk.RateSignBit) | disk.RateDisplayToByte(clampInt(v, 0, 99))
				if newByte == existing {
					return nil
				}
				return []model.Patch{{
					Offset: rateAbs,
					Old:    []byte{existing},
					New:    []byte{newByte},
				}}
			},
		},
		{
			label: "Stop level",
			kind:  fieldUnsigned,
			min:   0, max: 99,
			read: func(data []byte) int {
				return disk.StopByteToDisplay(data[stopAbs])
			},
			patch: func(data []byte, v int) []model.Patch {
				b := disk.StopDisplayToByte(clampInt(v, 0, 99))
				existing := data[stopAbs]
				if b == existing {
					return nil
				}
				return []model.Patch{{
					Offset: stopAbs,
					Old:    []byte{existing},
					New:    []byte{b},
				}}
			},
		},
	}
}

// ---- DCA row -----------------------------------------------------------

func dcaCellFields(col, voiceOff int) []field {
	switch col {
	case 0:
		return nil
	case 1:
		return []field{
			kfByteAt("DCA level KF", voiceOff, disk.VoiceDCAKFOffset),
			signedByteAt("DCA level VF", voiceOff, disk.VoiceVelDCAKFOffset),
		}
	case 2:
		return []field{
			kfByteAt("DCA rate KF", voiceOff, disk.VoiceDCARSOffset),
			signedByteAt("DCA rate VF", voiceOff, disk.VoiceVelDCARSOffset),
		}
	default:
		return dcaStageFields(voiceOff, col-3)
	}
}

// ---- DCF row -----------------------------------------------------------

func dcfCellFields(col, voiceOff int) []field {
	switch col {
	case 0:
		return nil
	case 1:
		return []field{
			unsignedByteAt("Cutoff", voiceOff, disk.VoiceDCFOffset, 0, 127),
			unsignedByteAt("Resonance", voiceOff, disk.VoiceDCQOffset, 0, disk.MaxResonance),
			signedByteAt("Vel res", voiceOff, disk.VoiceVelDCQKFOffset),
		}
	case 2:
		return []field{
			kfByteAt("DCF level KF", voiceOff, disk.VoiceDCFKFOffset),
			signedByteAt("DCF level VF", voiceOff, disk.VoiceVelDCFKFOffset),
		}
	case 3:
		return []field{
			kfByteAt("DCF rate KF", voiceOff, disk.VoiceDCFRSOffset),
			signedByteAt("DCF rate VF", voiceOff, disk.VoiceVelDCFRSOffset),
		}
	default:
		return dcfStageFields(voiceOff, col-4)
	}
}

// ---- LFO row -----------------------------------------------------------

func lfoCellFields(col, voiceOff int) []field {
	switch col {
	case 0:
		return nil
	case 1:
		abs := voiceOff + disk.VoiceLFONameOffset
		return []field{
			{
				label:   "Waveform",
				kind:    fieldEnum,
				options: []string{"sine", "saw up", "saw down", "triangle", "rectangle", "random"},
				read: func(data []byte) int {
					return int(data[abs] & disk.LFOWaveformMask)
				},
				patch: func(data []byte, v int) []model.Patch {
					if v < 0 || v > 5 {
						v = 0
					}
					existing := data[abs]
					newByte := (existing &^ disk.LFOWaveformMask) | uint8(v)
					if newByte == existing {
						return nil
					}
					return []model.Patch{{
						Offset: abs,
						Old:    []byte{existing},
						New:    []byte{newByte},
					}}
				},
			},
			unsignedByteAt("Rate", voiceOff, disk.VoiceLFORateOffset, 0, 127),
			unsignedByteAt("Attack", voiceOff, disk.VoiceLFOAtckOffset, 0, 127),
			uint16At("Delay", voiceOff, disk.VoiceLFODelayOffset, 0, 65535),
		}
	case 2:
		return []field{
			unsignedByteAt("Depth pitch", voiceOff, disk.VoiceLFODCPOffset, 0, 127),
			unsignedByteAt("Depth amp", voiceOff, disk.VoiceLFODCAOffset, 0, 127),
			unsignedByteAt("Depth filter", voiceOff, disk.VoiceLFODCFOffset, 0, 127),
			unsignedByteAt("Depth Q", voiceOff, disk.VoiceLFODCQOffset, 0, 127),
		}
	}
	return nil
}

// ---- Sample row --------------------------------------------------------

func sampleCellFields(col, voiceOff int) []field {
	// Name has its own gesture in Layout (`r` / F2); removed from the
	// Sample row to keep this column-set tight. Columns 1..5 hold the
	// remaining voice-header metadata.
	switch col {
	case 0:
		return nil
	case 1:
		abs := voiceOff + disk.VoiceSampOffset
		return []field{
			{
				label:   "Sample rate",
				kind:    fieldEnum,
				options: []string{"36 kHz", "18 kHz", "9 kHz"},
				read: func(data []byte) int {
					b := data[abs]
					if int(b) > 2 {
						return 0
					}
					return int(b)
				},
				patch: func(data []byte, v int) []model.Patch {
					if v < 0 || v > 2 {
						v = 0
					}
					existing := data[abs]
					b := uint8(v)
					if b == existing {
						return nil
					}
					return []model.Patch{{
						Offset: abs,
						Old:    []byte{existing},
						New:    []byte{b},
					}}
				},
			},
		}
	case 2:
		// Wave start / end are the bounds of the sample data in the
		// audio area. IsPlausibleVoiceSlot enforces wavst <= waved, so
		// edits to either must respect the other's current value.
		// Without this, a "max-out" value for wavst (or "zero-out"
		// value for waved) would invert the relationship and corrupt
		// the voice. Labels honestly say Wave (not Gen) since the
		// offsets target VoiceWaveStartOffset/VoiceWaveEndOffset.
		return []field{
			waveStartAt(voiceOff),
			waveEndAt(voiceOff),
		}
	case 3:
		return []field{
			noteByteAt("Root note", voiceOff, disk.VoiceKeyCentOffset, 0, 127),
		}
	case 4:
		// Playback mode (tune cell removed: its storage location isn't pinned).
		abs := voiceOff + disk.VoiceLoopModeOffset
		return []field{
			{
				label:   "Playback mode",
				kind:    fieldEnum,
				options: []string{"no sound", "normal", "reverse", "cue", "synthesized"},
				read: func(data []byte) int {
					m := binary.LittleEndian.Uint16(data[abs:])
					switch m {
					case disk.PlaybackModeNoSound:
						return 0
					case disk.PlaybackModeNormal, disk.PlaybackModeNormalVariant:
						return 1
					case disk.PlaybackModeReverse:
						return 2
					case disk.PlaybackModeCue:
						return 3
					case disk.PlaybackModeSynthesized:
						return 4
					}
					return 1
				},
				patch: func(data []byte, v int) []model.Patch {
					var target uint16
					switch v {
					case 0:
						target = disk.PlaybackModeNoSound
					case 1:
						target = disk.PlaybackModeNormal
					case 2:
						target = disk.PlaybackModeReverse
					case 3:
						target = disk.PlaybackModeCue
					case 4:
						target = disk.PlaybackModeSynthesized
					default:
						target = disk.PlaybackModeNormal
					}
					var newBuf [2]byte
					binary.LittleEndian.PutUint16(newBuf[:], target)
					oldBuf := make([]byte, 2)
					copy(oldBuf, data[abs:abs+2])
					if newBuf[0] == oldBuf[0] && newBuf[1] == oldBuf[1] {
						return nil
					}
					return []model.Patch{{
						Offset: abs,
						Old:    oldBuf,
						New:    newBuf[:],
					}}
				},
			},
		}
	}
	return nil
}

// ---- Loops row ---------------------------------------------------------
//
// Studio2 cell layout: col 0 = visual, col 1 = sus/release pointers
// (voice-level), col 2..9 = the 8 per-loop cells.

// loopPtrOptions: studio1 convention. Indices 0..7 select loop 0..7;
// index 8 is the sentinel ("none" for sustain, "all" for release).
var loopSusOptions = []string{"0", "1", "2", "3", "4", "5", "6", "7", "none"}
var loopEndOptions = []string{"0", "1", "2", "3", "4", "5", "6", "7", "all"}

func loopsCellFields(col, voiceOff int) []field {
	switch col {
	case 0:
		return nil
	case 1:
		// Voice-level Sustain/Release pointers.
		return []field{
			enumByteAt("Sustain loop", voiceOff, disk.VoiceLoopSusOffset, loopSusOptions),
			enumByteAt("Release loop", voiceOff, disk.VoiceLoopEndOffset, loopEndOptions),
		}
	default:
		loopIdx := col - 2
		if loopIdx < 0 || loopIdx > 7 {
			return nil
		}
		stOff := disk.VoiceLoopSt0Offset + loopIdx*4
		edOff := disk.VoiceLoopEd0Offset + loopIdx*4
		return []field{
			loopStartAddrAt(fmt.Sprintf("Loop %d start", loopIdx+1), voiceOff, stOff),
			loopEndAddrAt(fmt.Sprintf("Loop %d end", loopIdx+1), voiceOff, edOff),
			uint16At(fmt.Sprintf("Loop %d xfade", loopIdx+1),
				voiceOff, disk.VoiceLoopXFOffset+loopIdx*2, 0, 1023),
			uint16At(fmt.Sprintf("Loop %d time", loopIdx+1),
				voiceOff, disk.VoiceLoopTmOffset+loopIdx*2, 1, 1022),
			loopFineAt(fmt.Sprintf("Loop %d fine", loopIdx+1), voiceOff, stOff),
			loopNextAt(fmt.Sprintf("Loop %d next", loopIdx+1), voiceOff, edOff),
		}
	}
}

// ---- helpers -----------------------------------------------------------

// loopAddrBounds returns the [lo, hi] range a loop address may take,
// clamped to the sample's wave region (F-QA-16): a loop point outside
// [waveStart, waveEnd] produces clicks/garbage on hardware. maskHi is the
// field's own address-mask ceiling. waveEnd is only used as the ceiling
// when set (>0), so voices with unset wave bounds keep the mask ceiling.
func loopAddrBounds(data []byte, voiceOff, maskHi int) (lo, hi int) {
	waveStart := int(binary.LittleEndian.Uint32(data[voiceOff+disk.VoiceWaveStartOffset:]))
	waveEnd := int(binary.LittleEndian.Uint32(data[voiceOff+disk.VoiceWaveEndOffset:]))
	lo, hi = waveStart, maskHi
	if waveEnd > 0 && waveEnd < hi {
		hi = waveEnd
	}
	if lo > hi {
		lo = hi
	}
	return lo, hi
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
