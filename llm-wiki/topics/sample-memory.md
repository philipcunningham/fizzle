---
type: topic
title: Sample memory per machine
description: The FZ-1 shipped with 1 MB and the rack units with 2 MB, and the firmware discovers which at power on. There is no single figure for the series.
tags: [hardware, memory, fzf]
updated: 2026-08-20
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 4 (work area listing)
  - FZ-1 firmware, the wave memory probe at F000:07D4 and length_limit at F000:7A74
  - Micro Music, February 1990, Paul Wiffen on the FZ family (https://www.muzines.co.uk/articles/fz-update/6027)
  - FZ-1 owner report, 2026-08: an FZ-1 refusing a 1,188,000 byte instrument
status: confirmed-firmware
---

# Sample memory per machine

How much audio an FZ holds depends on the machine in front of you, not on the series. Two owners of the same model can differ.

- The FZ-1 keyboard shipped with 1 MB.
- An FZ-1 reaches 2 MB with Casio's MB-10 expansion card, which fits a slot on the back. Replicas are still made.
- The FZ-10M and FZ-20M rack units shipped with 2 MB.

## The machine measures itself

The firmware never assumes a figure. The wave memory probe at `F000:07D4` walks banks through the bank port, writing and reading back two patterns per bank. It stores the count it reaches in `memsize` at RAM `0x08D4`. The Casio spec's work area listing documents that variable as the wave memory size in 64 KB units.

Two bounds fall out of the probe itself. The count starts at 16, so 1 MB is assumed rather than tested. It runs to 64 because the firmware masks the bank index to six bits. Only five of those bits reach memory, which is 32 banks, so 2 MB is the hardware's ceiling.

Everything that spends memory then measures against `memsize`. `length_limit` at `F000:7A74` computes the remaining recording time as `memsize << 15` samples, less what is used. It converts that to hundredths of a second by dividing by 360, which is 36,000 samples per second times 0.01. The Casio spec calls the same unit 10 ms.

## The published times are that arithmetic

Casio's own figures are 14.5 seconds at 36 kHz for 1 MB and 29.1 for 2 MB. At 16 banks the firmware returns 1,456 hundredths, which is 14.56 seconds, and at 32 banks 2,913, which is 29.13. The usable ceiling is therefore the whole nominal byte count, with nothing held back, so a tool can measure against it exactly.

## What the hardware reported

An FZ-1 owner's disk held one voice of 33 seconds at 18 kHz, which is 1,188,000 bytes of audio. The sampler refused it with `NO MEMORY SPACE`. The instrument sits 139,424 bytes past 1 MB and well inside 2 MB, so the machine was unexpanded. The voice block loader at `F000:C626` computes `memsize << 15` as its first act and tracks the load against it. The error came from that comparison.

## A disk isn't a load

A full dump loads as a unit, so the audio area of that one file is what has to fit. Loose voice files and bank dumps on the same floppy are loaded separately. A floppy can therefore carry more audio than the machine holds at once.

## Open questions

- What does a load do to memory that is already resident? Appending is confirmed for the two halves of one split dump (see [Multi-disk full dumps](multi-disk-dumps.md)), and nothing records whether loading a second voice adds to the first or replaces it. A firmware trace of `load` at `F000:B30E` across four cases, or a hardware experiment reading the remaining memory display between loads, would settle it.
- Does any machine in use exceed 2 MB? Third party upgrades are described online, and the bank register's five usable bits argue against more reaching memory without further modification.
