---
type: source
title: Buchty fztoolkit
description: Rainer Buchty's 2000 C utilities for FZ-1 disks on a Linux floppy drive; a useful struct cross-check.
tags: [community, external]
updated: 2026-08-27
sources:
  - http://www.buchty.net/casio/
status: suspect
---

# Buchty fztoolkit

Rainer Buchty's fztoolkit is a set of C utilities that access FZ-1 disks through a Linux floppy drive. It is community documentation and ranks below firmware and hardware evidence.

Its `voice_data` and `bank_data` structures provide an independent specification cross-check. Its firmware notes corroborate V50 ROM API comments in `testdata/assembly/DEMO.asm`. Buchty also documents the LCD controller and GAA, GAB, and GAX sound chips.

Trust accordingly: independent reimplementation, not hardware measurement; where it disagrees with the corpus or firmware, those win.
