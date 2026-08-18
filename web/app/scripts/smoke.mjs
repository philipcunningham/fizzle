// Headless smoke over the real WASM core behind the mockup-derived
// UI: disks round trip byte identical, every editing surface commits
// through the core, the placement matrix places, and a split pair
// reopens in either order. Fails on any console error. Run
// `make wasm` first; uses the locally installed Chrome channel.
import { execSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "playwright";
import { preview } from "vite";

const FIXTURE = new URL("../../../testdata/synthetic/TECHNO.img", import.meta.url).pathname;
const REPO = new URL("../../..", import.meta.url).pathname;
const IMAGE_SIZE = 1_310_720;

const server = await preview({ preview: { port: 4519 } });
const errors = [];
let failed = false;

const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage();
page.on("console", (msg) => {
  if (msg.type() === "error") errors.push(msg.text());
});
page.on("pageerror", (err) => errors.push(String(err)));

const step = async (name, fn) => {
  try {
    await fn();
    console.log(`ok   ${name}`);
  } catch (err) {
    console.log(`FAIL ${name}: ${err}`);
    failed = true;
  }
};

const sha256 = (buf) => createHash("sha256").update(buf).digest("hex");

// A minimal 16-bit mono PCM WAV: RIFF header plus a ramp.
const monoWav = (samples, rate) => {
  const data = Buffer.alloc(samples * 2);
  for (let i = 0; i < samples; i++) data.writeInt16LE((i % 199) * 3, i * 2);
  const header = Buffer.alloc(44);
  header.write("RIFF", 0);
  header.writeUInt32LE(36 + data.length, 4);
  header.write("WAVEfmt ", 8);
  header.writeUInt32LE(16, 16);
  header.writeUInt16LE(1, 20); // PCM
  header.writeUInt16LE(1, 22); // mono
  header.writeUInt32LE(rate, 24);
  header.writeUInt32LE(rate * 2, 28);
  header.writeUInt16LE(2, 32);
  header.writeUInt16LE(16, 34);
  header.write("data", 36);
  header.writeUInt32LE(data.length, 40);
  return Buffer.concat([header, data]);
};

const exportDownloads = async (count) => {
  const downloads = [];
  const listener = (d) => downloads.push(d);
  page.on("download", listener);
  await page.getByRole("button", { name: "Export", exact: true }).click();
  const deadline = Date.now() + 15000;
  while (downloads.length < count && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  page.off("download", listener);
  if (downloads.length < count) {
    throw new Error(`expected ${count} downloads, got ${downloads.length}`);
  }
  const out = [];
  for (const d of downloads) {
    const path = join(tmpdir(), `fizzle-smoke-${Date.now()}-${d.suggestedFilename()}`);
    await d.saveAs(path);
    out.push({ name: d.suggestedFilename(), bytes: readFileSync(path), path });
  }
  return out;
};

// Opens images or FZ files through the catch-all picker, discarding
// unexported changes when the guard dialog intervenes.
const pickFiles = async (files) => {
  await page.getByLabel("fz files").setInputFiles(files);
  const discard = page.getByRole("button", { name: "Discard" });
  try {
    await discard.waitFor({ timeout: 1500 });
    await discard.click();
  } catch {
    /* no guard: the disk was clean */
  }
};

await page.goto("http://localhost:4519/");

await step("the start screen renders with no disk", async () => {
  await page.getByRole("button", { name: "New disk" }).waitFor({ timeout: 10000 });
  const exportCount = await page.getByRole("button", { name: "Export" }).count();
  if (exportCount !== 0) throw new Error("export visible with no disk");
});

await step("a new disk arrives through the dialog (WASM core)", async () => {
  await page.getByRole("button", { name: "New disk" }).click();
  await page.getByLabel("disk label").fill("SMOKE");
  await page.getByRole("button", { name: "Create" }).click();
  await page.getByText("[SMOKE]").waitFor({ timeout: 10000 });
});

await step("a fresh disk exports at image size with its label", async () => {
  const [image] = await exportDownloads(1);
  if (image.bytes.length !== IMAGE_SIZE) throw new Error(`export size ${image.bytes.length}`);
  if (!image.bytes.subarray(0, 5).equals(Buffer.from("SMOKE"))) {
    throw new Error("exported image does not carry the label");
  }
});

await step("a WAV converts into the instrument (WASM core)", async () => {
  const wavPath = join(tmpdir(), "fizzle-smoke-kick.wav");
  writeFileSync(wavPath, monoWav(3000, 18000));
  await pickFiles([wavPath]);
  await page.getByText("Import 1 WAV").waitFor({ timeout: 5000 });
  await page.getByRole("button", { name: "Convert" }).click();
  await page.getByText("Voices (1/64)").waitFor({ timeout: 10000 });
  await page.getByText("FIZZLE SMOK", { exact: false }).first().waitFor({ timeout: 5000 });
});

await step("schema editing round trips with undo (WASM core)", async () => {
  const cutoff = page.getByRole("slider", { name: "Cutoff" });
  await cutoff.waitFor({ timeout: 5000 });
  const before = await cutoff.getAttribute("aria-valuenow");

  await cutoff.focus();
  await page.keyboard.press("ArrowDown");
  await page.waitForFunction(
    (prev) =>
      document.querySelector('[aria-label="Cutoff"]')?.getAttribute("aria-valuenow") !== prev,
    before,
    { timeout: 5000 },
  );
  const edited = await cutoff.getAttribute("aria-valuenow");
  if (Number(edited) !== Number(before) - 1) {
    throw new Error(`cutoff ${before} then ${edited}, want one step down`);
  }

  await page.getByRole("button", { name: "Undo" }).click();
  await page.waitForFunction(
    (prev) =>
      document.querySelector('[aria-label="Cutoff"]')?.getAttribute("aria-valuenow") === prev,
    before,
    { timeout: 5000 },
  );

  // A select edit through the core: playback mode via the Radix menu.
  await page.getByRole("combobox", { name: "Playback" }).click();
  await page.getByRole("option", { name: "reverse" }).click();
  await page.waitForFunction(
    () =>
      document.querySelector('[aria-label="Playback"]')?.textContent?.includes("reverse") ?? false,
    undefined,
    { timeout: 5000 },
  );
});

await step("the keyboard auditions the focused voice (WASM core)", async () => {
  const key = page.locator('[data-testid="key-48"]');
  await key.waitFor({ timeout: 5000 });
  await key.dispatchEvent("pointerdown", { clientY: 10, pointerId: 1 });
  await page.locator(".keyboardbar[data-auditioning]").waitFor({ timeout: 5000 });
  await key.dispatchEvent("pointerup", { pointerId: 1 });
  await page.locator(".keyboardbar[data-auditioning]").waitFor({ state: "hidden", timeout: 5000 });
});

await step("the keyboard plays from the keyboard alone (Q5)", async () => {
  const key = page.locator('[data-testid="key-50"]');
  await key.focus();
  await page.keyboard.down("Enter");
  await page.locator(".keyboardbar[data-auditioning]").waitFor({ timeout: 5000 });
  await page.keyboard.up("Enter");
  await page.locator(".keyboardbar[data-auditioning]").waitFor({ state: "hidden", timeout: 5000 });
});

await step("waveform, loops, and envelopes edit over the WASM core", async () => {
  const canvas = page.locator('[data-testid="waveform"] canvas').first();
  await canvas.waitFor({ timeout: 5000 });
  const canvasPainted = await canvas.evaluate((el) => {
    const ctx = el.getContext("2d");
    const data = ctx.getImageData(0, 0, el.width, el.height).data;
    return data.some((v) => v !== 0);
  });
  if (!canvasPainted) throw new Error("waveform canvas is blank");

  // A numeric loop edit round trips the frames the core confirmed,
  // and an end past the voice clamps to its frame count.
  const start = page.getByLabel("loop 1 start");
  await start.fill("400");
  await start.press("Enter");
  await page.waitForFunction(
    () => document.querySelector('[aria-label="loop 1 start"]')?.value === "400",
    undefined,
    { timeout: 5000 },
  );
  const end = page.getByLabel("loop 1 end");
  await end.fill("9999999");
  await end.press("Enter");
  await page.waitForFunction(
    () => {
      const v = document.querySelector('[aria-label="loop 1 end"]')?.value;
      return v !== "9999999" && Number(v) > 0;
    },
    undefined,
    { timeout: 5000 },
  );

  // R17's drag affordance. An imported voice's loop 1 starts with its
  // start equal to its end, and wavesurfer styles a zero-width region
  // as a bare marker with no resize handles. Widening it has to give
  // back a filled, resizable region, or the loop cannot be dragged.
  const region = await page.evaluate(() => {
    const host = document.querySelector('[data-testid="waveform"] div');
    const el = [...(host?.shadowRoot?.querySelectorAll("[part]") ?? [])].find((n) =>
      /^region region-/.test(n.getAttribute("part")),
    );
    if (!el) return { found: false };
    return {
      found: true,
      background: getComputedStyle(el).backgroundColor,
      handles: el.querySelectorAll('[part*="region-handle"]').length,
    };
  });
  if (!region.found) throw new Error("no loop region on the waveform");
  if (region.background === "rgba(0, 0, 0, 0)") {
    throw new Error("the loop region has no fill, so it is invisible (R17)");
  }
  if (region.handles !== 2) {
    throw new Error(`loop region has ${region.handles} resize handles, want 2 (R17)`);
  }

  // Loop attributes: cross-fade and time land in the dump (R14).
  const xf = page.getByLabel("loop 1 crossfade");
  await xf.fill("512");
  await xf.press("Enter");
  await page.waitForFunction(
    () => document.querySelector('[aria-label="loop 1 crossfade"]')?.value === "512",
    undefined,
    { timeout: 5000 },
  );

  // An envelope stage edit through the grid round trips, then undoes.
  const stop = page.getByLabel("DCA envelope stage 2 level");
  await stop.fill("55");
  await stop.press("Enter");
  await page.waitForFunction(
    () => document.querySelector('[aria-label="DCA envelope stage 2 level"]')?.value === "55",
    undefined,
    { timeout: 5000 },
  );
  await page.getByRole("button", { name: "Undo" }).click();
  await page.waitForFunction(
    () => document.querySelector('[aria-label="DCA envelope stage 2 level"]')?.value !== "55",
    undefined,
    { timeout: 5000 },
  );
});

await step("R14's Sample group reads and edits over the WASM core", async () => {
  // Sample rate is a schema select, so it reaches the screen through
  // the same path every other schema control takes. If the schema is
  // doing its job this needs no code of its own.
  await page.getByRole("combobox", { name: "Sample rate (Hz)" }).click();
  await page.getByRole("option", { name: "9000" }).click();
  await page.waitForFunction(
    () =>
      document.querySelector('[aria-label="Sample rate (Hz)"]')?.textContent?.includes("9000") ??
      false,
    undefined,
    { timeout: 5000 },
  );

  // The generation window is bespoke, like the loops: its bounds are
  // the voice's own frame count, so a schema range could not carry it.
  // The cells must read the frames the core parsed out of the dump.
  const genStart = await page.getByLabel("generation start").inputValue();
  const genEnd = await page.getByLabel("generation end").inputValue();
  if (!/^\d+$/.test(genStart) || !/^\d+$/.test(genEnd)) {
    throw new Error(`generation window reads ${genStart}..${genEnd}, want frames from the core`);
  }
  if (Number(genEnd) <= Number(genStart)) {
    throw new Error(`generation window reads ${genStart}..${genEnd}, want an end past the start`);
  }
});

await step("an invalid image is rejected with an error envelope", async () => {
  const bad = join(tmpdir(), "fizzle-smoke-bad.img");
  writeFileSync(bad, Buffer.alloc(16));
  await page.getByLabel("fz files").setInputFiles(bad);
  const alert = page.getByRole("alert");
  await alert.first().waitFor({ timeout: 5000 });
  const text = await alert.first().textContent();
  if (!text?.includes("invalid-image")) throw new Error(`alert says: ${text}`);
  await page.getByRole("button", { name: "dismiss error" }).click();
});

await step("a corpus image round trips byte identical", async () => {
  await pickFiles([FIXTURE]);
  await page.getByText("[Techno Split]").waitFor({ timeout: 10000 });
  const [out] = await exportDownloads(1);
  const source = readFileSync(FIXTURE);
  if (sha256(out.bytes) !== sha256(source)) throw new Error("round trip differs from source");
});

await step("banks parse and a velocity switch builds (WASM core)", async () => {
  await page.getByRole("tab", { name: "Banks and Areas" }).click();
  const table = page.getByRole("table", { name: "areas" });
  await table.waitFor({ timeout: 5000 });
  const before = await table.locator("tbody tr").count();
  if (before === 0) throw new Error("no areas parsed from the corpus dump");

  await page.getByRole("button", { name: "duplicate area 1", exact: true }).click();
  await page.waitForFunction(
    (n) => document.querySelectorAll('table[aria-label="areas"] tbody tr').length === n + 1,
    before,
    { timeout: 10000 },
  );

  // Split the velocity layers through the edit panel steppers.
  await table.locator("tbody tr").first().click();
  const velHigh = page.getByLabel("Vel high", { exact: true });
  await velHigh.fill("64");
  await velHigh.press("Enter");
  await page.waitForFunction(
    () =>
      document.querySelector('table[aria-label="areas"] tbody tr')?.textContent?.includes("..64") ??
      false,
    undefined,
    { timeout: 5000 },
  );
});

await step("the effects matrix edits over the WASM core", async () => {
  await page.getByRole("tab", { name: "Effects" }).click();
  const cell = page.getByRole("spinbutton", { name: "Mod wheel to LFO pitch" });
  await cell.waitFor({ timeout: 5000 });
  const before = await cell.getAttribute("aria-valuenow");
  await cell.focus();
  // A cell already at the clamp only moves the other way.
  await page.keyboard.press(Number(before) >= 127 ? "ArrowDown" : "ArrowUp");
  await page.waitForFunction(
    (prev) =>
      document
        .querySelector('[aria-label="Mod wheel to LFO pitch"]')
        ?.getAttribute("aria-valuenow") !== prev,
    before,
    { timeout: 5000 },
  );

  const bend = page.getByLabel("Bend range (1/8 semi)", { exact: true });
  const initialBend = await bend.inputValue();
  const targetBend = initialBend === "24" ? "25" : "24";
  await bend.fill(targetBend);
  await bend.press("Enter");
  await page.waitForFunction(
    (want) => document.querySelector('[aria-label="Bend range (1/8 semi)"]')?.value === want,
    targetBend,
    { timeout: 5000 },
  );
  await page.getByRole("button", { name: "Undo" }).click();
  await page.waitForFunction(
    (want) => document.querySelector('[aria-label="Bend range (1/8 semi)"]')?.value === want,
    initialBend,
    { timeout: 5000 },
  );
});

await step("rename, extract, and delete flow through the core", async () => {
  // Rename the disk from the topbar.
  await page.getByText("[Techno Split]").dblclick();
  const label = page.getByLabel("disk label");
  await label.fill("RENAMED");
  await label.press("Enter");
  await page.getByText("[RENAMED]").waitFor({ timeout: 5000 });

  // Extract a voice as .fzv through the export dialog (R18).
  await page.getByRole("tab", { name: "Voices" }).click();
  await page.getByRole("table", { name: "instrument voices" }).waitFor({ timeout: 5000 });
  const downloads = [];
  const listener = (d) => downloads.push(d);
  page.on("download", listener);
  await page
    .getByRole("table", { name: "instrument voices" })
    .getByRole("button", { name: /export/ })
    .first()
    .click();
  await page.getByRole("button", { name: "As .fzv" }).click();
  const deadline = Date.now() + 10000;
  while (downloads.length < 1 && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  page.off("download", listener);
  if (downloads.length < 1) throw new Error("no voice download arrived");
  if (!downloads[0].suggestedFilename().endsWith(".fzv")) {
    throw new Error(`voice saved as ${downloads[0].suggestedFilename()}`);
  }
});

await step("the placement matrix places FZ files (WASM core)", async () => {
  await page.getByRole("tab", { name: "Voices" }).click();
  await page.getByRole("table", { name: "instrument voices" }).waitFor({ timeout: 5000 });
  const wavPath = join(tmpdir(), "smoke extra.wav");
  writeFileSync(wavPath, monoWav(2000, 18000));
  const fzvPath = join(tmpdir(), "smoke-extra.fzv");
  execSync(`go run ./cmd/fizzle fzv import "${wavPath}" "${fzvPath}" --rate 18000`, { cwd: REPO });
  const fzfPath = join(tmpdir(), "smoke-one.fzf");
  execSync(`go run ./cmd/fizzle fzf build "${fzfPath}" "${fzvPath}"`, { cwd: REPO });
  const fzbPath = join(tmpdir(), "smoke-one.fzb");
  writeFileSync(fzbPath, readFileSync(fzfPath).subarray(0, 2048));

  // The .fzv joins the voice list (R7).
  const voiceRows = page.locator('table[aria-label="instrument voices"] tbody tr');
  const voicesBefore = await voiceRows.count();
  await pickFiles([fzvPath]);
  await page.waitForFunction(
    (n) =>
      document.querySelectorAll('table[aria-label="instrument voices"] tbody tr').length === n + 1,
    voicesBefore,
    { timeout: 10000 },
  );

  // The .fzb offers the next bank slot and joins (R7).
  await pickFiles([fzbPath]);
  await page.getByRole("button", { name: /Add as bank|Replace bank/ }).click();
  await page
    .getByRole("button", { name: /Add as bank|Replace bank/ })
    .waitFor({ state: "hidden", timeout: 10000 });

  // The .fzf prompts, then replaces the instrument (R7).
  await pickFiles([fzfPath]);
  await page.getByRole("button", { name: "Replace the instrument" }).click();
  await page.waitForFunction(
    () => document.querySelectorAll('table[aria-label="instrument voices"] tbody tr').length === 1,
    undefined,
    { timeout: 10000 },
  );
});

// The two disk pair the next steps hand back and forth.
let pairFiles = null;

await step("an oversized SFZ splits across two disks (WASM core)", async () => {
  const files = [];
  const sfzLines = [];
  for (let i = 0; i < 3; i++) {
    const name = `long0${i}.wav`;
    files.push({ name, mimeType: "audio/wav", buffer: monoWav(300000, 18000) });
    sfzLines.push(
      `<region> sample=${name} lokey=${36 + i} hikey=${36 + i} pitch_keycenter=${36 + i}`,
    );
  }
  files.push({
    name: "big.sfz",
    mimeType: "application/octet-stream",
    buffer: Buffer.from(sfzLines.join("\n")),
  });

  await page.getByLabel("fz files").setInputFiles(files);
  await page.getByText("SFZ conversion").waitFor({ timeout: 5000 });
  await page.getByRole("radio", { name: "18 kHz" }).click();
  await page.getByRole("button", { name: "Split across two disks" }).click();
  await page.getByText("two disk set").waitFor({ timeout: 30000 });

  const downloads = await exportDownloads(2);
  const names = downloads.map((f) => f.name).sort();
  if (!names[0].endsWith("-1.img") || !names[1].endsWith("-2.img")) {
    throw new Error(`pair names: ${names.join(", ")}`);
  }
  pairFiles = downloads;
});

await step("two images open in either order as one instrument (R5)", async () => {
  if (!pairFiles) throw new Error("no pair from the split step");
  const sorted = [...pairFiles].sort((a, b) => (a.name < b.name ? -1 : 1));
  await pickFiles([sorted[1].path, sorted[0].path]);
  await page.getByText("two disk set").waitFor({ timeout: 15000 });
  await page.waitForFunction(
    () => document.querySelectorAll('table[aria-label="instrument voices"] tbody tr').length === 3,
    undefined,
    { timeout: 10000 },
  );
});

await browser.close();
await server.close();

if (errors.length > 0 || failed) {
  if (errors.length > 0) console.error(`console errors:\n${errors.join("\n")}`);
  process.exit(1);
}
console.log("smoke passed: the mockup UI drives the WASM core, byte identical, console clean");
