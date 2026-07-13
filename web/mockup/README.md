# fizzle Web UI mockup

Step 0 of the Web UI plan: an interactive, throwaway mockup over
canned data. No Go, no WASM, nothing byte correct. The specification,
plan, walkthrough, and decision log live in `handoffs/` at the
repository root.

## Run it

```sh
npm install
npm run dev
```

Open the printed localhost URL in Chromium. `npm run build` then
`npm run preview` serves the static bundle instead. `npm run smoke`
drives the built app through a headless click path and fails on any
console error. `npm run shots` refreshes the screenshots.
