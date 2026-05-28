# Synthetic test fixtures

Hand-built test fixtures used by `fizzle`'s own test suite, exercising
specific code paths under deterministic, version-controlled conditions.

## Disk images

Each `.img` file is a 1.25 MB Casio FZ-1 floppy image built with `fizzle`
itself, holding one or more files the test suite expects to read back.
They are committed (not regenerated) so test runs are reproducible and so
the on-disk layout itself is part of what's under test.

| File          | Contains          | Used by                                                                   |
|---------------|-------------------|---------------------------------------------------------------------------|
| `HOOVER.img`  | One FZV voice     | Disk listing, get/copy, voice-extract, studio browser, integration CLI    |
| `STAB.img`    | One FZV voice     | Disk listing, voice extract, integration CLI                              |
| `BRASS.img`   | Full data dump    | FZF parse and unpack, voice-edit, studio scenarios                        |
| `TECHNO.img`  | Full data dump    | FZF parse, voice-edit fixtures, fzfinfo real-hardware regression          |
| `PAD-LFO.img` | Full data dump    | LFO-specific voice-edit checks, SHA-256 pinned for byte-exact round-trip  |

The `extractTestFZF` and `fixtureImg` helpers in the test packages route
through these images. Treat them as immutable: regenerating loses the
real-hardware bit-for-bit guarantees that some tests assert.

## SFZ round-trip fixture

`JUNGLISM.sfz` plus the `JUNGLISM Samples/` directory (28 WAV files) are
the SFZ round-trip fixture used by `pkg/sfzconvert` and `pkg/integration`
to exercise the WAV-to-FZF conversion pipeline end to end. The samples are
realistic in length and content (drum hits, basses, pads), so the test
covers resampling, fit-to-disk packing, and the multi-disk split path with
something representative.

A handful of these WAVs (`reese.wav`, `amen 01.wav`, `808.wav`, `pad 1.wav`)
also seed `pkg/wav`'s fuzz tests so the WAV reader is exercised against
real-world headers, not just synthetic ones.
