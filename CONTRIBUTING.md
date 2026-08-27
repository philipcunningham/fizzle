# Contributing to fizzle

Bug reports should include reproduction steps and the smallest shareable `.img` or `.fzf` fixture. New behavior needs regression coverage.

## Setup

The pinned toolchain requires Go 1.26.5, Node 22 with npm, golangci-lint 2.12.2, Vale 3.15.1, and Make.

```sh
git clone https://github.com/philipcunningham/fizzle
cd fizzle
make tools
make tools-check
cd web/app && npm install && cd ../..
make build
```

`make build` generates attribution files and embeds them with the `release` build tag. Plain Go builds and tests use attribution stubs.

## Validation

Run the complete non-mutating gate before submitting a change:

```sh
make check
```

Use `make check-fast` during development. Run `make fmt` only when source formatting should change.

The browser screenshot baselines are platform specific and stay outside `make check`. Run this command from `web/app` before shipping a visual change:

```sh
npm run visual
```

## Test placement

| Change | Test |
|---|---|
| Package behavior or edge case | Unit test beside the package |
| Multi-package data pipeline | Test in `pkg/integration/` |
| CLI command, flag, output, or exit status | Integration-tagged test in `pkg/integration/cli_test.go` |
| Binary parser behavior | Fuzz test beside the parser |
| Browser behavior | Test in `web/app/tests/` against the staged protocol stub |
| Browser and real core interaction | Browser smoke test |

Commands with JSON output need package coverage for serialization and CLI coverage for flag wiring.

Golden SHA-256 failures signal byte changes in conversion output. Update a checksum only after inspecting and explaining the changed bytes.

Run a fuzz target directly when changing its parser or invariant:

```sh
go test -fuzz=FuzzRead ./pkg/wav/ -fuzztime=30s
```

Hardware-corpus tests run when fixtures exist under `testdata/corpus`. The normal suite uses tracked synthetic and regression fixtures.

## Code conventions

- Follow standard Go conventions and run `goimports` on changed Go files.
- Use constants from `pkg/disk` for binary offsets and `disk.FileType` for file types.
- Use `pkg/fileutil.WriteAtomic` for file output.
- Parse into typed results before rendering user-facing output.
- Accept `io.Writer` at rendering boundaries.
- Return only an error from side-effect commands that have no structured result.

## Test conventions

- Use `t.Parallel()` unless a test mutates shared process state.
- Use `t.TempDir()` and `filepath.Join()` for temporary paths.
- Restore global state with `t.Cleanup()`.
- Use standard library assertions.
- Use the shared CLI helpers in `pkg/integration/cli_test.go` for binary tests.
