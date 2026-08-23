# llm-wiki index

One line per page, grouped by type. Read this first on every query.
Conventions: [AGENTS.md](AGENTS.md).

## Map

- [Truth map](map.md): per topic, where truth lives across the spec,
  fizzle code, firmware findings, and the corpus.
- [Project outline](project.md): where the wiki links into the repo
  tree; the directory listing lives in the root AGENTS.md.

## Findings

- [DIS / file head deviates from the spec in three ways](findings/dis-file-head.md):
  extent area to 0x3F9, counts at the sector end in bn vn wn order,
  DIS first in its chain.
- [Deleted directory entries leave blank slots in place](findings/directory-blank-slots.md): the firmware zeroes the first name byte and saves later files behind the gap; whether it reads past one is open.
- [Full dumps carry up to 8 bank sectors](findings/multiple-bank-sectors.md):
  the spec shows one; the voice area follows the last.
- [Audio blocks are sector-padded but waved stores the unpadded count](findings/audio-block-padding.md):
  read each start from wavst, never from waved deltas.
- [The mchn array sits at 0x142, not 0x104](findings/mchn-offset.md):
  writing at 0x104 corrupts the cent array and wrecks pitch.
- [vel_dca_kf must be non-zero for velocity response](findings/vel-dca-kf.md):
  zero kills velocity response; 80 matches hardware.
- [gchn is an output bitmask that controls polyphony and mute groups](findings/gchn-polyphony.md):
  maps to SFZ mutegroup.
- [dcq resonance uses the full byte, not the upper nibble](findings/dcq-full-byte.md):
  the spec's 4-bit claim is wrong on hardware.
- [bstep counts key splits per bank, not file voices](findings/bstep-key-splits.md):
  it equals vn for only 24 of 235 dumps.
- [looptm is a duration in 16 ms units](findings/looptm-unit.md): the
  ROM's own timer settles it, and the 1024 the corpus writes on end
  loops is never read.

## Topics

- [Sample memory per machine](topics/sample-memory.md): the FZ-1
  shipped with 1 MB and the rack units with 2 MB, and the firmware
  discovers which at power on.
- [Multi-disk full dumps](topics/multi-disk-dumps.md): disk 1 carries
  all metadata, disk 2 is pure audio; FULL-DATA-FZ naming.
- [Voice-area sizing](topics/voice-area-sizing.md): the DIS tail's vn is the authority; the bounded slot walk is only for standalone files, and both of its bounds fail on firmware-authored dumps.
- [Multi-loop playback](topics/multi-loops.md): the eight loops play
  as a chain in numerical order, capped by one byte that makes both
  the sustain hold and the end loop.
- [Envelope timing](topics/envelope-timing.md): the rate table, the
  125 Hz stepper, the output slew, the stage walk, and the note on
  scaling behind the 8-stage envelopes.
- [Front-panel display scales](topics/display-scales.md): raw bytes to
  the front panel's 0 to 99 and -15 to +15 displays.
- [Voice authoring defaults](topics/voice-authoring-defaults.md): the
  loop, envelope, and effect values fizzle writes for hardware-native
  behaviour.

## Sources

- [Casio FZ-1 Data Structures spec](sources/casio-spec.md): the 1987
  primary spec; reading conventions and known errors.
- [Casio service manuals and books](sources/casio-service-manuals.md): the
  hardware documents behind the DCA chip, the playback path, and the
  fitted memory.
- [FZ-1 firmware reverse engineering](sources/firmware.md): ROM
  address anchors; outranks the spec.
- [Fixture corpus](sources/corpus.md): 235 real full dumps under
  testdata; the statistical evidence base.
- [Buchty fztoolkit](sources/buchty-fztoolkit.md): 2000 C utilities;
  struct cross-check.
- [Vosmaer fz1](sources/vosmaer-fz1.md): 2025 utilities and write-up;
  file-head correction, name-scan heuristic.
- [Undecyclenate FZ Editor and Librarian](sources/undecyclenate-editor.md):
  Windows XP editor manual; front-panel semantics, open behavioural
  questions.
