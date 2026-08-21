---
type: finding
title: looptm is a duration in 16 ms units
description: The spec's duration reading is right; the FZ book's repeat-count caption describes what a player hears, and the 1024 the corpus writes on end loops is never read at playback.
tags: [fzv, loops]
updated: 2026-08-21
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
  - FZ-1 ROM (loop advance F000:1D11 to F000:1D43)
  - The Casio FZ-1 and FZ-10M Book, chapter 8.3, page 73, Figure 33, and Experiment 12, pages 75 to 76
  - testdata/corpus, 4,954 voices
status: confirmed-firmware
---

# looptm is a duration in 16 ms units

## Claim

A timed loop's `looptm` counts elapsed time, not passes, in units of 16 ms.

## Evidence

The firmware settles it. At F000:1D1F the advance loads a counter from `gene+0x10`, increments it, and compares it against `looptm[index]` at F000:1D34. Nothing in the path observes the sample position, so a pass through the loop never enters the arithmetic.

The rate matches the spec's own figure. The counter advances on one service call in eight (F000:1D11). The service itself runs on one timer IRQ in eight. At the 4 kHz IRQ that is 62.5 Hz, so a unit is 16 ms. The spec calls `looptm` settable "by 16 milliseconds from 16 milliseconds up to 16 seconds". Its stated maximum of 1022 units is 16.35 s.

The FZ book reads as a repeat count from the keyboard. Its Figure 33 caption says a Loop Time of one repeats a loop three times. Experiment 12 hears each word of a phrase repeat under a time of one. A 16 ms timer predicts a single pass on a word length loop. The caption therefore describes an audible effect the service routine alone doesn't account for.

## The 1024 the corpus writes

Every usable loop named by `loop_end` alone carries `looptm = 1024`, across all 2,554 of them. No timed loop carries that value. Sustain loops run 0, 100, and 1024, the last only where one loop serves both roles. Both 0 and 1024 sit outside the spec's stated 1 to 1022.

The firmware explains why the file can hold a meaningless value there. The advance runs only while the cap sits above the current loop (F000:1D1A), and note off sets the cap to `loop_end`. The end loop's timer therefore never runs, and its `looptm` is never read. The value is an authoring marker for the END designation. That matches the panel, which folds SUS and END into the time control as positions past the numbers.

## What fizzle implements

Authoring writes `looptm` per [voice-authoring-defaults](../topics/voice-authoring-defaults.md) and round trips it untouched. The browser editor edits the value as R14's loop time attribute. Nothing in fizzle interprets the unit, and the preview ignores timed loops: see [multi-loops](../topics/multi-loops.md).

## Open questions

- What the book hears in Experiment 12 doesn't follow from a 16 ms timer alone. The timer runs only while a loop sounds, and what starts it is untraced, so the audible repeat count may come from that gating rather than from `looptm`.
