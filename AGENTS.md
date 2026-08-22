# Agent Guide

## Validation

Run the full check suite before submitting changes:

```sh
make check
```

This runs formatting, vet, lint, unit tests, CLI integration tests, fuzz
seed validation, and the browser checks below.

`make check` also runs the browser editor's three targets:

- `make wasm` builds the browser core into the front end's assets
- `make wasm-check` builds the browser target, catching a broken `js/wasm` build
- `make web-check` runs the front end chain: format, lint, types, unit tests, build, and the payload budget

Two front end gates sit outside `make check`. Run them from `web/app`:

- `npm run smoke` drives the built app through a browser over the real core
- `npm run visual` compares per-platform screenshot baselines

CI skips `npm run visual` because the baselines are per-platform, so it
needs running by hand before a UI change ships.

## Individual commands

- `make test` runs unit tests with race detector
- `make lint` runs `lint-go` and `lint-docs`
- `make lint-go` runs golangci-lint
- `make lint-docs` runs vale over markdown (install vale with `brew install vale`)
- `make fmt` formats code
- `make vet` runs go vet
- `make integration-test` runs CLI integration tests (builds binary automatically)
- `make build` builds the binary (regenerates `internal/licenses/THIRD_PARTY_LICENSES.txt` first, with `-tags release` so the attribution is embedded)
- `make tools` installs the pinned `go-licenses` tool; run once before the first `make build`
- `make licenses` regenerates third-party attribution and copies `LICENSE` into the embed directory

## Project structure

- `cmd/fizzle/` is the CLI entry point
- `pkg/disk/` is the core domain model (disk image format, sectors, directory). Owns the canonical FZ sample-rate constants (`rates.go`: `SampleRates`, `RateIndexFor`, `SampleRate`, `ValidateRate`).
- `pkg/sfz/` is the SFZ format parser
- `pkg/sfzconvert/` is the SFZ to FZ conversion pipeline
- `pkg/sfzexport/` is the SFZ export pipeline (FZF to SFZ with WAV extraction)
- `pkg/wav/` is the WAV file reader/writer
- `pkg/voice*/` contains voice file operations (import, extract, build, unpack, edit)
- `pkg/disk*/` contains disk operations (format, list, add, get, copy)
- `pkg/container/` contains pure FZF/disk container byte surgery: compaction, bank grow, and area swap/delete/duplicate patches. Functions take container bytes and return new bytes or `model.Patch` lists, unit-testable without a UI.
- `pkg/model/` holds `Patch`, the byte-range mutation type `pkg/container` produces and `pkg/webcore` applies.
- `pkg/webcore/` is the session facade the browser talks to. It owns the open document, validation, capacity, undo history, and the parameter schema the voice editor renders its controls from. A document is one disk image, or the pair a split instrument spans. Every mutating call is atomic: it returns either a fresh snapshot or a structured error envelope carrying a stable machine code. Canonical state lives here, never in the layers above.
- `web/wasm/` is the `js/wasm` entry point that exposes the facade to JavaScript. `module/` registers `fizzleCore` on the JS global and wraps each result in an `{ok, value}` or `{ok, error}` envelope. A Go panic is recovered into an envelope rather than crossing the boundary raw. `surface_js.go` pins the import surface the browser build needs, so `make wasm-check` catches a broken `js/wasm` build.
- `web/app/` is the React and TypeScript front end. `src/boundary/contract.ts` is the typed boundary both sides agree on. `src/core/worker.ts` runs the core in a Web Worker and serialises calls onto it. `src/core/fake.ts` is the hermetic fake the unit tests drive, and `src/shell/` holds the shell and its view state. Screens, controls, and dialogs live in `src/screens/`, `src/ui/`, and `src/dialogs/`. The front end owns view state only; no FZ format logic lives outside the core.
- `pkg/fzf*/` contains full dump operations (info, midi, output, effects). Note: `fzf build` dispatches to `pkg/voicebuild/`, `fzf unpack` to `pkg/voiceunpack/`, and `fzf edit` to `pkg/voiceedit/`.
- `pkg/fzb*/` contains bank dump operations (info)
- `pkg/fzv*/` contains voice info display
- `pkg/audioplayer/` provides cross-platform audio playback: native audio on macOS and Windows via oto/v3, system audio players (`aplay`, `paplay`, `ffplay`) on Linux. Exports a `Player` interface and `TestPlayer` for testing.
- `pkg/fzutil/` contains shared utilities (bounded file reads, resampling, voice-name normalisation, FZF header parsing)
- `pkg/fileutil/` contains atomic file writing and a cross-process file lock
- `pkg/logger/` contains zerolog initialisation and `Silence()` (discards library log output without redirecting stderr)
- `pkg/render/` contains shared output formatting (tables, note names, byte sizes)
- `pkg/version/` contains version string
- `pkg/integration/` contains three test layers: package-level integration tests (`integration_test.go`) that exercise multi-package pipelines against real-hardware fixture images with golden SHA-256 checksums; corpus snapshot tests (`corpus_snapshot_test.go`) that assert byte-equal `fzf info` / `fzv info` / `disk ls` / `sfz` parse JSON output against the ~254 fixtures under `testdata/corpus/` and `testdata/synthetic/` via `go-snaps`; and CLI binary-executing tests (`cli_test.go`) gated behind the `integration` build tag and run by `make integration-test`. Refresh snapshots with `UPDATE_SNAPS=true go test ./pkg/integration/ -run TestCorpus`.
- `pkg/internal/bitconv/` contains PCM sample bit-pattern conversions (centralises gosec G115 suppressions)
- `pkg/internal/limits/` contains shared upper bounds for untrusted-input reads (`MaxRead = 256 MiB`) to bound memory use on malformed input
- `internal/licenses/` exposes the project license and third-party attribution to the CLI's `licenses` subcommand (`fizzle licenses` prints the full text). Stub strings ship without the `release` build tag so plain `go build`/`go test` work without running `make licenses` first; `make build` adds `-tags release` and the embedded text replaces the stubs.
- `pkg/internal/testutil/` contains shared test helpers
- `docs/` contains the long-form user manual (`fizzle-manual.md`) and the benchmarking notes (`fizzle-benchmarking.md`). The FZ-1 data-structures specification (markdown transcription plus the original Casio R&D PDF) lives in `llm-wiki/sources/`; format findings and synthesis live in `llm-wiki/`

## Dependency injection conventions

Inject dependencies at boundaries; keep internal logic plain and concrete.

**Output rendering:** Functions that produce text, table, or JSON output accept
`io.Writer`. The CLI boundary (`cmd/fizzle/main.go`) passes `os.Stdout`.
Tests pass `bytes.Buffer`.

**Input parsing:** Core binary parsers accept `io.Reader` (for example `disk.ReadImage`,
`wav.Read`). Convenience wrappers like `disk.OpenImage(path)` handle the
`os.Open` call.

**Pure data functions:** Many packages separate pure computation from I/O.
Unexported byte-level functions (for example `fzvinfo.parseHeader`,
`voiceedit.applyPatches`, `voiceunpack.unpack`) accept `[]byte` and return
values without filesystem access. Others like `diskformat.buildImage` take
plain parameters (a `string` label) and return `[]byte` with no I/O.
Same-package tests call these directly. Don't export pure internals solely
for test access; use white-box tests instead.

**Logging:** Use `logger.InitWithWriter(debug, w)` in tests to capture log
output to a `bytes.Buffer` instead of mutating the global logger. Production
code uses `logger.Init(debug)` which writes to stderr. The shared test helper
`testutil.CaptureLog` uses `InitWithWriter` internally. `logger.Silence()`
discards all log output and returns a restore function; the sfzconvert
and webcore benchmarks use this to suppress library log noise.

**Audio playback:** The `audioplayer` package exports a `Player` interface with
platform-specific backends selected by build tags. `NewPlayer()` returns the
real backend; `NewTestPlayer(available)` returns a recording test double. The
CLI's `fzv play` uses `NewPlayer()`; tests inject `NewTestPlayer` to verify
playback behaviour without audio hardware.

**Environment variables:** Parse environment variables at the CLI boundary and
pass values as struct fields or function parameters. Don't call `os.Getenv`
deep in library code.

**Don't:**
- Use DI frameworks or service locators.
- Define broad interfaces in producer packages.
- Mock `os.Open`, `filepath.Join`, or other stable standard library calls.
- Abstract `fileutil.WriteAtomic` (atomicity requires real filesystem).
- Inject dependencies into pure data transformation functions.

**Testing:** Use `t.TempDir()` for filesystem tests. Use `bytes.Buffer` for
output capture. Use test fixture generators (`testutil.MakeTestVoice`,
`fzfbuilder.MakeTestFZF`) for in-memory test data.

## Performance

Hot paths are the SFZ to FZF conversion pipeline (`pkg/sfzconvert/`), the per-sample
loops in `pkg/fzutil/Resample`, `pkg/wav/`, `pkg/voiceimport/`, `pkg/voiceextract/`,
and the FZF assembly in `pkg/voicebuild/`. Benchmarks live next to the code as
`*_bench_test.go`. Run all benchmarks with:

```sh
make benchmark
```

For a focused CPU/alloc profile of the dominant end-to-end workload (the
28-voice JUNGLISM convert):

```sh
make profile
```

When proposing a performance change, capture before/after numbers from
`make benchmark` and quote the relevant deltas alongside the change. See
[docs/fizzle-benchmarking.md](../docs/fizzle-benchmarking.md) for how to run individual
benchmarks, capture profiles, and use `benchstat` for statistical
comparison. The integration tests at `pkg/integration/integration_test.go`
hold golden SHA-256 checksums over the conversion pipeline; any perf
change must keep the output bytes identical.

## Writing style

Don't use `--`, `-`, or an em dash (U+2014) as a grammatical separator
in code comments, markdown files, or documentation. Use proper
punctuation instead (periods, colons, semicolons, commas, or
parentheses), and restructure the sentence if needed.

Don't use the right-arrow character (U+2192) in code comments, markdown
files, or documentation. Write the relationship in English: "SFZ to FZF"
rather than an arrow between them, and "build then unpack round trip"
rather than an arrow chain. Restructure with "maps to", "yields", or
"becomes" if a plain "to" reads poorly.

The project name is always lowercase fizzle, including at the start of a
sentence.

Knowledge of FZ-1 firmware behaviour comes from reverse engineering the
firmware. Cite firmware findings by ROM address and routine name, for
example `midi_note_on` at `F000:0FFD`.

## Panel values and stored bytes

The FZ front panel shows most fields on its own scale, and Casio chose
that scale per field. The velocity quartet is the raw signed byte, and
velocity to resonance is plus or minus 100. AREA LEVEL is 127 minus
the byte, envelope rates and levels are 0 to 99, and key follow is the
byte over 8. There is no single rule, so never assume one.

Three layers, each taking its answer from a different place:

- Display follows the panel, per field, since a user checks the screen
  against the machine.
- Computation follows the stored byte, always. The firmware reads
  bytes, so a preview or a model that reads a display value is wrong.
- Storage preserves the byte. Never write back a field the user
  didn't edit: a display value that round trips through its scale
  isn't the byte it came from.

Derive a field's mapping from the firmware, not by guessing. Use
ghidra-mcp against the ROM. A panel row is a 24 byte blob: label text,
a max word, then a min word. The bounds come out of a read, and the
conversion sits in the screen that renders the row. `AREA LEVEL` is
127 minus the byte at F000:6562, written back inverted at F000:6725.

The firmware wins. A mapping the ROM shows is an invariant, and code
or documentation that disagrees is the thing to change. Override it
only with a measurement taken on a real device. Record that
measurement, the device, and the bytes tested, beside the firmware
reading.

Record every mapping in
[llm-wiki/topics/display-scales.md](llm-wiki/topics/display-scales.md),
with its evidence: a ROM address, or the hardware calibration that
overrode one.

## CLAUDE.md symlinks

`CLAUDE.md` at the repo root and `llm-wiki/CLAUDE.md` are symlinks to
the `AGENTS.md` beside them. Edit the `AGENTS.md` files only.

## Knowledge wiki

`llm-wiki/` is an LLM maintained knowledge base about the Casio FZ
samplers and their file formats. Its schema is `llm-wiki/AGENTS.md`;
read it before touching anything under `llm-wiki/`. For questions about
FZ formats or hardware behaviour, read `llm-wiki/index.md` first instead
of re-deriving from raw sources. Ingest and health checks run through
the `llm-wiki-ingest` and `llm-wiki-lint` skills.

## Agent skills

`.claude/skills/` holds the Claude Code skills: the llm-wiki pair
above plus skills adapted from Matt Pocock's skills repository.
`.claude/skills/README.md` names the adapted skills, describes the
local modifications, and reproduces the upstream MIT license.

## Docs tooling

vale lints markdown prose: `.vale.ini` plus custom rules under
`.vale/styles/fizzle/`. Every rule is an error and any finding fails
the build. Hard bans cover the em dash, arrows, separator hyphens,
slop terms, and repetition. Tone rules cover present tense,
contractions, Oxford comma, sentence spacing, precise wording over
subjective claims, inclusive language, plain English over Latin, and
sentence length. The only per-file exclusions are the verbatim Casio
transcription and the CLAUDE.md symlinks (which would double-report
AGENTS.md). A hook in `.claude/settings.json` runs vale on every file
written or edited (`go run ./scripts/valehook`, blocking with the
findings). `make lint-docs` is the manual full-repo pass.
