---
type: finding
title: bstep counts key splits per bank, not file voices
description: Bank 0's bstep equals the file voice count for only 24 of 235 corpus dumps; sizing the voice area by it reads garbage.
tags: [fzf, bank, parsing]
updated: 2026-07-11
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-2
  - testdata/corpus (bstep equals vn for 24 of 235 full dumps)
status: confirmed-hardware
---

# bstep counts key splits per bank, not file voices

The spec calls `bstep` "the current number of key splits or the number
of voices which the bank uses". The two coincide only in a single-bank
dump where every key split uses a distinct voice. Across the
[corpus](../sources/corpus.md), bank 0's `bstep` equals the file's
voice count `vn` for just 24 of 235 full dumps. It diverges two ways:

1. Multi-bank dumps (210 of 235): each bank's `bstep` counts only that
   bank's key splits.
2. Shared-voice kits: `vp[]` points several key splits at one voice
   slot, so key splits exceed distinct voices. `Drums.fzf` (FL-4)
   carries `bstep = 61` in bank 0 while `vp[]` references only 19
   distinct voice slots.

Never size the voice area by bank 0's `bstep`; see
[voice-area-sizing](../topics/voice-area-sizing.md) for what fizzle
does instead.
