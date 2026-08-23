---
type: finding
title: DIS / file head deviates from the spec in three ways
description: Extent area runs to 0x3F9, counts sit at the sector end in bn vn wn order, and the DIS is the first sector of its own chain.
tags: [disk, dis, parsing]
updated: 2026-08-23
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 1-4
status: confirmed-hardware
---

# DIS / file head deviates from the spec in three ways

The spec (section 1-4) puts up to 64 extent entries at 0x000 and the
counts at 0x100 in the order `vn bn wn`. Hardware writes differently:

1. The extent area extends to offset 0x3F9 (up to ~254 `[ss, es]`
   uint16 pairs). Read until the first `[0, 0]` pair or 0x3FA.
2. The counts sit at the end of the sector, in the order `bn vn wn`:
   `bn` at 0x3FA, `vn` at 0x3FC, `wn` at 0x3FE (uint16 each).
3. The DIS sector is itself `ss` of the first extent; file content
   starts at `ss + 1`. The DIS is in the chain but skipped when
   reading content.

fizzle reads the hardware offsets (`pkg/disk`, `pkg/diskget`,
`pkg/diskadd`). The sector-end count layout was first documented by
[vosmaer-fz1](../sources/vosmaer-fz1.md) and confirmed against the
corpus. Wrong tail counts make the sampler misparse the file.
Standalone `.fzf` files extracted with `disk get` lose the DIS and its
counts entirely; see [voice-area-sizing](../topics/voice-area-sizing.md).
