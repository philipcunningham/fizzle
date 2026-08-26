# fizzle Web UI

The React and TypeScript front end over the Go core compiled to
WebAssembly.

## Run it

```sh
make wasm      # from the repository root: builds the browser core
cd web/app
npm install
npm run dev
```

Open the printed localhost URL in Chromium. Create a disk or open an
`.img`, import WAVs as voices, then export it.

## Checks

`npm run check` chains format, lint, types, unit tests, build, and the
payload budget. `npm run smoke` drives the built app through a real
browser and verifies a corpus image round trips byte identical.
`make web-check` from the repository root runs the WASM build plus the
check chain; `make check` includes it.

`npm run visual` compares per-platform screenshot baselines. CI skips
it, so run it by hand before shipping a UI change.

## Core protocol

`../protocol/methods.json` is the browser boundary manifest. It names every
method plus its request, response, and transferable shape. Change that file
when adding or removing a capability, then run:

```sh
node scripts/check-protocol.mjs --write
```

The normal check rejects a stale generated method union. TypeScript proves
that the manifest, public `Core`, raw WASM surface, and worker dispatch are
complete. The Go parity test proves that the module registers exactly the
same methods. Payload conversion remains explicit in the worker because it
owns structured-clone and transferable behavior.

## Shell boundaries

`useDocumentSession` owns revision-driven snapshots, dirty state, history
gestures, fatal core errors, sampler-memory persistence, and unload protection.
Its caller callbacks may be inline: callback identity doesn't restart session
boot work.
`fileio` owns browser reads, saves, and emergency export. `App` composes those
workflows with selection, dialogs, audition, and rendering.

Component tests that only need staged boundary answers should use
`createCoreStub`. An unstaged call returns an `unstaged-call` error, so tests do
not acquire new disk or instrument rules accidentally. The stub is safe to
await, and object spread preserves its fallback. The behavioral fake is
reserved for integration-style editor workflows that exercise a long sequence
of document changes.
