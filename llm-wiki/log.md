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

## [2026-08-20] ingest | Envelope timing, the stage walk

Note on caps the run at the lower of the sustain and end stages. A
sustain at or past the end never holds, and the voice terminates with
the key still down. Note off skips a mid stage release to the end
stage, where a parked note runs the stages between.

## [2026-08-20] ingest | Envelope timing, the note on scaling

Each scaling term now carries its arithmetic, its ROM address, and its
shift kind, plus the `bvol` subtraction on stops. Corrected the slew
citation to F000:0A49. Split the level to code map at F000:214C from
the composition at F000:20BD, and recorded that velocity never reaches
that routine.

## [2026-08-20] ingest | The DCA is an analog amplifier, and its law is dB

Service manuals settled the open question the page carried. The DCA
sits inside the filter chip, downstream of two converters, and covers
0 to -87.75 dB. Its gain word and amplitude word are driven as one
monotone number, which the slew and the fade prove. The per step
figure and the select pin tie stay open.

## [2026-08-20] ingest | Service manuals, and what they don't settle

New source page for the manuals and books. The manuals establish the
DCA part and its control words. The books corroborate the one shot
envelope, the forced end stage, and the instant rate. Three open
questions added to envelope timing: the competing law, the wide
velocity span, and the stored stage direction.

## [2026-08-21] ingest | Multi-loop playback

The eight loops play as a chain in numerical order, with sustain and
end as roles inside it, per the spec and the FZ book.
Added [multi-loops](topics/multi-loops.md) and the looptm finding.
The spec says 16 ms steps where the book's figure shows repeat
counts. Map row for loops updated.

## [2026-08-21] lint | After the multi-loop ingest

Frontmatter complete on all pages; no orphans or duplicates. Map
drift fixed: the source list gains casio-service-manuals. Staleness
flagged on nine pages dated 2026-07-11 whose cited packages moved on
2026-08-18, all refactors; re-verify on next ingest. Twelve open
questions collected across six pages.

## [2026-08-21] ingest | Multi-loop pages hardened

An adversarial pass over the manuals found the book's Experiment 12
contradicting the looptm reconciliation, so the finding now leans
repeat count. The end loop credit widens to both envelopes, and the
spec's four bit loop fine comment joins casio-spec known errors.
