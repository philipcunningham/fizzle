# Agent skills

Claude Code skills for work on fizzle.

## Adapted skills

The following skills are adapted from Matt Pocock's skills repository
at <https://github.com/mattpocock/skills>:

- `grilling`
- `improve-codebase-architecture`
- `to-spec`
- `to-tickets`
- `wayfinder`

The prose follows this repo's writing style (`make lint-docs`).
References to artefacts fizzle lacks, such as a `CONTEXT.md` glossary
or ADRs, point at `llm-wiki/` and the root `AGENTS.md` instead.

### Upstream license

The adapted skills remain under their original MIT license:

```text
MIT License

Copyright (c) 2026 Matt Pocock

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Project skills

Everything else in this directory is fizzle's own, under the
repository's root [LICENSE](../../LICENSE):

- `llm-wiki-ingest` and `llm-wiki-lint` maintain the knowledge base at
  `llm-wiki/`; their authority is `llm-wiki/AGENTS.md`.
