---
type: source
title: Buchty fztoolkit
description: Rainer Buchty's 2000 C utilities for FZ-1 disks on a Linux floppy drive; a useful struct cross-check.
tags: [community, external]
updated: 2026-07-11
sources:
  - http://www.buchty.net/casio/
status: suspect
---

# Buchty fztoolkit

Rainer Buchty's fztoolkit (2000, http://www.buchty.net/casio/) is a set
of C utilities that read and write FZ-1 disks directly from a Linux
floppy drive. Community documentation, the lowest-ranked evidence.

Useful for: the `voice_data` and `bank_data` struct layouts as a
cross-check against the spec, and firmware notes that informed the V50
ROM API comments in `testdata/assembly/DEMO.asm`. Buchty also published
notes on the LCD controller and the GAA/GAB/GAX sound chips.

Trust accordingly: independent reimplementation, not hardware
measurement; where it disagrees with the corpus or firmware, those win.
