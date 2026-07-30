// Does the per-edit snapshot rebuild make a drag stutter?
//
// The cost lives in the worker: every edit copies the image, re-parses
// the document, and ships a fresh snapshot back. So drive the real
// worker with the real WASM core, on a real corpus image, at the rate a
// drag produces edits, and time the round trips. Driving the UI instead
// would measure Playwright's CDP round trip (12 ms a move), which is
// slower than the thing under test.
import { readFileSync } from "node:fs";
import { chromium } from "playwright";
import { preview } from "vite";

const FIXTURE = new URL("../../../testdata/synthetic/TECHNO.img", import.meta.url).pathname;
// A real 2 MB dump: too big for one disk, so the document is a split
// pair and every edit also re-splits it.
const BIG_FZF = new URL(
  "../../../testdata/corpus/casio-fz-1-factory-library/casio-fz-sound-disk-fl-12/harp/Harp.fzf",
  import.meta.url,
).pathname;
const EDITS = 60;

const server = await preview({ preview: { port: 4527 } });
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage();
page.on("pageerror", (err) => {
  console.error("page error:", String(err));
});

await page.goto("http://localhost:4527/");
// The app's own worker asset, whatever this build named it.
const workerUrl = await page.evaluate(async () => {
  const html = await (await fetch("/index.html")).text();
  const entry = /assets\/index-[\w-]+\.js/.exec(html)?.[0];
  const js = await (await fetch(`/${entry}`)).text();
  const worker = /assets\/worker-[\w-]+\.js/.exec(js)?.[0];
  return worker ? `/${worker}` : null;
});
if (!workerUrl) throw new Error("no worker asset found in the build");

const image = [...readFileSync(FIXTURE)];
const bigFzf = [...readFileSync(BIG_FZF)];

const measure = (mode) =>
  page.evaluate(
    async ({ workerUrl, image, bigFzf, edits, mode }) => {
      const worker = new Worker(workerUrl, { type: "module" });
      let seq = 0;
      const pending = new Map();
      worker.onmessage = (event) => {
        const { id, result } = event.data;
        pending.get(id)?.(result);
        pending.delete(id);
      };
      const call = (method, payload) =>
        new Promise((resolve) => {
          const id = ++seq;
          pending.set(id, resolve);
          worker.postMessage({ id, method, payload });
        });

      // A 16-bit mono WAV of n frames, the smallest real import.
      const wav = (frames) => {
        const buf = new ArrayBuffer(44 + frames * 2);
        const view = new DataView(buf);
        const tag = (off, s) => {
          for (let i = 0; i < s.length; i++) view.setUint8(off + i, s.charCodeAt(i));
        };
        tag(0, "RIFF");
        view.setUint32(4, 36 + frames * 2, true);
        tag(8, "WAVEfmt ");
        view.setUint32(16, 16, true);
        view.setUint16(20, 1, true);
        view.setUint16(22, 1, true);
        view.setUint32(24, 18000, true);
        view.setUint32(28, 36000, true);
        view.setUint16(32, 2, true);
        view.setUint16(34, 16, true);
        tag(36, "data");
        view.setUint32(40, frames * 2, true);
        for (let i = 0; i < frames; i++) view.setInt16(44 + i * 2, (i % 157) * 200, true);
        return new Uint8Array(buf);
      };

      let voices;
      let disks = 1;
      if (mode === "split") {
        const loaded = await call("loadFzf", { buffer: new Uint8Array(bigFzf).buffer });
        if (!loaded.ok) throw new Error(`loadFzf: ${loaded.error.code}`);
        voices = loaded.value.disk.instrument.voices.length;
        disks = loaded.value.disk.disks;
        if (disks !== 2) throw new Error("the dump did not split");
      } else if (mode === "corpus") {
        const bytes = new Uint8Array(image);
        const opened = await call("openImage", bytes.buffer);
        if (!opened.ok) throw new Error(`openImage: ${opened.error.code}`);
        voices = opened.value.disk.instrument.voices.length;
      } else {
        const made = await call("newDisk", "PERF");
        if (!made.ok) throw new Error(`newDisk: ${made.error.code}`);
        let last;
        for (let i = 0; i < 64; i++) {
          last = await call("importWavToInstrument", {
            filename: `V${String(i).padStart(2, "0")}.wav`,
            buffer: wav(7000),
            rate: 18000,
            channel: "mix",
          });
          if (!last.ok) throw new Error(`importWav ${String(i)}: ${last.error.code}`);
        }
        voices = last.value.disk.instrument.voices.length;
      }

      await call("beginGesture");
      // Warm: the first call through a path pays for its own JIT.
      await call("setSlotParamNumber", { slot: 0, field: "cutoff", value: 40 });

      const times = [];
      for (let i = 0; i < edits; i++) {
        const started = performance.now();
        const r = await call("setSlotParamNumber", {
          slot: 0,
          field: "cutoff",
          value: 40 + (i % 60),
        });
        times.push(performance.now() - started);
        if (!r.ok) throw new Error(`edit: ${r.error.code}`);
      }
      await call("commitGesture");
      worker.terminate();

      times.sort((a, b) => a - b);
      return {
        voices,
        disks,
        median: times[Math.floor(times.length / 2)],
        worst: times.at(-1),
        total: times.reduce((a, b) => a + b, 0),
      };
    },
    { workerUrl, image, bigFzf, edits: EDITS, mode },
  );

for (const mode of ["corpus", "full", "split"]) {
  const result = await measure(mode);
  console.log(
    `${mode.padEnd(7)} ${String(result.voices).padStart(2)} voices, ${String(result.disks)} disk(s):  ` +
      `${result.median.toFixed(2)} ms median, ${result.worst.toFixed(2)} ms worst, ` +
      `${result.total.toFixed(0)} ms for ${String(EDITS)} edits`,
  );
}
console.log(`frame budget:       16.7 ms at 60 Hz`);

await browser.close();
await server.close();
