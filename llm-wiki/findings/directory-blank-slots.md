---
type: finding
title: Deleted directory entries leave blank slots in place
description: The firmware deletes a file by zeroing the first name byte and saves later files behind the gap; whether it reads past one is open.
tags: [disk, directory, firmware]
updated: 2026-08-23
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 1-3
  - testdata/synthetic/PREY.img
status: confirmed-hardware
---

# Deleted directory entries leave blank slots in place

The spec (section 1-3) says a directory entry whose first name byte is NULL "is regarded as blank". The sentence covers that one entry, not the rest of the sector. The firmware deletes a file by zeroing the first name byte and leaves the other 15 bytes in place. It writes later files into later slots, so a live entry can sit behind any number of blank ones.

## Evidence

`testdata/synthetic/PREY.img`, saved by an FZ-1 (serial 001969) through a Gotek drive. Slot 0 holds a deleted voice: the first name byte is 0x00 and the rest still reads `DDICTED` plus a voice type and a DIS pointer. The CAT no longer allocates that DIS's sectors. Slot 1 holds the live `FULL-DATA-FZ` entry. A reader that stops at the first blank slot sees an empty disk. The image came from a user's machine; the sampled audio it carries is third-party material whose redistribution terms are unconfirmed.

The write side is what these bytes prove: the firmware deletes in place and saves around the gap. Whether its own directory scan also reads past a gap is inferred, not observed.

## What fizzle implements

`pkg/disk/disk.go` (`Directory`) steps over blank slots. A slot whose DIS pointer lands outside the data sectors is no entry either, so trailing rubbish neither lists nor refuses a disk. `disklist` still shows a printable-named one as a corrupt row. `RemoveFile` resolves a name to its raw slot rather than its position in the filtered listing, then compacts the directory. fizzle's own writes keep the directory dense.

## Open questions

- Trace the firmware's directory scan (or load a gapped disk on hardware) to confirm it reads past a blank slot. `NextFreeDirSlot` fills the lowest gap, so fizzle can hand a still-gapped directory back to the sampler.
