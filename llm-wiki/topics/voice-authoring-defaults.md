---
type: topic
title: Voice authoring defaults
description: The loop, envelope, and effect values fizzle writes so a generated voice behaves like a hardware-native one.
tags: [fzv, authoring, loops, envelope, effects]
updated: 2026-08-23
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md sections 2-1, 2-3
  - FZ-10M hardware (defaults verified by loading generated disks)
status: confirmed-hardware
---

# Voice authoring defaults

What fizzle writes when building voices (`pkg/voiceimport`,
`pkg/voicebuild`, `pkg/sfzconvert`), verified by loading the results
on hardware.

**Playback window**: `genst = wavst`, `gened = waved` plays the full
sample. Loop modes: 0x0000 no sound, 0x01D7 normal, 0x101D reverse,
0x2014 cue, 0x0013 synthesized (an unverified pitch offset is logged
under [undecyclenate-editor](../sources/undecyclenate-editor.md)).

**Loops**: sustain loop means `loop_sus = 0`, `loop_end = 7`, loop 1
addresses in `loopst[0]`/`looped[0]`, all unused slots set to `gened`.
One-shot means `loop_sus = 8` and every slot at `gened`. Loop
addresses are cumulative word indices; WAV SMPL loop points scale by
`round(loop * target_rate / source_rate)`. Auxiliary fields per spec:
`loopxf` 0 to 1023 (0 = no crossfade) and `looptm` 1 to 1022 in 16 ms
units. `loopst` upper 8 bits are loop fine; `looped` MSB picks Skip
over Trace.

**DCA default**: stage 0 rises at rate 127 to stop 255; stages 1 to 7
fall at 0xC0 (falling, magnitude 64) to stop 0. Instant attack, full
sustain, moderate release.

**DCF default (no filtering)**: `dcf = 127` offset, `dcf_rate[0] =
127`, remaining rates 0, all stops 255. The offset biases the filter
above its audible range.

**Velocity**: `vel_dca_kf = 80`; see
[vel-dca-kf](../findings/vel-dca-kf.md).

**Effect defaults** (bank sector 0x3C0, 24 one-byte fields):

```
18 00 00 0f 00 00 00 00  00 00 00 00 00 00 40 00
00 08 00 00 00 00 00 00
```

`bend = 24`, `mod_lfp = 15`, `fot_dca = 64`, `aft_lfp = 8`; `mvol` and
`suss` are unused per spec ("normally 0 is placed"). `fzf effects`
surfaces the four representative fields; the other eighteen routing
fields survive round-trips untouched.
