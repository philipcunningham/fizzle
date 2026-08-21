---
type: topic
title: Multi-loop playback
description: The eight loops play as a chain in numerical order; sustain and end are two roles inside it, assigned through each loop's time field.
tags: [fzv, loops, playback]
updated: 2026-08-21
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
  - The Casio FZ-1 and FZ-10M Book, chapter 8.3, pages 71 to 78
  - Casio FZ-1 Owner's Manual, pages 69 to 71
status: spec-only
---

# Multi-loop playback

A voice's eight loops aren't alternatives competing for the sustain
and end designations. They form a chain, played strictly in numerical
order, and the two designations are roles inside that chain. The FZ
book calls it the loop cycle (chapter 8.3, page 73).

## The three roles

Each loop's time field takes a number, SUS, or END, and the value is
what gives the loop its role:

- A number makes it a timed loop. It repeats for its own time, then
  playback continues through the sample to the next loop's start.
- SUS makes it the sustain loop, the hold point. One per voice.
- END makes it the end loop, which other samplers call a release
  loop. One per voice, and nothing after its end point is ever heard.

## The loop cycle

Key down runs the chain from the first loop. Each timed loop repeats
for its time, then playback traces on to the next loop, until the
sustain loop is reached. The sustain loop repeats for as long as the key or
pedal is held; the sample past it stays unheard until release. Key up
resumes the chain, and the remaining timed loops each run their time.
The end loop then repeats while the voice is audible, so the DCA
release is what ends it (book, page 73, Figure 33).

With no sustain and no end loop, all eight can be timed. The note
then plays through the whole chain and out, however long the key is
held. With `loop_sus = 8` and no loops at all, the voice is a plain
one shot.

## Trace, Skip, and the seam

Each loop's next flag governs the unlooped audio between its end and
the next loop's start. Trace plays it; Skip jumps straight to the
next loop's start (book, page 74). Audio after the end loop, or after
a final loop set to Skip, never sounds, so the book recommends
truncating it away to reclaim memory.

The loop itself is a jump back: forward playback, then a jump from
end to start, never a ping pong (owner's manual, page 71). A cross
fade time above zero overlaps the loop's start and end and balances
them against each other, smoothing the seam (book, page 78).

## The fields

Section 2-1 of the spec carries the whole mechanism:

- `loop_sus`: 0 to 7 names the sustain loop; 8 means none.
- `loop_end`: 0 to 7 names the chain's last loop; 8 runs all eight.
- `looptm[8]`: the per loop time. Its unit is contested: see
  [looptm-unit](../findings/looptm-unit.md).
- `loopxf[8]`: the cross fade time, 0 to 1023; 0 means none.
- `looped[8]` MSB: 1 for Skip, 0 for Trace.
- `loopst[8]` upper 8 bits: loop fine. The owner's manual's EX FINE
  positions a loop point in 1/256 of a sample (page 69).

## What fizzle implements

Authoring writes one sustain loop or a one shot, with the chain's
auxiliary fields at their defaults: see
[voice-authoring-defaults](voice-authoring-defaults.md). The browser
editor reads and edits all eight loops with their cross fade and time
attributes. The preview repeats the sustain loop only; timed loops,
Skip, and the cross fade don't reach it.
