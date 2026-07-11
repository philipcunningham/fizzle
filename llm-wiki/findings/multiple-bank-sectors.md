---
type: finding
title: Full dumps carry up to 8 bank sectors
description: The spec describes a single bank layout; hardware saves one bank sector per bank, and the voice area follows all of them.
tags: [fzf, bank, parsing]
updated: 2026-07-11
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md sections 1-5, 2-2
  - testdata/corpus (210 of 235 full dumps are multi-bank)
status: confirmed-hardware
---

# Full dumps carry up to 8 bank sectors

The spec's Full Data diagram (section 1-5) shows a single bank sector,
but real hardware saves the whole bank set. Each bank gets one
1024-byte `bankdata` sector, up to 8, and the voice parameter area
follows the last one. 210 of the 235 full dumps in the
[corpus](../sources/corpus.md) are multi-bank.

FZF readers must count consecutive valid bank sectors before locating
the voice area. fizzle does this in `pkg/fzfinfo`; per-bank `bstep`
consequences are in [bstep-key-splits](bstep-key-splits.md).
