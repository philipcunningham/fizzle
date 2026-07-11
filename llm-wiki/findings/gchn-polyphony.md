---
type: finding
title: gchn is an output bitmask that controls polyphony and mute groups
description: 0xff means all 8 generators round-robin; a single bit means one monophonic output, and voices sharing that bit mute each other.
tags: [fzf, bank, polyphony, sfz]
updated: 2026-07-11
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-2
status: confirmed-hardware
---

# gchn is an output bitmask that controls polyphony and mute groups

`gchn` (per key split, `bankdata` 0x182; "OUTPUT" on the front panel)
is a bitmask over the 8 voice generators, each feeding a rear output
jack: bit 0 is output 1, bit 7 is output 8.

- `0xff`: all 8 outputs, full polyphony (round-robin).
- Single bit (`0x01`): one output, monophonic; a new note cuts the
  previous one, and voices sharing the same single-bit value mute each
  other (open/closed hi-hat).
- Multiple bits (`0x05`): limited polyphony across the set outputs.

Shared mute groups combine with `vp[]` voice sharing: several key
splits can point at one voice slot and use distinct or matching `gchn`
bits (see [bstep-key-splits](bstep-key-splits.md) for how sharing
skews key-split counts). fizzle maps SFZ `mutegroup=N` onto separate
single-bit outputs during conversion (`pkg/sfzconvert`) and back on
export (`pkg/sfzexport`).

`gchn` sits in the same `bankdata` sector as
[mchn](mchn-offset.md); the field layout there applies.
