---
type: finding
title: vel_dca_kf must be non-zero for velocity response
description: With vel_dca_kf at 0 every note plays at identical volume; 80 matches real hardware samples.
tags: [fzv, velocity, authoring]
updated: 2026-07-11
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-1
status: confirmed-hardware
---

# vel_dca_kf must be non-zero for velocity response

`vel_dca_kf` (voice header 0xAA, signed) scales MIDI velocity into
amplitude. The spec documents the field but not the practical
consequence: at 0, velocity has no effect at all and every note plays
at identical volume. A value of 80 gives normal velocity response
matching real hardware samples.

fizzle writes 80 by default when building voices (`pkg/voiceimport`,
`pkg/sfzconvert`). Display-range calibration for the five velocity
sensitivity fields is still open; see
[display-scales](../topics/display-scales.md).
