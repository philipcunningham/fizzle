# Agent guide

## Validation

Run the complete non-mutating gate before submitting changes:

```sh
make check
```

Use `make check-fast` during development. UI changes also require `npm run visual` from `web/app`; its baselines are platform specific.

Run `make tools` once to install pinned Go tools. mise reads `.tool-versions` for the complete local toolchain.

## Architecture

- `cmd/fizzle` owns CLI parsing and process boundaries.
- `pkg/disk` owns disk structures and canonical FZ sample rates.
- `pkg/fzf` owns canonical full-dump documents and views.
- `pkg/container` performs pure container mutations and returns `model.Patch` values.
- `pkg/webcore` owns browser document state, validation, undo history, schemas, and atomic operations.
- `web/wasm/module` exposes structured success or error envelopes and contains panics.
- `web/app/src/boundary/contract.ts` defines the browser protocol. `src/core/worker.ts` serializes calls to the core.
- `web/app` owns view state only. FZ format behavior belongs in Go.
- `pkg/integration` covers multi-package pipelines, snapshots, and binary CLI behavior.

See [docs/architecture.md](docs/architecture.md) for dependency rules and their executable checks.

## Design rules

- Accept `io.Reader` in binary parsers and `io.Writer` in renderers.
- Keep transformations concrete and free of filesystem access.
- Parse environment variables at the CLI boundary.
- Use narrow consumer-owned interfaces at I/O boundaries. Don't add dependency injection frameworks or service locators.
- Use `fileutil.WriteAtomic` directly for file output.
- Keep canonical document state in `pkg/webcore`, not protocol or UI layers.
- Preserve atomic mutations: return a complete new snapshot or a structured error without changing state.
- Keep FZ parsing and mutation out of TypeScript.

Tests use `t.TempDir`, `bytes.Buffer`, fixture builders, and real filesystem behavior. Use `testutil.CaptureLog` only in tests that don't run in parallel.

## Performance

Measure changes to resampling, sample loops, WAV processing, and FZF assembly with adjacent benchmarks. Measure voice import and extraction too.

```sh
make benchmark
make profile
```

Report before and after measurements. Conversion output must remain byte identical unless the change intentionally updates reviewed golden checksums.

## Writing

Use lowercase `fizzle`, including at the start of a sentence.

Don't use dashes as sentence separators or use arrow characters. State each rule once in present tense.

Comments must explain a reason or record evidence that code can't express. Exported identifiers retain concise doc comments.

## Hardware evidence

Display values follow the panel, computation follows stored bytes, and edits preserve every untouched byte. Never derive one field's scale from another.

Verify mappings against firmware execution or hardware. Cite firmware behavior with its ROM address and routine name.

Evidence ranks from static firmware reading, to executed firmware, to real hardware measurement. Stronger evidence overrides weaker evidence.

Record display mappings and evidence in [llm-wiki/topics/display-scales.md](llm-wiki/topics/display-scales.md). Read [llm-wiki/AGENTS.md](llm-wiki/AGENTS.md) before editing the wiki.

## Symlinks and generated files

Root `CLAUDE.md` and `llm-wiki/CLAUDE.md` link to the adjacent `AGENTS.md`. Edit the target files only.

Don't edit `docs/cli-reference.md` directly. Change command metadata and let its test regenerate the reference.

Don't edit `.claude/skills` or verbatim files under `llm-wiki/sources` unless the task explicitly includes them.
