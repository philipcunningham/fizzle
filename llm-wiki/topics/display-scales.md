---
type: topic
title: Front-panel display scales
description: How the FZ front panel maps raw header bytes to its 0 to 99 and -15 to +15 displays; calibrated on hardware, absent from the spec.
tags: [fzv, display, hardware]
updated: 2026-08-22
sources:
  - FZ-10M hardware (calibration disk images; BRASS1 D3 1)
  - FZ-1 system ROM executed under an emulator (panel driven, bytes read back)
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
status: confirmed-hardware
---

# Front-panel display scales

The FZ-1/FZ-10M front panel never shows raw header bytes. Envelope
values display on a 0 to 99 scale. Key-follow values display as -15
to +15. The spec never gives the mapping; these were calibrated
against hardware.

The [undecyclenate editor](../sources/undecyclenate-editor.md)
documents the same mismatch from the UI side. It exposes raw 0 to 127
and 0 to 255 ranges where the FZ shows 0 to 99.

**Rates** (magnitude 0 to 127; bit 7 is direction):

- display = `(magnitude * 100) >> 7`
- byte magnitude = `ceil(display * 128 / 100)`

**Stop levels** (byte 0 to 255):

- display = `ceil(byte * 99 / 255)` (0 maps to 0)
- byte = `floor(255 * (display - 1) / 99) + 1` (0 maps to 0)

Confirmed against BRASS1 D3 1: rate byte 127 displays 99, stop byte
218 displays 85.

**Key follow / rate scaling** (signed byte, applies to `dca_kf`,
`dca_rs`, `dcf_kf`, `dcf_rs`):

- display = `clamp(int8(byte) / 8, -15, +15)`
- byte = `uint8(int8(display * 8))`

Validated on FZ-10M with calibration images at bytes 0, 1, 4, 8, 15,
64, 127, 128. Implemented as `disk.KFByteToDisplay` and
`disk.KFDisplayToByte` in `pkg/disk/voice.go`; `pkg/fzvinfo`,
`pkg/webcore`, and `pkg/sfzexport` render through them.

## Mappings read off the panel under an emulator

These come from driving the FZ-1 firmware under an emulator: set the
value on the panel, save, and read the bytes back. That beats a static
read of the ROM and falls short of a measurement on a real device.
Treat each row as provisional until hardware confirms it.

| Field | Panel range | Mapping |
|---|---|---|
| `vel_dca_kf`, `vel_dca_rs`, `vel_dcf_kf`, `vel_dcf_rs` | -127 to +127 | the raw signed byte |
| `vel_dcq_kf` | 0 to 127 | the same byte unsigned; the row carries no sign column and refuses to go below zero |
| `dcp` (TUNE) | -100 to +100 | `word = display * 255 / 100`, truncated toward zero. Reading back, the panel takes the magnitude from the low byte and the sign from the word, so a word beyond the span wraps |
| `lfo_delay` (DELAY) | 0 to 127 | `word = display * 16`. The same row writes `lfo_atck` as `18 - ceil(display / 8)`: there is no independent attack row |
| `bvol` (AREA LEVEL) | 0 to 127 | `byte = 127 - display`, so a stored 0 is the panel's loudest |

Two rows here replaced earlier readings taken statically from the ROM's
bounds table. Velocity to resonance was recorded as plus or minus 100,
and the DCF rate as 0 to 127. Both came from misjudging the 24 byte
record's phase by one, which still decodes into plausible bounds.

`lfo_atck` and `lfo_dcq` have no panel row at all. The DELAY row derives
the attack, and nothing reaches the resonance depth. `lfo_dcq` is zero
in all 735 voices unpacked from the Casio factory library, and it can't
be used on a physical unit.

## Open questions

- Whether the four writes an emulated panel makes with no edit also
  happen on hardware: rate sign bits set across all eight envelope
  stages, one stop level rewritten, a loop sustain index set past the
  last loop, and a tune word nudged by one.
