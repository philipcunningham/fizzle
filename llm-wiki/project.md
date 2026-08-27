---
type: map
title: Project outline
description: Routes from the wiki into the current command, browser, format, and fixture packages.
tags: [routing, repo]
updated: 2026-08-27
sources:
  - AGENTS.md
---

# Project outline

fizzle is a Go CLI and browser application for editing Casio FZ disk images, full dumps, voices, and samples.

- `cmd/fizzle/` defines the CLI. `pkg/webcore/` and `web/` implement the browser application.
- `pkg/document/` owns canonical in-memory documents. `pkg/fzf/` exposes validated full-dump views and mutations.
- `pkg/disk/` implements disk structures and panel encodings. See [display-scales](topics/display-scales.md).
- `pkg/fzutil/` detects file types and voice counts. See [voice-area-sizing](topics/voice-area-sizing.md).
- `pkg/voicebuild/` assembles voices and multi-disk outputs. See [multi-disk-dumps](topics/multi-disk-dumps.md).
- `testdata/corpus/`, `testdata/synthetic/`, and `testdata/assembly/` contain tracked fixtures. See [corpus](sources/corpus.md).
- `llm-wiki/sources/` holds the verbatim Casio specification beside its assessment page.
