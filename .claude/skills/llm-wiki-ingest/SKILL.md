---
name: llm-wiki-ingest
description: Ingest a source (spec section, hardware experiment, corpus discovery, firmware research result) into llm-wiki following its schema.
---

# llm-wiki-ingest

Read `llm-wiki/AGENTS.md` first; it is the authority. Then:

1. Read the source in full.
2. Route content with the new-page-versus-edit rule; merge rather than
   accumulate. Never mirror a single greppable file.
3. Write or update pages with complete frontmatter; update `map.md` if
   routing changed.
4. Update `llm-wiki/index.md`; append one entry to `llm-wiki/log.md`.
5. Record contradictions between evidence sources as finding pages;
   put unverified claims under `## Open questions`.
6. Keep ingest idempotent: check the index for existing slugs and
   citations before creating pages.

Finish by running the llm-wiki-lint skill.
