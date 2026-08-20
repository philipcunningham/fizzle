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

