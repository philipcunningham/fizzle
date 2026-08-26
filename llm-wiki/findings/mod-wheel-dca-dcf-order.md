---
type: finding
title: The mod wheel's amp and filter offsets are the reverse of the spec
description: The spec orders the mod row filter then amp at 0x07 and 0x08; an FZ-1 reads them amp then filter, like the other two controllers.
tags: [fzf, bank, effects, dca, dcf]
updated: 2026-08-26
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-3
  - effectdata_edit_screen at F000:6B98
status: confirmed-hardware
---

# The mod wheel's amp and filter offsets are the reverse of the spec

## Claim

In the effect block, byte 0x07 is the mod wheel's amp offset and byte 0x08 is its filter offset. The spec has the two the other way round.

## Spec disagreement

The effect block holds a 3 by 7 matrix: three controllers, each with four LFO depths and three offsets. List C in section 2-3 orders the offsets amp, filter, then resonance for the foot pedal at 0x0e, 0x0f, and 0x10. It repeats that order for aftertouch at 0x15, 0x16, and 0x17. The mod row alone it orders filter then amp, at 0x07 and 0x08. The page 25 scan carries the same order as the transcription, so the reversal is the document's own and not a transcription slip.

## Hardware evidence

On a bank saved from an FZ-1, the MOD WHEEL page reads `DCA LEVEL = 000` and `DCF LEVEL = 127`. The saved effect block holds 0 at 0x07 and 127 at 0x08. The panel names the byte at 0x08 as the filter offset.

## Firmware evidence

`effectdata_edit_screen` at `F000:6B98` recovers a controller from an offset by subtracting 3 and dividing by 7, at `F000:6BF5`. The three controllers are three uniform groups of seven, so the mod row can't carry an order of its own. The same screen skips group index 3 at `F000:6C23`, which is why the panel page lists five rows and omits LFO res and DCQ.

## Executed firmware

Booting the ROM under the emulator settles the geometry. Writing one depth byte in `pare` at `0x5288` and sending its controller change scales that depth into `nowe` at `0x52A0`, at the same index within the group. Byte 0x07 lands on group index 4, where `fot_dca` at 0x0e also lands. Byte 0x08 lands on index 5, where `fot_dcf` at 0x0f lands. The foot row's labels aren't in dispute, so the mod row's two follow from them.

## Corpus evidence

Every `.fzf` under `testdata/corpus/` was read at bank offset 0x3c0, 112 files in all. Byte 0x07 is never non-zero. Byte 0x08 is set in eight: `Choir.fzf` at 50, `Ahhs.fzf` at 40, `Ahhs-Oohs.fzf` at 60, `Gothic-Voices.fzf` at 90, `Ghostly-Voices.fzf` at 40, `Tremolo-Strings.fzf` at 50, `Symphony-Strings-Marcato.fzf` at 35, and `Orch-Hits-Modern-Hit.fzf` at 25. A mod wheel opening the filter suits every one of them. A mod wheel driving amplitude on a choir would be an odd patch to ship eight times and never once the other way.

## What fizzle implements

`EffectModDCAOffset` is 0x07 and `EffectModDCFOffset` is 0x08 in `pkg/disk/voice.go`. `pkg/fzfeffects` reads and writes through both, and `pkg/webcore` projects them into the browser's matrix as the DCA and DCF columns. `TestParseModWheelMatchesHardwareBytes` and `TestSetModWheelWritesHardwareOffsets` pin the read and write sides against raw offsets, and `TestModWheelMatrixColumnsFollowHardware` pins the browser column.

## Compatibility

No stored bytes were ever wrong. Reads and writes used the same offset, so files round-tripped correctly and only the two labels were transposed. A corrected build re-reads a file written by an older one and shows the machine's values. Anyone who set the filter offset on an older build set what the machine calls DCA LEVEL. That value still sits at 0x07.
