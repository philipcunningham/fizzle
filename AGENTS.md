# Agent Guide

## Validation

Run the full check suite before submitting changes:

```sh
make check
```

This runs formatting, vet, lint, unit tests, CLI integration tests, and fuzz
seed validation.

## Individual commands

- `make test` runs unit tests with race detector
- `make lint` runs golangci-lint
- `make fmt` formats code
- `make vet` runs go vet
- `make integration-test` runs CLI integration tests (builds binary automatically)
- `make build` builds the binary (regenerates `internal/licenses/THIRD_PARTY_LICENSES.txt` and `fizzle.cdx.json` first, with `-tags release` so the attribution is embedded)
- `make tools` installs the pinned supply-chain tooling (`go-licenses`, `cyclonedx-gomod`); run once before the first `make build`
- `make licenses` regenerates third-party attribution and copies `LICENSE` into the embed directory
- `make sbom` regenerates the CycloneDX SBOM at `fizzle.cdx.json`

## Project structure

- `cmd/fizzle/` is the CLI entry point
- `pkg/disk/` is the core domain model (disk image format, sectors, directory). Owns the canonical FZ sample-rate constants (`rates.go`: `SampleRates`, `RateIndexFor`, `SampleRate`, `ValidateRate`).
- `pkg/sfz/` is the SFZ format parser
- `pkg/sfzconvert/` is the SFZ to FZ conversion pipeline
- `pkg/sfzexport/` is the SFZ export pipeline (FZF to SFZ with WAV extraction)
- `pkg/wav/` is the WAV file reader/writer
- `pkg/voice*/` contains voice file operations (import, extract, build, unpack, edit)
- `pkg/disk*/` contains disk operations (format, list, add, get, copy)
- `pkg/studio/` contains the interactive Bubble Tea TUI (`fizzle studio`), a workspace-oriented editor for FZ-1 / FZ-10M / FZ-20M sound material. Sub-packages: `app/` (root tea.Model + Update / View, modal stack, save / autosave / recovery, journey tests), `audio/` (audition path via oto with single-in-flight playback and an owner-identity guard), `clock/` (tea.Tick seam for tests), `loader/` (.img / .fzf loader returning a model.Model + ContainerInfo summary), `model/` (in-memory container bytes plus undo/redo and dirty flag), `nav/` (Action enum + keymap), `spaces/{workspace,pool,layout,sound}/` (one sub-package per space), `theme/` (lipgloss palette), and `widgets/` (minimap, status, toast, hint, help, confirm, areaeditor, effectseditor, envelopevisual, lfovisual, samplevisual, topbar). studio's own README is at `pkg/studio/README.md`; it carries the feature spec, key bindings, user workflows, and testing strategy.
- `pkg/fzf*/` contains full dump operations (info, midi, output, effects). Note: `fzf build`, `fzf unpack`, and `fzf edit` dispatch to `pkg/voicebuild/`, `pkg/voiceunpack/`, and `pkg/voiceedit/` respectively.
- `pkg/fzb*/` contains bank dump operations (info)
- `pkg/fzv*/` contains voice info display
- `pkg/audioplayer/` provides cross-platform audio playback: native audio on macOS and Windows via oto/v3, system audio players (`aplay`, `paplay`, `ffplay`) on Linux. Exports a `Player` interface and `TestPlayer` for testing.
- `pkg/fzutil/` contains shared utilities (bounded file reads, resampling, voice-name normalisation, FZF header parsing)
- `pkg/fileutil/` contains atomic file writing and a cross-process file lock
- `pkg/logger/` contains zerolog initialisation and `Silence()` (used by the studio TUI to suppress library log output without redirecting stderr)
- `pkg/render/` contains shared output formatting (tables, note names, byte sizes)
- `pkg/version/` contains version string
- `pkg/integration/` contains three test layers: package-level integration tests (`integration_test.go`) that exercise multi-package pipelines against real-hardware fixture images with golden SHA-256 checksums; corpus snapshot tests (`corpus_snapshot_test.go`) that assert byte-equal `fzf info` / `fzv info` / `disk ls` / `sfz` parse JSON output against the ~254 fixtures under `testdata/corpus/` and `testdata/synthetic/` via `go-snaps`; and CLI binary-executing tests (`cli_test.go`) gated behind the `integration` build tag and run by `make integration-test`. Refresh snapshots with `UPDATE_SNAPS=true go test ./pkg/integration/ -run TestCorpus`.
- `pkg/internal/bitconv/` contains PCM sample bit-pattern conversions (centralises gosec G115 suppressions)
- `pkg/internal/limits/` contains shared upper bounds for untrusted-input reads (`MaxRead = 256 MiB`) to bound memory use on malformed input
- `internal/licenses/` exposes the project license, third-party attribution, and CycloneDX SBOM to the CLI's `licenses` subcommand (`fizzle licenses` for full text, `fizzle licenses --json` for the SBOM). Stub strings ship without the `release` build tag so plain `go build`/`go test` work without running `make licenses` first; `make build` adds `-tags release` and the embedded text replaces the stubs.
- `pkg/internal/testutil/` contains shared test helpers
- `docs/` contains the FZ-1 data-structures specification (`casio-fz1-data-structures.md` + the original Casio R&D reference PDF), the format implementation notes (`casio-fz1-format.md`), the long-form user manual (`fizzle-manual.md`), and the benchmarking notes (`fizzle-benchmarking.md`)

## Dependency injection conventions

Inject dependencies at boundaries; keep internal logic simple and concrete.

**Output rendering:** Functions that produce text, table, or JSON output accept
`io.Writer`. The CLI boundary (`cmd/fizzle/main.go`) passes `os.Stdout`.
Tests pass `bytes.Buffer`.

**Input parsing:** Core binary parsers accept `io.Reader` (e.g. `disk.ReadImage`,
`wav.Read`). Convenience wrappers like `disk.OpenImage(path)` handle the
`os.Open` call.

**Pure data functions:** Many packages separate pure computation from I/O.
Unexported byte-level functions (e.g. `fzvinfo.parseHeader`,
`voiceedit.applyPatches`, `voiceunpack.unpack`) accept `[]byte` and return
values without filesystem access. Other pure functions like
`diskformat.buildImage` accept simple parameters (a `string` label) and
return `[]byte` with no I/O. Same-package
tests can call these directly. Do not export pure internals solely for
test access; use white-box tests instead.

**Logging:** Use `logger.InitWithWriter(debug, w)` in tests to capture log
output to a `bytes.Buffer` instead of mutating the global logger. Production
code uses `logger.Init(debug)` which writes to stderr. The shared test helper
`testutil.CaptureLog` uses `InitWithWriter` internally. `logger.Silence()`
discards all log output and returns a restore function; the studio TUI uses
this to suppress library log noise during interactive sessions.

**Audio playback:** The `audioplayer` package exports a `Player` interface with
platform-specific backends selected by build tags. `NewPlayer()` returns the
real backend; `NewTestPlayer(available)` returns a recording test double. The `studio`
package accepts `audioplayer.Player` and tests inject `NewTestPlayer` to verify
playback behaviour without audio hardware.

**Environment variables:** Parse environment variables at the CLI boundary and
pass values as struct fields or function parameters. Do not call `os.Getenv`
deep in library code.

**Do not:**
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

Do not use `--`, `-`, or em dash (`—`) as a grammatical separator in code
comments, markdown files, or documentation. Use proper punctuation instead:
periods, colons, semicolons, commas, or parentheses. Restructure the sentence
if needed.

Do not use the right-arrow character `→` in code comments, markdown files,
or documentation. Use English instead. For example, write "SFZ to FZF"
rather than "SFZ→FZF", and "build then unpack round trip" rather than
"build→unpack round trip". Restructure with "maps to", "yields", or
"becomes" if a plain "to" reads poorly.
