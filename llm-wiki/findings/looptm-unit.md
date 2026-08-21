---
type: finding
title: The looptm unit is contested
description: The spec calls looptm a duration in 16 ms steps; the FZ book shows a value of one repeating a loop three times; the struct comment hedges between the two.
tags: [fzv, loops]
updated: 2026-08-21
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
  - The Casio FZ-1 and FZ-10M Book, chapter 8.3, page 73, Figure 33
status: suspect
---

# The looptm unit is contested

## Claim

What a timed loop's `looptm` value counts is unsettled. The spec's
prose says a duration. The FZ book's worked figure reads as a repeat
count, and Casio's own struct comment declines to choose.

## Evidence

- The spec (section 2-1) says `looptm` "denotes a timing duration for
  Multi Loop", 1 to 1022, settable "by 16 milliseconds from 16
  milliseconds up to 16 seconds".
- The same listing's field comment reads `loop time (' or times)`,
  which hedges between a duration and a count.
- The FZ book's Figure 33 caption states that a Loop Time value of
  one causes a loop to repeat three times (page 73). Its body text
  says a timed loop repeats "for the specified amount of time".
- The owner's manual (pages 69 to 71) walks the panel without
  defining the number at all. Its screens still carry range
  evidence. Step 12 shows LOOP TIME at 1023 where the spec stops at
  1022. The same field shows END at step 11 and SUS on the next loop
  screens, so the panel folds both designations into the time
  control as positions past the numbers. The file stores them apart,
  in `loop_sus` and `loop_end`, so the screens prove a panel
  encoding rather than a stored one.
- The book's Experiment 12 (pages 75 to 76) sets every loop's time to
  one, with each loop spanning a spoken word, and hears each word
  repeat a few times. A word length loop runs far past 16 ms, so a
  duration reading predicts a single pass there.

A duration with a whole pass floor would reconcile the two for loops
shorter than about 5 ms. Experiment 12 contradicts it at word length,
so the repeat count reading holds the stronger evidence today.

## What fizzle implements

Authoring writes `looptm` per
[voice-authoring-defaults](../topics/voice-authoring-defaults.md) and
round trips it untouched. The browser editor edits the value as R14's
loop time attribute. Nothing in fizzle interprets the unit, and the
preview ignores timed loops: see
[multi-loops](../topics/multi-loops.md).

## Open questions

- Does the firmware treat `looptm` as elapsed time or as passes? A
  firmware trace of the loop advance during a timed loop, or a
  hardware recording of one loop at two lengths under the same
  `looptm`, would settle it.
