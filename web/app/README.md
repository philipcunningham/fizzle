# fizzle Web UI

## Development

```sh
make wasm
cd web/app
npm install
npm run dev
```

Run `make wasm` from the repository root. Use a desktop Chromium browser.

`npm run check` covers formatting, lint, types, unit tests, build output, and payload size. Run `npm run smoke` against the real core.

Run `npm run visual` before shipping UI changes because its screenshot baselines are platform specific and absent from CI.

## Protocol

`../protocol/methods.json` defines each method's request, response, and transferable values. Update it when the boundary changes, then regenerate its method union:

```sh
node scripts/check-protocol.mjs --write
```

TypeScript checks the manifest against public interfaces and worker dispatch. Go tests check registered WASM methods against the same manifest.

## State boundaries

`useDocumentSession` owns revisions, snapshots, dirty state, history gestures, fatal core errors, persistence, and unload protection.

`fileio` owns browser reads, saves, and emergency export. `App` composes these workflows with UI state.

Use `createCoreStub` for component tests with staged responses. Reserve the behavioral fake for workflows that require long document sequences.
