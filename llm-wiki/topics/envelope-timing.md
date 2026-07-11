---
type: topic
title: Envelope timing
description: How the firmware turns 8-stage rate and stop bytes into wall-clock envelope times, and how the studio renders it.
tags: [fzv, envelope, dca, dcf, firmware]
updated: 2026-07-11
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
  - FZ-1 ROM (rate table F000:0490; handlers F000:2039, F000:218B; service loop F000:1CD8)
status: confirmed-firmware
---

# Envelope timing

DCA and DCF envelopes each have 8 stages. On note-on the envelope runs
from stage 0, advancing when each stage's level is reached, and holds
at `sus`; on note-off it resumes from `sus + 1` and runs to `end`.
`sus` can sit beyond `end` (the factory piano has Sus 7, End 4). Rate
bytes carry direction in bit 7 (set means falling) and magnitude in
bits 0 to 6; stop levels are 0 to 255.

Reverse engineering the firmware established the timing model:

- A 128-entry, 16-bit rate table lives at F000:0490 to F000:058F in
  the ROM. `pkg/studio/widgets/envelopevisual` carries a verified copy.
- Each per-voice service tick indexes the table with `rate & 0x7F`,
  negates the value if bit 7 was set, and adds it to a 16-bit ramp
  accumulator. When the accumulator's high byte crosses the stage's
  stop level, the stage advances. The DCF handler reads the table at
  F000:218B; the DCA handler at F000:2039 has identical shape.
- Ticks per stage are therefore `|level_delta| * 256 / table[rate]`.
- The per-voice service routine at F000:1CD8 runs every 8 timer IRQs
  (~6.4 ms each); an 8-phase round is ~50 ms and each DCA state runs
  once per round, giving roughly 25 ms per DCA tick.

The studio's envelope visual multiplies ticks by that ~25 ms to plot
milliseconds. Front-panel value mapping is in
[display-scales](display-scales.md); authoring defaults are in
[voice-authoring-defaults](voice-authoring-defaults.md).

## Open questions

- The ~25 ms per DCA tick is derived, not measured; confirm against
  hardware with a timed recording of a known envelope.
