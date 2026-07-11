---
type: source
title: Vosmaer fz1
description: Jacob Vosmaer's 2025 FZ-1 disk-image utilities and write-up; corrected the file-head layout numbers.
tags: [community, external]
updated: 2026-07-11
sources:
  - https://github.com/jacobvosmaer/fz1
  - https://blog.jacobvosmaer.nl/0057-fz-1-images/
status: suspect
---

# Vosmaer fz1

Jacob Vosmaer's fz1 project (2025): small C utilities for building
FZ-1 disk images, with a blog write-up on the format. Community
documentation, the lowest-ranked evidence.

Useful for: the file-head layout-number correction (counts at the
sector end; see [dis-file-head](../findings/dis-file-head.md)) and the
heuristic for reconstructing layout numbers on FZF files found online
by scanning for 12-byte ASCII voice names, which informed fizzle's
voice-slot walk (see
[voice-area-sizing](../topics/voice-area-sizing.md)).
