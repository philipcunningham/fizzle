# Synthetic test fixtures

Tracked fixtures make parser, mutation, conversion, and browser tests reproducible. Treat images as immutable unless the expected bytes intentionally change.

`../layout-manifest.json` records each image's checksum, evidence, tags, voice count, and resolved layout. Tests reject unlisted images.

| File | Purpose |
|---|---|
| `HOOVER.img` | Single voice disk operations |
| `STAB.img` | Single voice extraction |
| `BRASS.img` | Multi-voice parsing, unpacking, and editing |
| `TECHNO.img` | Multi-bank parsing and editing |
| `PAD-LFO.img` | LFO behavior and byte-preserving round trips |
| `PREY.img` | Deleted directory entries, unassigned voices, and DIS authority |
| `LOOPDEMO.img` | Loop-chain behavior and browser smoke tests |

## Loop demonstration

`LOOPDEMO.img` contains five voices built from one sample and loop table. Their different loop caps make each chain state audible.

| Voice | Sustain loop | End loop | Expected behavior |
|---|---:|---:|---|
| `1 LOW HIGH` | 0 | 2 | Repeats low while held, then high after release |
| `2 MID BOTH` | 1 | 1 | Repeats the middle loop throughout |
| `3 HIGH ONLY` | none | 2 | Repeats the high loop from note on |
| `4 LOW ONLY` | 0 | none | Repeats low while held, then plays the tail |
| `5 NO LOOP` | none | none | Plays once |

`pkg/webcore/loopdemo_test.go` pins this table to the image.

## SFZ conversion

`JUNGLISM.sfz` references 28 WAV files and exercises resampling, packing, split disks, and SFZ round trips. Selected WAVs also seed parser fuzz tests.
