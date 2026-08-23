---
type: topic
title: Voice-area sizing
description: Size the voice area by the DIS tail's vn where a disk supplies one; only a standalone dump falls back to the validated slot walk.
tags: [fzf, parsing, voices]
updated: 2026-08-23
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md sections 1-5, 2-1, 2-2
  - testdata/corpus (summed bstep equals vn for 80 of 235 full dumps)
  - testdata/synthetic/PREY.img
status: confirmed-hardware
---

# Voice-area sizing

The voice parameter area is `ceil(vn / 4)` sectors: 4 headers per sector at 256-byte intervals, 192 bytes used per slot. Voice `i` sits at `(i / 4) * 1024 + (i % 4) * 256`.

Sizing it means knowing `vn`:

1. On a disk, read `vn` from the DIS tail (see [dis-file-head](../findings/dis-file-head.md)). It is the authority: the firmware sizes the voice area by it, never by a walk.
2. Standalone `.fzf` files have no DIS, so `vn` must be inferred. Bank 0's `bstep` isn't it (see [bstep-key-splits](../findings/bstep-key-splits.md)); sizing by it reads audio bytes as voice headers.

The summed `bstep` can also run below `vn`, because a dump keeps a voice no bank references. `testdata/synthetic/PREY.img` shows it: two banks whose bsteps sum to 4, a DIS tail vn of 5, and the fifth voice (CHEMICAL) in no bank. The same image carries three stale slots past vn, byte-plausible remnants of an earlier, larger save the firmware never zeroed. So for a firmware-authored dump neither bound of the walk is reliable: the bstep sum undercounts, and walking to the first implausible slot overcounts. Only the DIS vn separates live slots from stale ones.

The vn is trusted in one direction only: where it runs above the walk. `testdata/synthetic/TECHNO.img` carries a DIS vn of 30 while the walk finds 32 live, named voices. A vn at or below the walk is therefore an undercount and the walk wins. Which reading the real firmware makes of such a disk is untested. The rule has an accepted blind spot. On a dump whose stale slots walk as plausible up to or past its vn, the walk wins there too. Stale slots then list as voices, and an edit stamps the walked count over the firmware's tail. Hiding live voices would be worse; fizzle pins the TECHNO restamp in a test.

A standalone `.fzf` export can carry the count in a fizzle-defined record at bank offset 0x294: the magic `fzv1`, the count, the dump length, and a CRC32 over the structural bytes. That sits inside the same firmware padding region as the 0x290 wave total. Readers honour it only when the whole record still binds to the dump and the count validates above the walk. A record that outlives an edit dies with it. The offset holds firmware garbage on 203 of 235 corpus dumps, so a bare count there means nothing.

The standalone inference walks voice slots from 0 upward and stops at the first failure. It accepts each slot whose 192-byte header passes a plausibility check. The check wants a valid rate index, monotonic wave pointers, and a known playback mode. The name must be printable or padded. The walk was informed by the name-scan heuristic in [vosmaer-fz1](../sources/vosmaer-fz1.md). The summed `bstep` of every bank bounds the walk. The validation trim handles the overshoot on shared-voice kits, since the sum equals `vn` for only 80 of 235 dumps in the [corpus](../sources/corpus.md). The undercount has no standalone remedy; an extracted dump with a bank-less voice reads short.

fizzle implements the policy once, in `pkg/fzutil`'s resolver: an explicit count wins only where it validates above the walk. `ResolveDiskFZF` takes the DIS tail's count and `ResolveStandaloneFZF` the marker record; the two authorities never compete. The standalone resolver backs `ReadFZF`, so the CLI readers honour a stamped export. `pkg/webcore` resolves the mode at document boundaries, edits under it, and `pkg/diskadd` writes the count back, keeping `wn` in step.

Related heuristic: a printable 12-byte name at header offset 0xB2 marks a voice file; its absence marks a full dump. fizzle uses this for file-type detection.

## Open questions

- `Drums.fzf` (FL-4) has been recorded with three voice counts in past notes: `vp[]` referencing 19 distinct slots (max index 23), "24 voices total", and "vn of about 28". Recount with `fizzle fzf info` and settle the number.
- Whether slot order always tracks wave-address order in firmware-saved dumps. If it does, a cross-slot `wavst` monotonicity check could trim stale slots for standalone files too; test it over the corpus before trusting it.
