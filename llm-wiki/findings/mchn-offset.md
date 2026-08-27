---
type: finding
title: The mchn array sits at 0x142, not 0x104
description: Summing the spec's bankdata field sizes puts the MIDI channel array at 0x142; writing at 0x104 corrupts the cent array and wrecks pitch.
tags: [fzf, bank, midi]
updated: 2026-08-27
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-2 List B
status: confirmed-hardware
---

# The mchn array sits at 0x142, not 0x104

`struct bankdata` orders its arrays `hwid`, `lwid`, `htch`, `ltch`, `cent`, `mchn`, `gchn`, `bvol`, `vp`: 64 one-byte entries each except `vp` (uint16). Summing those sizes puts `mchn` at 0x142. Writing MIDI channels at 0x104 lands inside `cent[]`, zeroing root keys from voice 2 onward and causing severe pitch errors on hardware.

fizzle reads and writes `mchn` at 0x142 (`pkg/fzfinfo`, `pkg/voicebuild`, `pkg/fzfmidi`). Buchty's independently written `bank_data` struct agrees with the summed layout ([buchty-fztoolkit](../sources/buchty-fztoolkit.md)).

Neighbouring `bankdata` fields: [gchn](gchn-polyphony.md) (the output bitmask at 0x182) and [bstep](bstep-key-splits.md) (the key-split count). A dump carries one such sector per bank ([multiple-bank-sectors](multiple-bank-sectors.md)).
