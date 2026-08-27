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

The recorded corpus comes from the Casio FZ sampler archive at zine.r-massive.com. It covers FL-1 through FL-14, FL-A, FL-B, CASIO001 through CASIO142, and the Soundwaves library.

Nineteen shareware files use an `.FZF` extension but contain single voices. Import renames those files with an `.FZV` extension according to `testdata/corpus/README.md`.

Corpus claims record observations from 235 full dumps across those collections. They remain hardware evidence, but clean checkouts can't reproduce them without those fixtures.

Corpus-dependent tests skip missing fixtures. `testdata/synthetic/` contains generated round-trip and QA fixtures, while `testdata/assembly/` contains the Type 5 program fixture.
