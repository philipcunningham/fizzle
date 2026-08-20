---
type: topic
title: Envelope timing
description: How the firmware turns 8-stage rate and stop bytes into wall-clock envelope times, and what the output stage does to them.
tags: [fzv, envelope, dca, dcf, firmware]
updated: 2026-08-20
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
  - FZ-1 ROM (rate table F000:0490; output table F000:0590; handlers F000:2039, F000:218B, F000:20BD; slew F000:0A49; service loop F000:1CD8; note on F000:12B4; note off F000:1512)
status: confirmed-firmware
---

# Envelope timing

DCA and DCF envelopes each have 8 stages. On note on the envelope runs from stage 0, advancing when each stage's level is reached, and holds at `sus`. On note off it resumes from `sus + 1` and runs to `end`. `sus` can sit beyond `end`: the factory piano has Sus 7, End 4.

Rate bytes carry direction in bit 7, set meaning falling, and magnitude in bits 0 to 6. Stop levels are 0 to 255.

## The stepper

A 128-entry, 16-bit rate table sits at F000:0490 to F000:058F. Each update indexes it with `rate & 0x7F` and negates the value when bit 7 is set. It adds the result to a 16-bit accumulator whose high byte is the current level. When that high byte reaches the stage's stop, the accumulator snaps to `stop << 8` and the stage advances. The DCF handler reads the table at F000:218B; the DCA handler at F000:2039 has identical shape.

Updates per stage are `|level delta| * 256 / table[rate]`.

## The clock

Timer channel 0 is re-armed with 1000 counts at 4 MHz at F000:0A25, so IRQ0 is 4 kHz. The ISR adds 0x20 to a byte at [0x040A] and runs the slow chain only on carry. The per-voice service at F000:1CD8 therefore runs at 500 Hz. It dispatches one state per voice per call, and states 3 and 7 both point at the DCA handler. A DCA stage advances 125 times a second.

Seconds per stage are therefore `|level delta| * 2.048 / table[rate]`. A full sweep takes 0.387 s at panel 50, 2.30 s at panel 25, and 7.46 s at panel 12.

Note on clamps an effective rate into 1 to 0x7F at F000:12E9, so a stored rate of 0 never stalls a stage.

## The slew, and the cliff at panel 99

The envelope's own timing isn't the whole story. The ISR moves the output code toward its target by one unit per 4 kHz tick at F000:0A5D. No segment therefore crosses the 224 to 895 code range faster than 168 ms. Anything nominally faster is slew limited, which is where the FZ's characteristic softness comes from.

One case escapes it. A rate magnitude of 0x7F, which the panel shows as 99, writes the ports directly at F000:2094 and skips the slew entirely. Panel 99 is an instant jump where panel 98 takes 168 ms.

## Level to the wire

F000:20BD composes the level with modulation and main volume, then maps it to the code the output stage carries. Up to 0x9F the code is the level plus 0xE0, giving 224 to 383. Above it, a 96-entry table at F000:0590 expands, its slope rising from 1 to about 16, giving 384 to 895. The value 223 is reserved: the slew reaching it frees the voice slot at F000:0A87.

How that code becomes loudness is unknown. A log-domain converter wouldn't need an expansion table, which argues against an exponential law, and nobody has measured the real thing.

## Note on scaling

Velocity and key scaling are applied once, at note on. The firmware writes scaled per-voice copies of every rate and every stop at F000:12B4 to F000:135E. `dca_kf` and `vel_dca_kf` shift levels; `dca_rs` and `vel_dca_rs` shift rates. Velocity enters as `min(velocity + 0x10, 0x7F)`, and a sensitivity of zero is a no op at any velocity. The stage named by `dca_end` is then forced falling to a stop of zero, whatever the file stores.

## Release

Note off sets the cap to `dca_end` at F000:1512. A voice that reached its sustain stage runs `sus + 1` through `end`. A note released earlier jumps straight to `dca_end` and runs that stage alone.

## Open questions

- The code to loudness law is unmeasured. Settling it needs a hardware recording of a known envelope against the emulator's sampled output code.
- The 125 Hz figure is measured in an emulator running the ROM, not against hardware.
