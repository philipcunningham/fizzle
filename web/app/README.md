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
