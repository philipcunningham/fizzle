# llm-wiki log

Append only, newest last. Entry format: [AGENTS.md](AGENTS.md).

## [2026-07-11] scaffold | llm-wiki bootstrapped

Added the schema, `index.md`, `map.md`, `project.md`, 8 findings pages,
5 topic pages, and 6 source pages, distilled from
`docs/casio-fz1-format.md` (deleted, citations repointed). Moved the
Casio spec (md and pdf) verbatim into `llm-wiki/sources/`.

## [2026-08-18] ingest | studio TUI removal

fizzle removed the studio TUI; `pkg/studio/container` and
`pkg/studio/model` moved to `pkg/container` and `pkg/model`. Repointed
code anchors in map.md, project.md, firmware.md, corpus.md,
display-scales.md, and envelope-timing.md. The verified rate-table Go
copy now lives only in git history; the ROM at F000:0490 is the source.

## [2026-08-20] ingest | Sample memory per machine

An FZ-1 owner reported `NO MEMORY SPACE` on a 1,188,000 byte
instrument, which the wiki's 2 MB figure couldn't explain. Added
[sample-memory](topics/sample-memory.md). The FZ-1 shipped with 1 MB
and the rack units with 2 MB, and the probe at `F000:07D4` discovers
which at power on. Corrected multi-disk-dumps.md, which derived the two
disk cap from a series wide 2 MB; the cap comes from the `disknum` bit.
One open question logged: what a load does to resident memory.

## [2026-08-20] ingest | Envelope timing, corrected and extended

The page's tick figure was wrong by a factor of three. It derived 25 ms
from a stale 6.4 ms IRQ estimate. The chain is a 4 kHz IRQ, a 500 Hz
voice service, and 125 Hz per DCA stage, so a stage steps every 8 ms.
Added the slew limit, the instant rate at panel 99, and the level to
code mapping through the table at F000:0590. Added the note on scaling
and the release rules. The code to loudness law stays open.


## [2026-08-20] ingest | Envelope timing, the stage walk and the scaling arithmetic

Reverse engineering settled two things the page carried loosely.

The stage walk: note on caps the run at the lower of the sustain and
end stages. A voice whose sustain sits at or past its end therefore
never holds, and terminates with the key still down. Note off compares
the counter with the sustain stage. A note released mid stage skips to
the end stage; a parked one runs the stages between.

The note on scaling now carries each term's exact arithmetic, its ROM
address, and the shift kind. Added the `bvol` subtraction on stops.
Corrected the slew citation to the loop head at F000:0A49. Split the
level to code map at F000:214C from the composition at F000:20BD, and
recorded that velocity never reaches that routine.
