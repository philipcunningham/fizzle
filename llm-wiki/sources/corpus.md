---
type: source
title: Fixture corpus
description: 235 real-hardware full dumps plus voice files under testdata/corpus; the evidence base for every statistical claim.
tags: [corpus, primary-source, testdata]
updated: 2026-08-18
sources:
  - testdata/corpus/README.md
status: confirmed-hardware
---

# Fixture corpus

`testdata/corpus/` holds real FZ-1 material downloaded from the Casio
FZ sampler archive at zine.r-massive.com. It contains the factory
library (FL-1 to FL-14 plus FL-A and FL-B). It also holds the
shareware set (CASIO001 to CASIO142) and the Soundwaves library. Nineteen shareware files shipped as `.FZF`
but are single voices and were renamed `.FZV` on import; see
`testdata/corpus/README.md` for the renaming rules.

The strongest evidence this wiki has: these bytes came from real
hardware and real-world distribution. Statistical claims count against
the 235
full dumps (for example `bstep` equals `vn` for 24 of them). Snapshot
tests in `pkg/integration/` pin `fzf info`, `fzv info`, and `disk ls`
output for every fixture, so corpus-derived claims are re-verified on
every test run.

`testdata/synthetic/` holds fizzle-generated images (round-trip and
QA fixtures); `testdata/assembly/` holds the Type 5 program demo.
