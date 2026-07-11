---
type: source
title: Undecyclenate FZ Editor and Librarian
description: A Windows XP FZ editor whose manual documents front-panel semantics and UI-side behaviour.
tags: [community, external]
updated: 2026-07-11
sources:
  - https://undecyclenate.neocities.org/manual
status: suspect
---

# Undecyclenate FZ Editor and Librarian

A Windows XP editor and librarian for FZ samplers; prior art for
fizzle, and its manual structured `docs/fizzle-manual.md`. Community
documentation, the lowest-ranked evidence.

Useful for: front-panel menu locations per parameter, display-range
notes (editor exposes 0 to 127 and 0 to 255 where the FZ shows 0 to
99), and behavioural observations worth verifying: synthesized loop
mode plays 6 semitones flat (correctable by tuning the root note the
other way), and the LFO resonance and attack fields produced no
audible effect in their testing.

## Open questions

- Verify the synthesized-mode -6 semitone offset on hardware; if
  real, `pkg/sfzconvert` may need to compensate when emitting mode
  0x0013.
- Verify whether `lfo_atck` and `lfo_dcq` are audible on hardware; the
  manual reports no effect.
