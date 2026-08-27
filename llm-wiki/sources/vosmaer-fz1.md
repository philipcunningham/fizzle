---
type: source
title: Vosmaer fz1
description: Jacob Vosmaer's 2025 FZ-1 disk-image utilities and write-up; corrected the file-head layout numbers.
tags: [community, external]
updated: 2026-08-27
sources:
  - https://github.com/jacobvosmaer/fz1
  - https://blog.jacobvosmaer.nl/0057-fz-1-images/
status: suspect
---

# Vosmaer fz1

Jacob Vosmaer's fz1 project provides small C utilities for building FZ-1 disk images and a format article. It is community documentation and ranks below firmware and hardware evidence.

It independently corroborates the file-head counts at the sector end; see [dis-file-head](../findings/dis-file-head.md). Its 12-byte ASCII voice-name scan also provides a cross-check for fizzle's stricter standalone voice-slot walk; see [voice-area-sizing](../topics/voice-area-sizing.md).
