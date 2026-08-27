---
type: topic
title: Multi-disk full dumps
description: Disk 1 carries all metadata for the whole instrument; disk 2 is pure audio continuation. Confirmed on FZ-10M hardware.
tags: [disk, fzf, multi-disk]
updated: 2026-08-27
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 1-3
  - FZ-10M hardware (2-disk save inspected byte by byte)
status: confirmed-hardware
---

# Multi-disk full dumps

The spec (section 1-3) allows a full dump to span 2 floppies via the directory entry's disk number (`disknum` 0 and 1). That single bit is what caps a dump at two disks. The memory ceiling is a separate limit that varies by machine, and it binds first on a stock FZ-1: see [Sample memory per machine](sample-memory.md). Saving a 2-disk instrument from a real FZ-10M and inspecting the output shows:

**Disk 1** (`disknum = 0`) holds the entire instrument's metadata:

- Bank sector `bstep` counts all voices across both disks; without the full count the sampler considers loading complete and never asks for disk 2.
- Voice headers cover every voice, including audio stored on disk 2. Their `wavst` values point into the RAM region filled by disk 2. See [audio-block-padding](../findings/audio-block-padding.md) for wave-address semantics.
- DIS tail `wn` is the total wave sectors across both disks. Multi-disk saves can stamp that total at bank offset 0x290. The field can contain residue, so fizzle accepts it only when a voice crosses disk 1's audio boundary. A separate fizzle-defined record at 0x294 can carry a dump's voice count; see [voice-area-sizing](voice-area-sizing.md).
- Audio for as many voices as fit in 1.25 MB.

**Disk 2** (`disknum = 1`) is pure audio continuation: no bank sector, no voice headers, DIS tail counts identical to disk 1's. The sampler appends its data straight into sample RAM.

Both disks name the file `FULL-DATA-FZ`. The firmware identifies full dumps by that exact name, while another name triggers the `Next disk?` prompt. `fizzle sfz convert --split-disks` assembles through `pkg/voicebuild`, then `pkg/diskadd` writes each disk. `pkg/document` and `pkg/fzf` retain the paired document's validated layout.
