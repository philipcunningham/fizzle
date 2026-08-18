---
type: map
title: Truth map
description: Per topic, where truth lives across the Casio spec, fizzle code, firmware findings, and the corpus.
tags: [routing]
updated: 2026-08-18
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md
---

# Truth map

Spec sections refer to
[the Casio spec](sources/casio-spec.md); firmware citations are ROM
addresses (see [firmware](sources/firmware.md)). Read the linked wiki
page where one exists; otherwise the spec section and code are the
authorities.

| Topic | Spec | fizzle code | Wiki |
|---|---|---|---|
| Disk geometry, head, CAT | 1-1, 1-2 | `pkg/disk`, `pkg/diskformat` | |
| Directory, file types | 1-3 | `pkg/disk`, `pkg/disklist` | |
| DIS / file head | 1-4 | `pkg/disk`, `pkg/diskget`, `pkg/diskadd` | [dis-file-head](findings/dis-file-head.md) |
| Multi-disk full dumps | 1-3 | `pkg/disk/disk.go`, `pkg/diskadd`, `pkg/fzfinfo` | [multi-disk-dumps](topics/multi-disk-dumps.md) |
| Bank sector (`bankdata`) | 2-2 List B | `pkg/fzfinfo`, `pkg/voicebuild` | [mchn-offset](findings/mchn-offset.md), [bstep-key-splits](findings/bstep-key-splits.md), [multiple-bank-sectors](findings/multiple-bank-sectors.md) |
| Voice parameter area | 1-5, 2-1 | `pkg/fzutil` (`CountAllVoices`, `InferVoiceCount`), `pkg/voiceextract`, `pkg/voiceunpack` | [voice-area-sizing](topics/voice-area-sizing.md) |
| Audio area, sample rates | 1-5 | `pkg/disk/rates.go`, `pkg/wav`, `pkg/voiceimport` | [audio-block-padding](findings/audio-block-padding.md) |
| Voice header (`voicedata`) | 2-1 List A | `pkg/disk/voice.go`, `pkg/fzvinfo`, `pkg/voiceedit` | [dcq-full-byte](findings/dcq-full-byte.md) |
| Loops, playback modes | 2-1 | `pkg/voiceimport`, `pkg/sfzconvert` | [voice-authoring-defaults](topics/voice-authoring-defaults.md) |
| DCA / DCF envelopes | 2-1 | `pkg/voiceedit`, `pkg/disk/voice.go` | [envelope-timing](topics/envelope-timing.md) |
| Velocity response | 2-1 | `pkg/voiceimport`, `pkg/sfzconvert` | [vel-dca-kf](findings/vel-dca-kf.md) |
| Outputs, polyphony, mute groups | 2-2 (`gchn`) | `pkg/sfzconvert`, `pkg/sfzexport` | [gchn-polyphony](findings/gchn-polyphony.md) |
| Effect data (`effectdata`) | 2-3 List C | `pkg/fzfeffects` | [voice-authoring-defaults](topics/voice-authoring-defaults.md) |
| MIDI, Area mode | 2-2, 3 | `pkg/fzfmidi`, `pkg/fzfoutput` | |
| Front-panel display scales | (undocumented) | `pkg/disk/voice.go` (`KFByteToDisplay`) | [display-scales](topics/display-scales.md) |
| Program files (Type 5) | 4 | `pkg/diskadd`, `testdata/assembly/` | [corpus](sources/corpus.md) |
| SFZ conversion | (not in spec) | `pkg/sfz`, `pkg/sfzconvert`, `pkg/sfzexport` | |
| File-type detection | 2-1 (`name` at 0xb2) | `pkg/fzutil` | [voice-area-sizing](topics/voice-area-sizing.md) |

Repo layout cross-references are in [project](project.md). Every raw
source has an assessment page: [casio-spec](sources/casio-spec.md),
[firmware](sources/firmware.md), [corpus](sources/corpus.md),
[buchty-fztoolkit](sources/buchty-fztoolkit.md),
[vosmaer-fz1](sources/vosmaer-fz1.md), and
[undecyclenate-editor](sources/undecyclenate-editor.md).
