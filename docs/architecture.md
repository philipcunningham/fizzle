# Architecture

Fizzle keeps binary-format decisions in a small domain core and treats the CLI,
WASM layer, and React application as adapters. One validated document owns both
bytes and the evidence used to interpret them. Callers don't independently
guess bank, voice, or audio boundaries.

```text
CLI commands             WASM protocol             React workflows
     |                         |                          |
filesystem adapters      webcore Session facade     strict protocol client
     |                         |                          |
diskfs storage           document.State transactions    view state only
                \             |             /
                 fzf.Document and bounded views
                              |
                  patches, disk bytes, WAV codecs
```

## Ownership

`pkg/fzf` owns full-dump construction, immutable resolved layouts, retained
voice-count authority, bounded bank and voice views, and byte-preserving
operations. `NewStandalone`, `NewDiskFile`, and `NewBankDump` make source
context explicit. A fixed operation is a stale-safe patch batch; a structural
operation is an explicit replacement with a checked preimage.

`pkg/document` owns the one or two disk aggregate. Pair opening, continuation
validation, stitching, replacement, splitting, collapse to one disk, and
disk-full refusal happen before a new immutable state is published. Failed
operations leave both input images byte-identical.

`pkg/diskfs` owns pure in-memory directory allocation and file operations.
Path I/O, locking, command rendering, and logging stay in adapter packages.

`pkg/webcore` is the browser boundary. `Session` retains revision, history,
gesture, error-envelope, and projection behavior. It installs a prepared
`document.State` only after parsing and derived snapshots succeed. Public
protocol behavior is covered independently from the domain operations it uses.

The React application owns selections, dialogs, persistence, audition, and
other view workflows. Binary mutation and capacity rules stay in Go. Component
tests stage protocol results explicitly; the real-WASM smoke suite covers
end-to-end document behavior.

## Executable boundaries

The architecture test rejects imports of retired command packages and raw
binary parsing in migrated projections. It also rejects retired layout-count
APIs and caps `session.go` at 500 lines. Protocol parity checks compare Go
registration, worker dispatch, and the
TypeScript contract. CLI reference generation walks the same command tree the
binary executes and fails when checked-in documentation drifts.

## Validation and fixtures

`make check-fast` is non-mutating and offline. It runs formatting, static
analysis, short race tests, protocol checks, a WASM compile, and fast frontend
checks. `make check-full`, also exposed as `make check`, runs the complete race,
integration, frontend, and real build gates. Hardware-corpus tests also run
when developers have supplied fixtures under `testdata/corpus`; normal builds
and CI neither download nor package that optional data. Git retains compact
synthetic and real-hardware regression images for offline work.
