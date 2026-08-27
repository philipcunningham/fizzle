---
type: finding
title: dcq resonance uses the full byte, not the upper nibble
description: The spec says only the upper 4 bits are effective; the FZ-10M reads the whole 0 to 127 byte.
tags: [fzv, dcf, resonance]
updated: 2026-08-27
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
status: confirmed-hardware
---

# dcq resonance uses the full byte, not the upper nibble

The spec describes `dcq` (voice header 0x77, filter resonance offset) as 0 to 127. It adds: "however, notice that the effective bit number is upper 4 bits". On an FZ-10M the entire byte is effective. Writing the low nibble changes audible resonance, and the front panel (0 to 127 scale) responds to single-byte increments.

Confirmed by `pkg/voiceedit` round-trip tests: the byte written by `--resonance N` reads back unchanged via `fzv info` and matches the front-panel display. fizzle treats the field as a full byte.
