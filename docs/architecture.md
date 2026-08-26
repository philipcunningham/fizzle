# Architecture

Fizzle keeps binary-format decisions in a small domain core and treats the CLI,
WASM layer, and React application as adapters. One validated document owns both
bytes and the evidence used to interpret them. Callers do not independently
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

The architecture test prevents application and domain packages from importing
retired command packages, prevents migrated projections from returning to raw
binary parsing, rejects retired layout-count APIs, and caps `session.go` at 500
lines. Protocol parity checks compare Go registration, worker dispatch, and the
TypeScript contract. CLI reference generation walks the same command tree the
binary executes and fails when checked-in documentation drifts.

## Validation and fixtures

`make check-fast` is non-mutating and offline. It runs formatting, static
analysis, short race tests, protocol checks, a WASM compile, and fast frontend
checks. `make check-full`, also exposed as `make check`, installs the pinned
hardware corpus and runs the complete race, integration, corpus, frontend, and
real build gates.

The 251 MiB real-hardware corpus is a separately versioned release archive.
`make corpus` caches it, verifies its pinned SHA-256 digest, and rejects unsafe
archive paths. Git retains compact synthetic and real-hardware regression
images for offline work. The full corpus checks construction, layout bounds,
authority, bounded access, byte-preserving reads, targeted mutations, and
stable command projections.

## Issue 39 completion evidence

| Criterion | Implementation and executable evidence |
| --- | --- |
| One retained layout and provenance | `pkg/fzf` constructors, layout tests, corpus authority digests |
| Bank dump and reader migration | `NewBankDump`; reader view tests for FZB, FZF, FZV, disk add, and webcore |
| Atomic mutation contract | `fzf.OperationResult`, `document.State`, stale-preimage and rollback tests |
| Reusable operations | Domain packages and dependency guards excluding webcore and command packages |
| Split and multi-disk transactions | Pair, replace, split, collapse, malformed continuation, and disk-full tests |
| Thin browser facade | Focused webcore files plus the enforced 500-line `session.go` limit |
| Bounded binary access | Bank, area, voice, DIS, and bank-dump view tests plus projection guards |
| Storage separation | Pure `pkg/diskfs` operations and adapter-level path and lock handling |
| Boundary drift failure | Go, WASM, worker, and TypeScript protocol parity tests |
| No frontend editor duplicate | Strict protocol stubs, frontend import guards, real-WASM smoke suite |
| Reproducible validation | Pinned tools, non-mutating `fmt-check`, fast and full gates |
| Maintainable integration tests | Workflow files, central manifest lookup, corpus invariants |
| Complete CLI documentation | Generated `docs/cli-reference.md` and regeneration test |
| Sustainable corpus distribution | Pinned release archive, verified installer tests, CI cache |
