# Synthetic test fixtures

Hand-built test fixtures used by `fizzle`'s own test suite, exercising
specific code paths under deterministic, version-controlled conditions.

## Disk images

Each `.img` file is a 1.25 MB Casio FZ-1 floppy image built with `fizzle`
itself. Each image holds one or more files the test suite expects to
read back.
They are committed (not regenerated) so test runs are reproducible and so
the on-disk layout itself is part of what's under test.

| File           | Contains       | Used by                                                                  |
|----------------|----------------|--------------------------------------------------------------------------|
| `HOOVER.img`   | One FZV voice  | Disk listing, get/copy, voice-extract, integration CLI                   |
| `STAB.img`     | One FZV voice  | Disk listing, voice extract, integration CLI                             |
| `BRASS.img`    | Full data dump | FZF parse and unpack, voice-edit, QA scenarios                           |
| `TECHNO.img`   | Full data dump | FZF parse, voice-edit fixtures, fzfinfo real-hardware regression         |
| `PAD-LFO.img`  | Full data dump | LFO-specific voice-edit checks, SHA-256 pinned for byte-exact round-trip |
| `LOOPDEMO.img` | Full data dump | The loop chain: webcore's loopdemo tests, and the browser smoke          |

The `extractTestFZF` and `fixtureImg` helpers in the test packages route
through these images. Treat them as immutable: regenerating loses the
real-hardware bit-for-bit guarantees that some tests assert.

## The loop chain fixture

`LOOPDEMO.img` is built rather than sampled, and it exists to make the
loop chain audible. Five voices carry one 3.4 second sample at 18 kHz
and one loop table. They differ only in which loops they name. What
changes between them is the cap rule and nothing else.

The sample runs in five parts: a bright tick, then three one second
windows, then a falling tail. Loop 1 is a 180 Hz sine, loop 2 a 360 Hz
sawtooth, and loop 3 a 720 Hz pulsing tone. Which window is repeating
is obvious by ear. Each window holds a whole number of cycles, which
keeps the seam clean. Every voice but the last carries
a DCA release of about two and a half seconds. An end loop is inaudible
on a voice that stops when the key comes up.

| Voice         | `loop_sus` | `loop_end` | What it demonstrates                            |
|---------------|------------|------------|-------------------------------------------------|
| `1 LOW HIGH`  | 0          | 2          | The cap moves at the key: low held, high freed  |
| `2 MID BOTH`  | 1          | 1          | One loop in both roles, so nothing moves        |
| `3 HIGH ONLY` | none       | 2          | The cap is the lower of the two, from note on   |
| `4 LOW ONLY`  | 0          | none       | A sustain loop that repeats past the key        |
| `5 NO LOOP`   | none       | none       | A voice that plays through once                 |

`pkg/webcore/loopdemo_test.go` holds the image to that table, so a
fixture that drifts fails rather than quietly demonstrating nothing.

## SFZ round-trip fixture

`JUNGLISM.sfz` plus the `JUNGLISM Samples/` directory (28 WAV files) are
the SFZ round-trip fixture used by `pkg/sfzconvert` and `pkg/integration`.
The fixture exercises the WAV-to-FZF conversion pipeline end to end. The
samples are realistic in length and content (drum hits, basses, pads). The
test therefore covers resampling, fit-to-disk packing, and the multi-disk
split path with something representative.

A handful of these WAVs (`reese.wav`, `amen 01.wav`, `808.wav`, `pad 1.wav`)
also seed `pkg/wav`'s fuzz tests so the WAV reader is exercised against
real-world headers, not just synthetic ones.
