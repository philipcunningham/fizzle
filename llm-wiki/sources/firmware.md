---
type: source
title: FZ-1 firmware reverse engineering
description: What the ROM actually does, established by reverse engineering; cited by ROM address and routine name.
tags: [firmware, primary-source]
updated: 2026-07-11
sources:
  - FZ-1 system ROM (64 KiB, disassembled and annotated)
status: confirmed-firmware
---

# FZ-1 firmware reverse engineering

The FZ-1 system ROM has been reverse engineered: disassembled,
annotated, and partially decompiled, with an engineer-facing system
guide covering MIDI, the voice service loop, disk/file I/O, and the
panel. Outranked only by direct hardware observation; outranks the
spec.

Cite findings by ROM address and routine name; cite documents by
filename and section. Anchors used by this wiki so far:

- Envelope rate table at `F000:0490` to `F000:058F` (128 entries,
  16-bit); DCA stage handler at `F000:2039`; DCF stage handler reads
  the table at `F000:218B`.
- Per-voice service routine at `F000:1CD8`, run every 8 timer IRQs of
  ~6.4 ms (see [envelope-timing](../topics/envelope-timing.md)).
- MIDI note-on handler `midi_note_on` at `F000:0FFD`; file I/O
  routines `load` at `F000:B30E` and `save` at `F000:B13F`.

The verified rate table is embedded in
`pkg/studio/widgets/envelopevisual`.
