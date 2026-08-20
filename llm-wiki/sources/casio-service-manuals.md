---
type: source
title: Casio service manuals and third-party books
description: The hardware documents behind the DCA chip, the playback path, and the fitted memory; what each is authoritative for and where it stops.
tags: [hardware, dca, primary-source]
updated: 2026-08-20
sources:
  - Casio FZ-1 Service Manual & Parts List, April 1987
  - Casio FZ-20M service manual, circuit explanation and parts list
  - The Casio FZ-1 and FZ-10M Book, and the FZ-1 Book
  - Casio FZ-1 Owner's Manual
status: confirmed-hardware
---

# Casio service manuals and third-party books

Scanned PDFs held outside this repository. Pages are cited by document and page number rather than by URL. The copies consulted carry a manualslib.com watermark. Nothing from them is reproduced here beyond the findings they support.

## What each is authoritative for

**FZ-1 Service Manual & Parts List (April 1987).** Board schematics and the per-board parts list. It is authoritative for what is fitted. Four `FM-1` LSIs carry the DCF and DCA, two `PCM54HP` converters carry the samples, `MB653121` is the playback gate array, and 32 `MN41256-08` chips are the wave memory. That last count is the 1 MB an unexpanded FZ-1 carries. Its schematic sheets are dense scans, and component labels need magnifying before they can be read.

**FZ-20M service manual.** The better of the two for how the machine works, because it carries a circuit explanation in prose. It is authoritative for the DCA. It names the part `MB87186` and gives its block diagram as two DCF and two DCA sections, with `G = 0 to -87.75 dB`. It prints the pin table that splits the DCA control into a gain word and an amplitude word. Its playback section describes sample data reaching two converters through gate array GAX. The DCFs filter and gain control what comes out. The FZ-20M shares this engine with the FZ-1, so its explanations carry across; the parts differ only in naming and in fitted memory.

**Where the manuals stop.** Neither carries a gain table for the DCA. How a control word becomes decibels is therefore inference from the total range, not a documented per-step figure. Neither shows how the chip's F/A and FC/Q select pins are driven. Which port maps to which register is inferred from what the firmware writes where.

## The books

*The Casio FZ-1 and FZ-10M Book*, *The FZ-1 Book*, and the owner's manual are player facing. They carry no timing tables, so they settle nothing about rates in seconds. They do corroborate firmware findings from the player's side, which is worth having when a disassembly is the only other evidence:

- An envelope with no sustain step fades to nothing however long a key is held, and its rates set how long that fade takes. The books describe the self-terminating one shot of [envelope-timing](../topics/envelope-timing.md) to players.
- A step set to END becomes the last step of the cycle, and the FZ sets its level to zero. The books describe the forced end stage at F000:1351.
- The quickest rate is 99, drawn on the display as a vertical line. The books describe the instant rate at F000:2094.

## Known errors

None found so far. The FZ-20M manual's DCA pin table says the gain write puts its data on the upper 2 bits of the data bus. That reading points at D7 and D6. The firmware writes 0 to 3 there. So either the wording means the top two bits of the control word, or something sits between the CPU and the chip. The question is recorded as open rather than as an error.
