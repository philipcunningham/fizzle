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
