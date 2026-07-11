---
name: improve-codebase-architecture
description: Scan a codebase for deepening opportunities, present them as a visual HTML report, then grill through whichever one you pick.
disable-model-invocation: true
---

# Improve Codebase Architecture

Surface architectural friction and propose **deepening opportunities**: refactors that turn shallow modules into deep ones. The aim is testability and AI-navigability.

This command is _informed_ by the project's domain model and built on a shared design vocabulary:

- Use a fixed architecture vocabulary (**module**, **interface**, **depth**, **seam**, **adapter**, **reuse**, **locality**) and its principles (the deletion test, "the interface is the test surface", "one adapter = hypothetical seam, two = real"). Use these terms exactly in every suggestion; don't drift into "component," "service," "API," or "boundary."
- The domain language in `llm-wiki/` gives names to good seams; the root `AGENTS.md` records conventions this command shouldn't re-litigate.

## Process

### 1. Explore

Read the project's domain pages first: `llm-wiki/index.md`, then the pages relevant to the area you're touching, plus the root `AGENTS.md` conventions.

Then use the Agent tool with `subagent_type=Explore` to walk the codebase. Don't follow rigid heuristics; explore organically and note where you experience friction:

- Where does understanding one concept require bouncing between many small modules?
- Where are modules **shallow**, with an interface nearly as complex as the implementation?
- Where have pure functions been extracted just for testability, but the real bugs hide in how they're called (no **locality**)?
- Where do tightly-coupled modules leak across their seams?
- Which parts of the codebase are untested, or hard to test through their current interface?

Apply the **deletion test** to anything you suspect is shallow: would deleting it concentrate complexity, or just move it? A "yes, concentrates" is the signal you want.

### 2. Present candidates as an HTML report

Write a self-contained HTML file to the OS temp directory so nothing lands in the repo. Resolve the temp dir from `$TMPDIR`, falling back to `/tmp` (or `%TEMP%` on Windows), and write to `<tmpdir>/architecture-review-<timestamp>.html` so each run gets a fresh file. Open it for the user (`xdg-open <path>` on Linux, `open <path>` on macOS, `start <path>` on Windows) and tell them the absolute path.

The report uses **Tailwind via CDN** for layout and styling, and **Mermaid via CDN** for diagrams where a graph/flow/sequence reliably communicates the structure. Mix Mermaid with hand-crafted CSS/SVG visuals. Use Mermaid when relationships are graph-shaped (call graphs, dependencies, sequences). Use hand-built divs/SVG when you want something more editorial (mass diagrams, cross-sections, collapse animations). Each candidate gets a **before/after visualisation**. Be visual.

For each candidate, render a card with:

- **Files**: which files/modules are involved
- **Problem**: why the current architecture is causing friction
- **Solution**: plain English description of what would change
- **Benefits**: explained in terms of locality and reuse, and how tests would improve
- **Before / After diagram**: custom-drawn panels side by side, illustrating the shallowness and the deepening
- **Recommendation strength**: one of `Strong`, `Worth exploring`, `Speculative`, rendered as a badge

End the report with a **Top recommendation** section: which candidate you'd tackle first and why.

**Use the llm-wiki vocabulary for the domain, and the architecture vocabulary above for the architecture.** If `llm-wiki/` defines "key split," talk about "the key-split mapping module", not "the FooBarHandler," and not "the split service."

**Convention conflicts**: a candidate may contradict a documented convention in the root `AGENTS.md`. Only surface it when the friction is real enough to warrant revisiting the convention. Mark it clearly in the card (for example a warning callout: _"contradicts the dependency-injection conventions, but worth reopening because…"_). Don't list every theoretical refactor the conventions forbid.

See [HTML-REPORT.md](HTML-REPORT.md) for the full HTML scaffold, diagram patterns, and styling guidance.

DON'T propose interfaces yet. After the file is written, ask the user: "Which of these would you like to explore?"

### 3. Grilling loop

Once the user picks a candidate, run the `/grilling` skill to walk the design tree with them. Cover constraints, dependencies, the shape of the deepened module, what sits behind the seam, and what tests survive.

Side effects happen inline as decisions crystallize; update the project's domain docs directly to keep the domain model current as you go:

- **Naming a deepened module after a domain concept `llm-wiki/` lacks?** File the term into the wiki with the llm-wiki-ingest skill.
- **Sharpening a fuzzy term during the conversation?** Update the relevant `llm-wiki/` page right there.
- **User rejects the candidate with a lasting reason?** Offer to record it as a convention in the root `AGENTS.md`, framed as: _"Want me to record this convention so future architecture reviews don't re-suggest it?"_ Only offer when the reason would actually be needed by a future explorer to avoid re-suggesting the same thing; skip ephemeral reasons ("not worth it right now") and self-evident ones.
- **Want to explore alternative interfaces for the deepened module?** Design it twice: spawn parallel sub-agents to draft two competing interfaces, then compare them before committing.
