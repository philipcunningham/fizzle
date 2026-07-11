---
type: map
title: Project outline
description: Where the wiki links into the repo tree; the directory listing lives in the root AGENTS.md.
tags: [routing, repo]
updated: 2026-07-11
sources:
  - AGENTS.md
---

# Project outline

fizzle is a Go CLI (plus a Bubble Tea TUI) for loading samples onto
Casio FZ series samplers via floppy disk images. The authoritative
directory-by-directory listing is the Project structure section of the
root `AGENTS.md`; maintaining a second copy here would drift. This
page carries only the wiki's cross-references into the tree:

- `pkg/fzutil/` voice counting and file-type detection are explained
  in [voice-area-sizing](topics/voice-area-sizing.md).
- `pkg/studio/widgets/envelopevisual/` embeds the verified firmware
  rate table; see [envelope-timing](topics/envelope-timing.md).
- `pkg/disk/voice.go` implements the front-panel value mappings; see
  [display-scales](topics/display-scales.md).
- `testdata/` is the fixture corpus, the wiki's strongest evidence,
  including the Type 5 assembly demo; see
  [corpus](sources/corpus.md).
- `llm-wiki/sources/` holds the verbatim Casio spec (md and pdf)
  beside the source pages.
