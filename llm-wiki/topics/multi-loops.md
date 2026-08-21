---
type: topic
title: Multi-loop playback
description: The eight loops play as a chain in numerical order; sustain and end are two roles inside it, assigned through each loop's time field.
tags: [fzv, loops, playback]
updated: 2026-08-21
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
  - FZ-1 ROM (note on F000:122B; note off F000:1515; loop advance F000:1D11 to F000:1D43)
  - The Casio FZ-1 and FZ-10M Book, chapter 8.3, pages 71 to 78
  - Casio FZ-1 Owner's Manual, pages 69 to 71
status: confirmed-firmware
---

# Multi-loop playback

A voice's eight loops aren't alternatives competing for the sustain and end designations. They form a chain, played strictly in numerical order, and the two designations are roles inside that chain. The FZ book calls it the loop cycle (chapter 8.3, page 73), and the firmware runs it exactly as described.

## The three roles

On the panel, each loop's time field takes a number, SUS, or END, and that value is what gives the loop its role. The file stores the roles apart from the time, in `loop_sus` and `loop_end`; the panel's SUS and END are labels derived from those two fields.

- A number makes it a timed loop. It repeats for its own time, then playback continues through the sample to the next loop's start.
- SUS makes it the sustain loop, the hold point. One per voice.
- END makes it the end loop, which other samplers call a release loop. One per voice, and nothing after its end point is ever heard.

## The loop cycle

Key down runs the chain from the first loop. Each timed loop repeats for its time, then playback traces on to the next loop, until the sustain loop is reached. The sustain loop repeats for as long as the key or pedal is held; the sample past it stays unheard until release. Key up resumes the chain, and the remaining timed loops each run their time. The end loop then repeats while the voice is audible, so the DCA and DCF releases are what end it (book, page 73, Figure 33).

With no sustain and no end loop, all eight can be timed. The note then plays through the whole chain and out, however long the key is held. With `loop_sus = 8` and no loops at all, the voice is a plain one shot.

The chain's order is the loop numbering, not the sample's layout. The book's Experiment 12 chains the words of a phrase in reverse, loop by loop (book, page 76). A next loop can therefore sit behind the one before it, and the jump runs backward through the sample.

## How the firmware runs it

One cap and one counter drive the whole chain, the same shape the DCA envelope uses.

Three bytes of the per-voice slot carry the state. `gene+0x0E` is the loop the voice is on. `gene+0x0F` holds the cap in its low 6 bits, with a running flag at 0x40 and a released flag at 0x80. `gene+0x10` counts the current loop's elapsed time.

Note on starts the chain at loop 0 and parks the timer, at F000:1212 to F000:121C. It sets the cap to `min(loop_sus, loop_end)` at F000:122B, four instructions before it caps the DCA envelope at `min(dca_sus, dca_end)` the same way. Note off raises the cap to `loop_end` at F000:1515, preserving the two flag bits.

The advance sits in the per-voice service at F000:1D15. It runs only while the cap sits above the current loop, so reaching the cap stops the chain and leaves that loop repeating. That single rule produces both holds. The sustain loop holds while the key is down, because note on capped the chain there. The end loop repeats afterwards, because note off moved the cap to it.

While the chain is below its cap, the counter at `gene+0x10` increments and is compared against `looptm[index]` at F000:1D34. Passing it parks the counter at 0xFFFE and advances the loop with `INC byte ptr [DI+0x0E]` at F000:1D43.

A negative counter is parked and never expires, which is how the timer runs only while a loop is actually sounding. Note on parks it, expiry parks it, and `isr_voice_segment_advance` parks it again at F000:251F. The running value comes from the state 1 handler at F000:2033.

The counter advances once every eight service calls (F000:1D11), and the service itself runs every eight timer IRQs. At the 4 kHz IRQ that is 62.5 Hz, so one `looptm` unit is 16 ms: see [looptm-unit](../findings/looptm-unit.md).

Because the timer stops at the cap, the end loop's own `looptm` is never read.

## Trace, Skip, and the seam

Each loop's next flag governs the unlooped audio between its end and the next loop's start. Trace plays it; Skip jumps straight to the next loop's start (book, page 74). Audio after the end loop, or after a final loop set to Skip, never sounds, so the book recommends truncating it away to reclaim memory.

The loop itself is a jump back: forward playback, then a jump from end to start, never a ping pong (owner's manual, page 71). A cross fade time above zero overlaps the loop's start and end and balances them against each other, smoothing the seam (book, page 78).

## The fields

Section 2-1 of the spec carries the whole mechanism:

- `loop_sus`: 0 to 7 names the sustain loop; 8 means none.
- `loop_end`: 0 to 7 names the chain's last loop; 8 runs all eight.
- `looptm[8]`: the per loop time, in 16 ms units, confirmed against the ROM in [looptm-unit](../findings/looptm-unit.md).
- `loopxf[8]`: the cross fade time, 0 to 1023; 0 means none.
- `looped[8]` MSB: 1 for Skip, 0 for Trace.
- `loopst[8]` upper 8 bits: loop fine. The owner's manual's EX FINE positions a loop point in 1/256 of a sample (page 69). The spec's own struct comment says four bits; that slip is logged under [casio-spec](../sources/casio-spec.md) known errors.

## What fizzle implements

Authoring writes one sustain loop or a one shot, with the chain's auxiliary fields at their defaults: see [voice-authoring-defaults](voice-authoring-defaults.md). The browser editor reads and edits all eight loops with their cross fade and time attributes. The preview repeats the sustain loop while a key is held, then moves the window to the end loop at key up. Timed loops, Skip, and the cross fade still don't reach it.

## Open questions

- Trace and Skip rest on the book and the manual. The firmware's use of the `looped` MSB is untraced, so how a Skip transition reaches the sample hardware is unrecorded.
- What starts a loop's timer is untraced. The writers are known, at F000:2033 and F000:251F, but the conditions they run under aren't, so how long a loop sounds before its time begins counting is unrecorded.
- The cross fade's effect on the seam is documented by the manual, not by a trace.
