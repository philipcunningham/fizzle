---
type: source
title: Fixture corpus
description: Optional real-hardware fixtures used to reproduce the wiki's recorded corpus observations.
tags: [corpus, primary-source, testdata]
updated: 2026-08-27
sources:
  - testdata/corpus/README.md
status: confirmed-hardware
---

# Fixture corpus

The repository doesn't store, download, or package the hardware corpus. An authorized local corpus can be placed under `testdata/corpus/` using the README's layout and manifest.

Corpus claims record observations from 235 full dumps from the FZ factory, shareware, and Soundwaves libraries. They remain hardware evidence, but clean checkouts can't reproduce them without those fixtures.

Corpus-dependent tests skip missing fixtures. `testdata/synthetic/` contains generated round-trip and QA fixtures, while `testdata/assembly/` contains the Type 5 program fixture.
