---
type: source
title: FZ-1 firmware reverse engineering
description: What the ROM actually does, established by reverse engineering; cited by ROM address and routine name.
tags: [firmware, primary-source]
updated: 2026-08-27
sources:
  - FZ-1 system ROM (64 KiB, disassembled and annotated)
status: confirmed-firmware
---

# FZ-1 firmware reverse engineering

The annotated FZ-1 ROM and fizzlab execution traces resolve behavior that the specification omits or states incorrectly. Direct hardware observations remain the higher authority.

Cite findings by ROM address and routine name; cite documents by filename and section. Anchors used by this wiki so far:

- The envelope rate table spans `F000:0490` to `F000:058F`. The DCA stage handler starts at `F000:2039`, and the DCF handler reads the table at `F000:218B`.
- `voice_step` at `F000:1CD8` runs at 500 Hz. Each DCA stage advances at 125 Hz; see [envelope-timing](../topics/envelope-timing.md).
- `midi_note_on` starts at `F000:0FFD`. `load` starts at `F000:B30E`, and `save` starts at `F000:B13F`.
- `effectdata_edit_screen` starts at `F000:6B98`. `length_limit` starts at `F000:7A74`.
