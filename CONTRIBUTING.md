# Contributing to fizzle

Requires **Go 1.26+**.

## Before you contribute

Pull requests are disabled on this repository. If you'd like to make a
contribution, please open an issue first. The issue may include a spec, a
patch, or a proposal, and is how proposed changes get discussed and merged.

If you encounter a bug or something that isn't very ergonomic, please consider
sharing an `.img` or `.fzf` along with steps to reproduce. Concrete fixtures
make it much easier to investigate and to add a regression test once the
underlying issue is fixed.

If your issue includes a patch, please make sure that:

- `make check` passes
- `make integration-test` passes
- new functionality is covered by tests
- documentation is updated if commands change

## Prerequisites

- **Go 1.26+**: https://go.dev/dl/
- **golangci-lint**: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest` (or see https://golangci-lint.run/welcome/install/)
- **make**: included on macOS and Linux; on Windows, use `make` from Git for Windows or run the Go commands directly

## Building

```sh
git clone https://github.com/philipcunningham/fizzle
cd fizzle
make tools   # one-off: installs go-licenses and cyclonedx-gomod at pinned versions
make build
```

`make build` regenerates `internal/licenses/THIRD_PARTY_LICENSES.txt` and
`fizzle.cdx.json` (a CycloneDX SBOM) every time, and compiles with
`-tags release` so the binary embeds those artefacts. Both generated
files are gitignored.

Plain `go build`, `go test`, `go vet`, and `make check` work without
running `make tools` or `make licenses`: they compile the
`!release`-tagged stub in `internal/licenses/licenses.go`. The
shipped binary uses the embedded version; development workflows use
the stub.

To inspect the embedded attribution after a release build:

```sh
./fizzle licenses | less                       # full text (compliance-grade)
./fizzle licenses --json | jq '.components'    # CycloneDX SBOM (supply-chain tooling)
```

## Testing

Run the full validation suite:

```sh
make check
```

This runs formatting, vet, lint, unit tests, CLI integration tests, and fuzz
seed validation in sequence.

Run tests with race detection:

```sh
make test
```

For a faster inner loop during development, skip the package integration
tests (which read large fixture files) with:

```sh
go test -short -race ./...
```

Run integration tests (builds the binary, then exercises the CLI end-to-end):

```sh
make integration-test
```

Integration tests use fixtures in `testdata/`. They are pure Go and run on
Linux, macOS, and Windows. No shell or Python required.

### Test layers

fizzle uses three layers of automated tests:

**Layer 1: Unit tests** (`pkg/*/_test.go`): validate individual package
logic, edge cases, error paths, and boundary conditions. Run with `make test`.

**Layer 2: Package integration tests** (`pkg/integration/integration_test.go`):
validate multi-package pipelines at the data level using real hardware fixture
images. Include audio fidelity correlation checks and golden SHA-256 checksums.
Run with `make test`.

**Layer 3: Binary-executing integration tests** (`pkg/integration/cli_test.go`):
validate the compiled binary end-to-end via `os/exec`. These tests check CLI
output, exit codes, error messages, and flag handling. Gated behind the
`integration` build tag. Run with `make integration-test`.

### When to write which test

| Scenario | Test layer |
|---|---|
| New package function or edge case | Unit test in `pkg/<package>/_test.go` |
| Multi-package data pipeline | Package integration test |
| New CLI command, flag, or error message | Binary-executing test in `cli_test.go` |
| New `--json` flag on a command | Both: package test for `RenderJSON`, CLI test for flag wiring |
| Binary format parser robustness | Fuzz test in `pkg/<package>/fuzz_test.go` |

### JSON output testing

Commands that support `--json` (`disk ls`, `fzv info`, `fzf info`) need tests
at two levels:

1. **Package test**: call `RenderJSON` directly and verify the struct is
   serialized correctly (these already exist in `disklist`, `fzvinfo`,
   `fzfinfo`).
2. **CLI test**: run the binary with `--json` and verify the output is valid
   JSON with expected top-level keys. This catches flag wiring bugs that
   package tests cannot detect.

When adding `--json` to a new command, add both tests.

### Golden checksums

`integration_test.go` uses SHA-256 checksums to verify that the conversion
pipeline produces byte-identical output. If you intentionally change the
output format (resampling, sector layout, voice packing), the golden
checksums will fail.

To update after an intentional change:

1. Run `go test -v ./pkg/integration/` and find the failing checksum test.
2. Copy the "got" hex string from the test output.
3. Replace the corresponding "want" string in `integration_test.go`.
4. Verify the change is intentional by inspecting the diff.

Do not update checksums to fix a test without understanding why the output
changed.

### Fuzz tests

Fuzz tests exist for binary format parsers, conversion pipelines, and format
integrity chains (`pkg/wav`, `pkg/sfz`, `pkg/disk`, `pkg/diskformat`,
`pkg/voiceextract`, `pkg/voiceimport`, `pkg/voiceunpack`, `pkg/sfzconvert`,
`pkg/sfzexport`, `pkg/fzutil`, `pkg/fzbinfo`, `pkg/fzvinfo`, `pkg/fzfinfo`,
`pkg/fzfoutput`, `pkg/fzfmidi`, `pkg/fzfeffects`, `pkg/voicebuild`,
`pkg/voiceedit`, `pkg/integration`).
Run them with:

```sh
go test -fuzz=FuzzRead ./pkg/wav/ -fuzztime=30s
go test -fuzz=FuzzParse ./pkg/sfz/ -fuzztime=30s
go test -fuzz=FuzzReadImage ./pkg/disk/ -fuzztime=30s
go test -fuzz=FuzzFormat ./pkg/diskformat/ -fuzztime=30s
go test -fuzz=FuzzDecode ./pkg/voiceextract/ -fuzztime=30s
go test -fuzz=FuzzImport ./pkg/voiceimport/ -fuzztime=30s
go test -fuzz=FuzzUnpack ./pkg/voiceunpack/ -fuzztime=30s
go test -fuzz=FuzzConvertVoices ./pkg/sfzconvert/ -fuzztime=30s
go test -fuzz=FuzzResampleIdentity ./pkg/fzutil/ -fuzztime=30s
go test -fuzz=FuzzParseFZB ./pkg/fzbinfo/ -fuzztime=30s
go test -fuzz=FuzzParseFZV ./pkg/fzvinfo/ -fuzztime=30s
go test -fuzz=FuzzParseFZF ./pkg/fzfinfo/ -fuzztime=30s
go test -fuzz=FuzzParseOutputFlag ./pkg/fzfoutput/ -fuzztime=30s
go test -fuzz=FuzzAssembleWithKeygroups ./pkg/voicebuild/ -fuzztime=30s
go test -fuzz=FuzzEncode ./pkg/voiceimport/ -fuzztime=30s
go test -fuzz=FuzzExport ./pkg/sfzexport/ -fuzztime=30s
go test -fuzz=FuzzVoiceEncodeDecodeRoundTrip ./pkg/integration/ -fuzztime=30s
```

Format integrity fuzz tests verify that no combination of valid operations
can produce a corrupt file. Each drives up to 100 random operations and
asserts structural invariants after every step:

```sh
go test -fuzz=FuzzFZVEditChain ./pkg/voiceedit/ -fuzztime=60s
go test -fuzz=FuzzFZFMidiChain ./pkg/fzfmidi/ -fuzztime=60s
go test -fuzz=FuzzFZFOutputChain ./pkg/fzfoutput/ -fuzztime=60s
go test -fuzz=FuzzFZFEffectsChain ./pkg/fzfeffects/ -fuzztime=60s
go test -fuzz=FuzzBuildUnpackRoundTrip ./pkg/voiceunpack/ -fuzztime=60s
go test -fuzz=FuzzSFZConvertChaos ./pkg/sfzconvert/ -fuzztime=120s
go test -fuzz=FuzzDiskImageRoundTrip ./pkg/integration/ -fuzztime=120s
```

Property-based fuzz tests verify round-trip correctness, range bounds, and
structural invariants across the codebase:

- `pkg/disk/`: envelope display conversions (range, round-trip, monotonicity),
  `DirEntry`/`DisSector` encode-decode round-trip, `PadLabel`/`TrimPadded`
  round-trip, `SectorsNeeded`/`PadToSector` equivalence, `VoiceSlotOffset`
  ordering, `ForEachSamplePointer` preservation, `DecodeDisSector` crash
  resistance, `RateIndexFor`/`SampleRate` round-trip
- `pkg/wav/`: write-then-read round-trip, write-then-read round-trip with
  SMPL chunks
- `pkg/fzutil/`: resample identity, resample never-extrapolates,
  `VoiceName` bounds
- `pkg/sfzconvert/`: region-to-voice conversion with varied key ranges and
  sample data
- `pkg/integration/`: voice encode-then-decode round-trip

Run any of them with:

```sh
go test -fuzz=FuzzDirEntryRoundTrip ./pkg/disk/ -fuzztime=30s
go test -fuzz=FuzzRateIndexRoundTrip ./pkg/disk/ -fuzztime=30s
go test -fuzz=FuzzWriteReadRoundTrip ./pkg/wav/ -fuzztime=30s
go test -fuzz=FuzzResampleNeverExtrapolates ./pkg/fzutil/ -fuzztime=30s
go test -fuzz=FuzzVoiceEncodeDecodeRoundTrip ./pkg/integration/ -fuzztime=30s
```

### Fixtures

Real hardware disk images live in `testdata/`: `HOOVER.img`, `STAB.img`,
`TECHNO.img`, `BRASS.img` (13 voice multi-bank brass patch),
`PAD-LFO.img` (single pad voice with LFO on filter), plus `JUNGLISM.sfz`
with 28 WAV samples. These are used by both package integration tests and
CLI integration tests.

The `docs/` directory contains the FZ-1 data structures specification
(`casio-fz1-format.md`) and the original Casio R&D reference
(`casio-fz1-data-structures.pdf`).

## Code style

- Run `goimports` on all Go files
- Follow standard Go conventions
- No comments unless they add meaningful context
- Use existing constants from `pkg/disk` for binary format offsets
- Use `pkg/fileutil.WriteAtomic` for all file output

## Separating computation from presentation

Packages that display information to the user should separate parsing from
rendering. The established pattern is:

- `Parse(path) (*Result, error)` reads a file and returns a typed struct
- `Render(w io.Writer, result *Result)` formats the struct for terminal output
- `Info(path, w)` or `List(path, w)` is a convenience wrapper composing both

This pattern is used in `fzfinfo`, `fzvinfo`, and `disklist`. The `fzfmidi`
package uses a similar structure (`Set`/`Render`) but is a side-effect command
rather than a display command.

Benefits:
- Tests can assert on struct fields instead of parsing rendered text
- Programmatic callers can use `Parse` without terminal output
- Rendering changes don't require updating domain logic tests

When writing new display commands, follow this pattern. When writing
side-effect commands (disk add, disk format, voice import), returning only
`error` is fine because those don't produce structured results.

## File type constants

Use the `disk.FileType` type for all file type values. The typed constants
(`disk.TypeFullDump`, `disk.TypeVoice`, etc.) prevent accidental misuse of raw
integer values. When decoding from raw bytes, use an explicit conversion:
`disk.FileType(b)`.

## Test conventions

- Use `t.Parallel()` on all tests except those that call `testutil.CaptureLog`
  (which mutates the global logger)
- Use `t.TempDir()` for all temporary file paths (never hardcode `/tmp/`)
- Use `t.Cleanup()` to restore any global state mutations
- Use `filepath.Join()` for all paths (never hardcode `/` separators)
- Standard library assertions only (no testify)
- Binary-executing tests use `runFizzle()` / `mustRun()` / `mustFail()` helpers
  from `cli_test.go`

