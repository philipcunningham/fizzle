---
type: topic
title: Multi-disk full dumps
description: Disk 1 carries all metadata for the whole instrument; disk 2 is pure audio continuation. Confirmed on FZ-10M hardware.
tags: [disk, fzf, multi-disk]
updated: 2026-07-11
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 1-3
  - FZ-10M hardware (2-disk save inspected byte by byte)
status: confirmed-hardware
---

# Multi-disk full dumps

The spec (section 1-3) allows a full dump to span 2 floppies via the
directory entry's disk number (`disknum` 0 and 1). Two disks is also
the hardware maximum: the FZ series has 2 MB of sample RAM. Saving a
2-disk instrument from a real FZ-10M and inspecting the output shows:

**Disk 1** (`disknum = 0`) holds the entire instrument's metadata:

- Bank sector `bstep` counts all voices across both disks; without the
  full count the sampler considers loading complete and never asks for
  disk 2.
- Voice headers for every voice, including those whose audio lives on
  disk 2; their `wavst` values point past disk 1's local audio into
  the RAM region that disk 2's audio fills (wave-address semantics:
  [audio-block-padding](../findings/audio-block-padding.md)).
- DIS tail `wn` is the total wave sectors across both disks. fizzle
  stores that total at bank sector offset 0x290 (a fizzle-defined
  field, zero on single-disk dumps) so `pkg/diskadd` can write the
  correct `wn`.
- Audio for as many voices as fit in 1.25 MB.

**Disk 2** (`disknum = 1`) is pure audio continuation: no bank sector,
no voice headers, DIS tail counts identical to disk 1's. The sampler
appends its data straight into sample RAM.

Both disks name the file `FULL-DATA-FZ`; the firmware identifies full
dumps by that exact name, and any other name triggers the "Next disk?"
prompt. fizzle implements the split in `fizzle sfz convert
--split-disks` (`pkg/disk/disk.go`, `pkg/diskadd`, `pkg/fzfinfo`).
