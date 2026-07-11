---
type: topic
title: Front-panel display scales
description: How the FZ front panel maps raw header bytes to its 0 to 99 and -15 to +15 displays; calibrated on hardware, absent from the spec.
tags: [fzv, display, hardware]
updated: 2026-07-11
sources:
  - FZ-10M hardware (calibration disk images; BRASS1 D3 1)
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
`disk.KFDisplayToByte` in `pkg/disk/voice.go`; the studio and
`pkg/fzvinfo` render through them.

## Open questions

- The five velocity sensitivity fields (`vel_dca_kf`, `vel_dca_rs`,
  `vel_dcf_kf`, `vel_dcf_rs`, `vel_dcq_kf`) almost certainly have a
  narrower display range too, but the mapping is uncalibrated; fizzle
  exposes the raw signed byte for now.
