---
type: topic
title: Voice-area sizing
description: Size the voice area by vn when the file head exists; otherwise walk and validate slots, bounded by summed bstep.
tags: [fzf, parsing, voices]
updated: 2026-07-11
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md sections 1-5, 2-1, 2-2
  - testdata/corpus (summed bstep equals vn for 80 of 235 full dumps)
status: confirmed-hardware
---

# Voice-area sizing

The voice parameter area is `ceil(vn / 4)` sectors: 4 headers per
sector at 256-byte intervals, 192 bytes used per slot. Voice `i` sits
at `(i / 4) * 1024 + (i % 4) * 256`.

Sizing it means knowing `vn`:

1. On a disk, read `vn` from the DIS tail (see
   [dis-file-head](../findings/dis-file-head.md)).
2. Standalone `.fzf` files have no DIS, so `vn` must be inferred.
   Bank 0's `bstep` is not it (see
   [bstep-key-splits](../findings/bstep-key-splits.md)); sizing by it
   reads audio bytes as voice headers.

The reliable inference walks voice slots from 0 upward, accepting each
slot whose 192-byte header passes a plausibility check (rate index in
{0, 1, 2}, wave pointers monotonic, playback mode known, name
printable or padded), and stops at the first failure. The walk was
informed by the name-scan heuristic in
[vosmaer-fz1](../sources/vosmaer-fz1.md). The summed `bstep` of every
bank is a safe upper bound for the walk, but it overshoots on
shared-voice kits: it equals `vn` for only 80 of 235 dumps in the
[corpus](../sources/corpus.md), so the validation trim is essential.
fizzle implements this in `pkg/fzutil` (`CountAllVoices`,
`InferVoiceCount`).

Related heuristic: a printable 12-byte name at header offset 0xB2
marks a voice file; its absence marks a full dump. fizzle uses this
for file-type detection.

## Open questions

- `Drums.fzf` (FL-4) has been recorded with three voice counts in past
  notes: `vp[]` referencing 19 distinct slots (max index 23), "24
  voices total", and "vn of about 28". Recount with `fizzle fzf info`
  and settle the number.
