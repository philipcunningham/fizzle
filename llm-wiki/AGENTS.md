# llm-wiki schema

Rules for `llm-wiki/`: an LLM maintained knowledge base about the Casio FZ samplers and their file formats. Read this file before touching anything here.

Knowledge of firmware behaviour comes from reverse engineering the FZ-1 firmware. Cite firmware findings by ROM address and routine name (`midi_note_on` at `F000:0FFD`).

## Layers

Raw sources are immutable; the LLM reads them, never edits them. They include the Casio specification, an authorized corpus when installed, and FZ-1 firmware research. External references are cited by URL from their source pages; their content is never copied into this repo.

This directory is LLM written; humans read it and direct the work. The schema co-evolves: when a rule proves wrong, update it and log the change.

## Evidence hierarchy

Evidence ranks in this order; when sources disagree, the higher-ranked one wins and the disagreement becomes a finding page.

1. Hardware observation (corpus bytes, round-trip tests, real samplers).
2. Firmware behaviour, established by reverse engineering.
3. Casio spec: authoritative for intent, known to contain errors.
4. Community docs (Buchty, Vosmaer, Undecyclenate).

## Pages

- `map.md`: per topic, the spec section, the fizzle code paths, and the wiki page where one exists. `index.md` is the first hop for every query and routes to pages; the map routes topics to the spec and the code.
- `project.md`: where the wiki links into the repo tree. It carries only the wiki's cross-references and is the second and last map page.
- `findings/<slug>.md`: one page per divergence between evidence sources (spec says X, hardware does Y): claim, evidence, status, what fizzle implements.
- `topics/<slug>.md`: synthesis pages, only when content spans two or more sources.
- `sources/<slug>.md`: one page per raw source: what it is authoritative for and its known errors. Verbatim raw documents (the Casio spec today, anything acquired later) live directly in `sources/` beside these pages: no frontmatter, never linted, never edited.

Rules:

- New page only for a distinct entity other pages would link to; otherwise edit in place. Merge overlapping pages.
- Never mirror a single greppable file; link to it from `map.md`. A page that restates one source shouldn't exist; synthesis across sources may run longer than any one of them.
- Pages are trusted at query time; freshness is lint's job, never a re-verify instruction on the page.
- A page states the best current reading, in the present tense. It never narrates its own history: no "this page used to say", no record of a reversal. `log.md` carries what changed, and a page that argues with its past selves costs every later reader the detour.
- Any page may carry `## Open questions` (claims awaiting a hardware experiment or firmware trace); lint collects them as the research agenda.

## Frontmatter

On every page except `index.md` and `log.md`:

```yaml
---
type: map | finding | topic | source
title: Voice-area sizing
description: One sentence, used by the index for routing.
tags: [fzf, bank, parsing]
updated: 2026-07-11
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md section 2-2
  - FZ-1 firmware system guide, chapter 4
status: confirmed-hardware | confirmed-firmware | spec-only | suspect
---
```

`status` names the strongest evidence behind the page's claims; pages of type map make no evidentiary claims and omit it. `updated` means the page's content and citations were verified on that date. Editing a page requires rechecking its citations.

## index.md and log.md

`index.md`: one line per page (`- [Title](path.md): summary.`), grouped by type. Read it first on every query; never scan page bodies for relevance. Update it in the same session as any page change.

`log.md`: append only, one entry per operation (`## [YYYY-MM-DD] ingest | title`, body two to five lines). Operations: scaffold, ingest, query, lint, schema. Never rewrite old entries.

## Workflows

Ingest: read the source; place content with the new-page-versus-edit rule; update pages, `map.md`, `index.md`, `log.md`. Contradictions between evidence sources become finding pages. Ingest is idempotent: findings key on stable slugs and citations, so re-ingesting converges instead of duplicating.

Query: read `index.md` first; open `map.md` when the question needs spec-section or code anchors. Then read only the selected pages and answer with citations. File the answer back only when it adds new evidence or synthesis.

Run `make lint-docs` after every ingest session. Vale checks prose, but it doesn't validate the wiki schema. Manually check frontmatter, links, index coverage, map paths, duplicate pages, and stale citations. An orphan is a page whose only inbound link is the index. Log every audit.

## Citations

- fizzle code: `pkg/fzf/document.go` and the relevant bounded view.
- Firmware: ROM address and routine name; reverse engineering documents by filename and section.
- Casio spec: file plus section number.
- Corpus: collection and fixture identity, with counts for statistical claims. Add a path when the authorized corpus is installed.

## Writing style

Root `AGENTS.md` rules apply: no em dash, `--`, or `-` as a grammatical separator; no right arrow character; lowercase fizzle. Short declarative sentences; pages are retrieval targets, not essays.
