---
type: source
title: Fixture corpus
description: Tracked real-hardware fixtures used for regression tests and corpus observations.
tags: [corpus, primary-source, testdata]
updated: 2026-08-27
sources:
  - testdata/corpus/README.md
status: confirmed-hardware
---

# Fixture corpus

The repository tracks the authorized hardware corpus under `testdata/corpus/`. Its layout and provenance are recorded in the adjacent README and manifest.

The recorded corpus comes from the Casio FZ sampler archive at zine.r-massive.com. It covers FL-1 through FL-14, FL-A, FL-B, CASIO001 through CASIO142, and the Soundwaves library.

Nineteen shareware files use an `.FZF` extension but contain single voices. Import renames those files with an `.FZV` extension according to `testdata/corpus/README.md`.

Corpus claims record observations from 235 full dumps across those collections. A clean checkout can reproduce them from the tracked fixtures.

Corpus tests cover parsing, snapshots, layouts, document invariants, and mutations. `testdata/synthetic/` contains generated round-trip and QA fixtures.
