---
type: topic
title: Envelope timing
description: How the firmware turns 8-stage rate and stop bytes into wall-clock envelope times, what note on scales them by, and what the output stage does to the result.
tags: [fzv, envelope, dca, dcf, firmware]
updated: 2026-08-27
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
  - llm-wiki/sources/casio-service-manuals.md
  - FZ-1 ROM (rate table F000:0490; output table F000:0590; handlers F000:2039, F000:218B, F000:20BD; slew F000:0A49; service loop F000:1CD8; note on F000:12B4; note off F000:1512)
status: confirmed-firmware
---

# Envelope timing

DCA and DCF envelopes each have 8 stages. On note on the envelope runs from stage 0, advancing when each stage's level is reached. On note off it resumes and runs to `end`.

Rate bytes carry direction in bit 7, set meaning falling, and magnitude in bits 0 to 6. Stop levels are 0 to 255.

## The stepper

A 128-entry, 16-bit rate table sits at F000:0490 to F000:058F. Each update indexes it with `rate & 0x7F` and negates the value when bit 7 is set. It adds the result to a 16-bit accumulator whose high byte is the current level. When that high byte reaches the stage's stop, the accumulator snaps to `stop << 8` and the stage advances. The DCF handler reads the table at F000:218B; the DCA handler at F000:2039 has identical shape.

Updates per stage are `|level delta| * 256 / table[rate]`.

## The clock

Timer channel 0 is re-armed with 1000 counts at 4 MHz at F000:0A25, so IRQ0 is 4 kHz. The ISR adds 0x20 to a byte at [0x040A] and runs the slow chain only on carry. The per-voice service at F000:1CD8 therefore runs at 500 Hz. It dispatches one state per voice per call, and states 3 and 7 both point at the DCA handler. A DCA stage advances 125 times a second.

Seconds per stage are therefore `|level delta| * 2.048 / table[rate]`. A full sweep takes 0.387 s at panel 50, 2.30 s at panel 25, and 7.46 s at panel 12.

## The stage walk

A stage counter sits at `gene+0x15` and a cap at `gene+0x14`. The handler compares them at F000:203C and leaves the active path once the counter passes the cap. That comparison is the whole hold mechanism, and the frozen accumulator is what sustain means.

Note on sets the cap to `min(dca_sus, dca_end)` at F000:123B, and fills scaled stage copies for stages 0 to `dca_end` only (F000:12AC). A held key therefore runs stages 0 through `min(sus, end)` and parks one past it.

That gives three cases, and the third is the common one:

- `sus < end`: the counter parks inside `[sus + 1, end]`, and the note holds at `stop[sus]` until the key comes up.
- `sus == end` or `sus > end`: the counter parks past `dca_end`, so the tail test at F000:20AB fails and the voice terminates with the key still down. Sustain at or past end is how the format spells a one shot, and `dca_sus` carries no meaning in that case.

Termination writes the sentinel 0x00DF to the voice's output code (F000:20B6). The slew walks the code down to it and F000:22EE frees the slot, writing 0xFF occupancy at F000:2301.

Note on clamps an effective rate into 1 to 0x7F at F000:12E9, so a stored rate of 0 never stalls a stage.

## Release

Note off at F000:1512 sets the cap to `dca_end`. It then compares the counter with `dca_sus` at F000:1525. A counter past the sustain stage is the parked state, and it keeps its place, so stages `sus + 1` through `end` run. A counter that has not passed it is forced to `dca_end`, so the end stage runs alone and the stages between are skipped. That covers every stage still ramping, the sustain stage included.

Note on forces the stage named by `dca_end` falling to a stop of zero (F000:1351), whatever the file stores. A release therefore always ends in silence. The stages before it keep their own stops, so a release can rise before it falls.

## The slew, and the cliff at panel 99

The envelope's own timing isn't the whole story. The slew loop at F000:0A49 moves each voice's output code one unit toward its target on every 4 kHz tick. It has one arm per direction, at F000:0A5D and F000:0A60. No segment therefore crosses the 224 to 895 code range faster than 168 ms. Anything nominally faster is slew limited, which is where the FZ's characteristic softness comes from.

One case escapes it. A rate magnitude of 0x7F, which the panel shows as 99, writes both port bytes directly at F000:2094. It sets the old code equal to the new one, so the slew has nothing to do. Panel 99 is an instant jump where panel 98 takes 168 ms.

## Level to the wire

One routine spans F000:20BD to F000:2172. Its first half composes the level with the LFO, the modulation sources, and per-channel main volume. Its tail at F000:214C maps the result to the code the output stage carries. Up to 0x9F the code is the level plus 0xE0, giving 224 to 383. Above it, a 96-entry table at F000:0590 expands, its slope rising from 1 to about 16, giving 384 to 895. The value 223 is the sentinel described above.

## The amplifier the code drives

The DCA is an analog amplifier inside the filter chip, not a digital multiply. The FZ-20M service manual names it MB87186 and gives it two DCF and two DCA sections. The FZ-1 parts list carries four of the same part as FM-1, two channels each for eight voices. Sample data reaches it through two 16-bit converters (PCM54HP), fed alternately by gate array GAX, so the envelope never touches the samples.

The manual's block diagram gives the DCA range as 0 to -87.75 dB. Its pin table splits the control across two writes. With F/A low and FC/Q high the upper 2 bits are gain data; with both low the 8 bits are the amplitude value. Port 0x90 + 2i carries the gain word and 0x80 + 2i the amplitude word (F000:2094, F000:0A65, F000:1CCA, F000:2324, F000:2646).

The firmware drives the pair as one monotone 16-bit number rather than as a coarse and a fine control. It steps the combined value by one on the slew and by four on the fade at F000:2330. Both cross the boundaries between the two registers with no special handling, and F000:0A55 orders codes by unsigned compare. The high byte is never above 3.

That fade also settles the law's shape. It ramps down only to 0x017C, about a quarter of the way up the range, then cuts to zero. Under a law linear in amplitude that cut lands near half scale and clicks, so the code is dB-like.

A code step is therefore 87.75 dB spread over the 10-bit word, or 0.0858 dB. That figure is inference from the manual's range rather than a sourced per-step table. It is the one number here nobody has confirmed against the chip.

Velocity never reaches this routine. Its only route into the amplitude is the note on scaling below, which arrives here as the envelope accumulator.

## Note on scaling

Note on applies velocity and key scaling once, writing per-voice copies of every rate and every stop (F000:12B4 to F000:135E). Velocity enters as `min(velocity + 0x10, 0x7F)` at F000:125A, saturating on signed overflow. Key distance is `key - cent`, read as a signed byte. That `cent` is the root key: the bank's copy at `bankdata+0x102` during normal play, or the voice's own at `voicedata+0xB0` when a voice is auditioned standalone.

Every shift below is arithmetic, so each floors rather than truncating. There is no division in the block.

| Term | Arithmetic | Address |
| --- | --- | --- |
| Key follow on a stop | `((key - cent) * dca_kf) >> 4` | F000:1314 |
| Key follow on a rate | `((key - cent) * dca_rs) >> 7` | F000:12C3 |
| Velocity on a stop | `t = ((vel * vel_dca_kf * 2) >> 8); if (vel_dca_kf >= 0) t -= vel_dca_kf; term = t << 1` | F000:131D |
| Velocity on a rate | `t = ((vel * vel_dca_rs * 2) >> 8) + 1; if (vel_dca_rs >= 0) t -= vel_dca_rs; term = t >> 1` | F000:12CD |

Both velocity terms are no-ops at a sensitivity of zero. The rate term's `+ 1` also makes a full press exact, so a full press runs the stored rate. The stop term carries no such correction, so a full press lands two below the stored stop. A negative sensitivity skips the subtraction, which inverts the curve. The softer press is then the louder one, or the faster one.

Stops take one further term, a subtraction of the bank's per-voice output level `bvol` at `bankdata+0x1C2` (F000:133C). It is forced to zero on the edit-voice path.

Rates are then clamped to 1 to 0x7F and stops to 0 to 255, both signed and saturating, after every term is in. The rate's direction bit is split off before the scaling and reattached after the clamp, so it never takes part in either.

## Open questions

- The dB per code step is inferred from the chip's total range, not read from a control table. Settling it needs the MB87186 gain table or a recording across the code range. One competing law fits similar chips. The two gain bits could select four coarse ranges, with the eight bit word linear inside each. The firmware's stepping argues against it. Its slew moves the combined word by one, while its fade moves by four across the register boundary. Those changes sound even only if the steps are even in dB. A mantissa and exponent pair would jump at every boundary.
- Spreading the chip's range evenly is what the figure above assumes. It puts 46 dB between a soft press and a hard one on a voice with ordinary velocity sensitivity. Such a span is wide for an instrument, which is the strongest hint that the even spread is wrong somewhere.
- The tie from F/A and FC/Q to the port address bits is inferred from which registers the firmware writes where. A board schematic would confirm it.
- A rate byte carries its direction in bit 7, and note on rewrites the end stage falling. Whether note on rewrites other stage directions is unrecorded. Hardware behavior is unknown when a stored direction disagrees with its stop. A stage could run away from its stop rather than toward it. Real dumps can store either.
- The 125 Hz figure is measured in an emulator running the ROM, not against hardware.
