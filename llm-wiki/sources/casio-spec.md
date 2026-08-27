---
type: source
title: Casio FZ-1 Data Structures spec
description: The primary format specification (T. Sasaki, Casio R&D, 1987); authoritative for intent, with known errors and compiler-specific reading conventions.
tags: [spec, primary-source]
updated: 2026-08-27
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md
  - llm-wiki/sources/casio-fz1-data-structures.pdf
status: spec-only
---

# Casio FZ-1 Data Structures spec

"Casio Digital Sampling Keyboard Model FZ-1 Data Structures (For Software Developers)", T. Sasaki, Casio R&D, March 1987. The md file is a machine transcription of the PDF; the PDF is the authority where they disagree. Authoritative for intent, overruled by firmware findings and hardware observation.

## Reading conventions

- All multi-byte integers are little-endian (NEC V50, an 8086-family CPU).
- Type sizes are HP 64000 cross-compiler conventions, not modern C: `long` is 4 bytes, `int` is 2, and `short` is 1. Reading `struct voicedata` with 2-byte shorts shifts every offset past 0x12. The spec's offset comments are the source of truth.
- "Word Address" means a 16-bit-word index into the PCM stream, not a byte offset; multiply by 2 for bytes.
- 16-bit fields start on even offsets (V50 bus); one-byte `short` fields pack anywhere.

## Known errors

- File-head counts: order and position wrong ([dis-file-head](../findings/dis-file-head.md)).
- Single bank sector shown; hardware writes up to 8 ([multiple-bank-sectors](../findings/multiple-bank-sectors.md)).
- `dcq` described as a 4-bit field; the full byte is effective ([dcq-full-byte](../findings/dcq-full-byte.md)).
- The `loopst` struct comment says bits 15 to 12 carry loop fine, four bits. The spec's own prose says the upper 8 bits with values 0 to 255, the owner's manual's EX FINE is 1/256 of a sample, and corpus round trips use the full byte, so the comment is the error.
- `bstep` wording conflates key splits with voices ([bstep-key-splits](../findings/bstep-key-splits.md)).
