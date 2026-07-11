---
name: llm-wiki-lint
description: Health-check llm-wiki; report and flag issues without writing content pages or deleting anything.
---

# llm-wiki-lint

Read `llm-wiki/AGENTS.md` first. Check every page under `llm-wiki/`:

1. Frontmatter complete and valid (type, title, description, tags,
   updated, sources, status).
2. Staleness: cited files with commits dated after the page's
   `updated` date (`git log --since`); oldest pages checked against
   newer pages for contradictions.
3. Coverage gaps: concepts mentioned on multiple pages with no page or
   map entry of their own.
4. Map drift: `map.md` behind newer pages.
5. Orphans: pages whose only inbound link is the index. The index
   links every page by construction, so count links from the map and
   from other pages.
6. Duplicates: near-identical titles or overlapping scope.
7. Open questions: collect every `## Open questions` section.

Report findings grouped by check. Fix frontmatter only where the correct
value is unambiguous. Never write content pages; never delete anything
without user approval. Append a lint entry to `llm-wiki/log.md`.
