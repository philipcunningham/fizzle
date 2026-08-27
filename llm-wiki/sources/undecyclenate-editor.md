---
type: source
title: Undecyclenate FZ Editor and Librarian
description: A Windows XP FZ editor whose manual documents front-panel semantics and UI-side behaviour.
tags: [community, external]
updated: 2026-08-27
sources:
  - https://undecyclenate.neocities.org/manual
status: suspect
---

# Undecyclenate FZ Editor and Librarian

A Windows XP editor and librarian for FZ samplers. Its manual is community documentation and ranks below the specification, firmware, and hardware evidence.

Useful for: front-panel menu locations per parameter, display-range notes, and behavioural observations worth verifying. The editor exposes raw 0 to 127 and 0 to 255 ranges where the FZ shows 0 to 99. Synthesized loop mode plays 6 semitones flat (correctable by tuning the root note the other way). The LFO resonance and attack fields produced no audible effect in their testing.

## Open questions

- Verify the synthesized-mode -6 semitone offset on hardware; if real, `pkg/sfzconvert` may need to compensate when emitting mode 0x0013.
- Verify whether `lfo_atck` and `lfo_dcq` are audible on hardware; the manual reports no effect.
