// Headless smoke over the real WASM core: disks round trip byte
// identical, every editing surface commits through the core, the
// placement matrix places, and a split pair reopens in either order.
// Fails on any console error. Run `make wasm` first; uses the locally
// installed Chrome channel.
import { execSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "playwright";
import { preview } from "vite";
import { makeCommitField, makeRegionFill } from "./pagehelpers.mjs";

const FIXTURE = new URL("../../../testdata/synthetic/TECHNO.img", import.meta.url).pathname;
const LOOPDEMO = new URL("../../../testdata/synthetic/LOOPDEMO.img", import.meta.url).pathname;
const REPO = new URL("../../..", import.meta.url).pathname;
const IMAGE_SIZE = 1_310_720;

const server = await preview({ preview: { port: 4519 } });
const errors = [];
let failed = false;

const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ reducedMotion: "reduce" });
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

const regionFill = makeRegionFill(page);

// The same, once the strip has a region: switching the drawn loop
// removes it and remakes it, and a read in the gap sees nothing.
const regionFillWhenDrawn = async () => {
  await page.waitForFunction(
    () => {
      const host = document.querySelector('[data-testid="waveform"] div');
      return [...(host?.shadowRoot?.querySelectorAll("[part]") ?? [])].some((n) =>
        /^region region-/.test(n.getAttribute("part")),
      );
    },
    undefined,
    { timeout: 5000 },
  );
  return regionFill();
};

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

// A press that follows an uncommitted edit plays the document as it
// was, so every editing step commits through here.
const commitField = makeCommitField(page);

const fieldFrames = (label) =>
  page.evaluate((l) => Number(document.querySelector(`[aria-label="${l}"]`)?.value), label);

const voiceRate = async () => {
  const text = await page.getByRole("combobox", { name: "Sample rate (Hz)" }).textContent();
  const rate = Number(/\d+/.exec(text ?? "")?.[0]);
  if (!Number.isFinite(rate)) throw new Error(`the sample rate reads ${text}`);
  return rate;
};

// A voice's samples decode off the main thread, so a press can land
// before the payload does and sound nothing.
const pressUntilSounding = async (key) => {
  const deadline = Date.now() + 15000;
  for (;;) {
    await key.dispatchEvent("pointerdown", { clientY: 10, pointerId: 1 });
    try {
      await page.locator(".keyboardbar[data-auditioning]").waitFor({ timeout: 1000 });
      return;
    } catch (err) {
      await key.dispatchEvent("pointerup", { pointerId: 1 });
      if (Date.now() > deadline) throw err;
    }
  }
};

// The loop window the source node carried while the key was down, and
// again once it came up. Two readings, or a window already set at note
// on reads as one that moved.
const acrossAKey = async (testid = "key-48") => {
  await page.evaluate(() => {
    window.__start = AudioBufferSourceNode.prototype.start;
    AudioBufferSourceNode.prototype.start = function (...args) {
      window.__node = this;
      return window.__start.apply(this, args);
    };
  });
  const read = () =>
    page.evaluate(() => ({
      on: window.__node.loop,
      start: window.__node.loopStart,
      end: window.__node.loopEnd,
    }));
  try {
    const key = page.locator(`[data-testid="${testid}"]`);
    await pressUntilSounding(key);
    const held = await read();
    await key.dispatchEvent("pointerup", { pointerId: 1 });
    return { held, freed: await read() };
  } finally {
    await page.evaluate(() => {
      AudioBufferSourceNode.prototype.start = window.__start;
      delete window.__start;
      delete window.__node;
    });
  }
};

const isWindow = (got, [start, end], rate) =>
  got.on && Math.abs(got.start - start / rate) < 1e-6 && Math.abs(got.end - end / rate) < 1e-6;

const showWindow = (got) => `${got.on ? "" : "unlooped "}${got.start} to ${got.end}`;

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

await step("a held key repeats the voice's sustain loop (WASM core)", async () => {
  // Loop 1 takes bounds the assertion can recognise, then the select
  // makes it the sustain loop. The caption is the core's answer coming
  // back: the waveform only marks a loop the document really names.
  // Bounds of its own, not the ones the step above left behind, so an
  // edit there can't quietly become this step's premise.
  await commitField("loop 1 start", "500");
  await commitField("loop 1 end", "1200");
  // The fill the region carries before it is the loop that repeats.
  // Which hue it changes to is the screenshot baseline's to judge; this
  // asks only that recolouring reached the drawn strip, which jsdom
  // cannot show and which a stale region would fail.
  const plain = await regionFill();
  await page.getByRole("combobox", { name: "Sustain loop" }).click();
  await page.getByRole("option", { name: "1", exact: true }).click();
  await page.getByText("repeats while held").waitFor({ timeout: 5000 });

  const marked = await regionFill();
  if (marked === plain) {
    throw new Error(`the sustain loop region is still filled ${plain}, unmarked`);
  }
  if (!marked || marked === "rgba(0, 0, 0, 0)") {
    throw new Error(`the marked region is filled ${marked}, so it is invisible`);
  }

  // What the browser's own audio node received, read as it starts.
  const rate = await voiceRate();
  const { held } = await acrossAKey();
  // Buffer seconds: the frames the fields hold over the voice's rate.
  if (!isWindow(held, [500, 1200], rate)) {
    throw new Error(`a held key repeats ${showWindow(held)} at ${rate} Hz, want loop 1's bounds`);
  }

  // The designation is document state, not this step's to leave behind:
  // a later step reading the select would meet it far from here.
  await page.getByRole("combobox", { name: "Sustain loop" }).click();
  await page.getByRole("option", { name: "none", exact: true }).click();
  await page.getByText("repeats while held").waitFor({ state: "detached", timeout: 5000 });
});

await step("the key coming up moves the window to the release loop (WASM core)", async () => {
  const designate = async (which, option) => {
    await page.getByRole("combobox", { name: which }).click();
    await page.getByRole("option", { name: option, exact: true }).click();
    await page.waitForFunction(
      ([w, o]) => document.querySelector(`[aria-label="${w}"]`)?.textContent?.includes(o) ?? false,
      [which, option],
      { timeout: 5000 },
    );
  };
  // Forward first: loop 2 sits after loop 1, so Chrome traces into it.
  await commitField("loop 1 start", "500");
  await commitField("loop 1 end", "1200");
  await commitField("loop 2 start", "2000");
  await commitField("loop 2 end", "3000");

  // A designation edit puts the drawn loop back to the first.
  const drawLoop = async (n) => {
    await page.getByLabel(`loop ${n} start`).click();
    await page.waitForFunction(
      (want) => document.querySelector(".loopname")?.textContent?.trim() === want,
      `Loop ${n}`,
      { timeout: 5000 },
    );
  };

  // The strip draws one loop at a time, so its three fills are read
  // one at a time: plain, release, sustain. All three differ, or the
  // caption is the only marking there is.
  await drawLoop(2);
  const plainFill = await regionFillWhenDrawn();

  // The sustain designation puts the cap below the release loop
  // (F000:122B). Left at none, note on caps at the release loop
  // itself, and every assertion below passes with the release path
  // deleted, which is what this step used to do.
  await designate("Sustain loop", "1");
  await designate("Release loop", "2");

  await drawLoop(2);
  await page.getByText("repeats after the key").waitFor({ timeout: 5000 });
  const releaseFill = await regionFillWhenDrawn();
  if (!releaseFill || releaseFill === "rgba(0, 0, 0, 0)") {
    throw new Error(`the release region is filled ${releaseFill}, so it is invisible`);
  }
  if (releaseFill === plainFill) {
    throw new Error(`the release region is filled ${releaseFill}, unmarked`);
  }

  await drawLoop(1);
  await page.getByText("repeats while held").waitFor({ timeout: 5000 });
  const sustainFill = await regionFillWhenDrawn();
  if (sustainFill === releaseFill || sustainFill === plainFill) {
    throw new Error(`the sustain region is filled ${sustainFill}, which is not its own state`);
  }

  const rate = await voiceRate();
  const sustainWindow = [await fieldFrames("loop 1 start"), await fieldFrames("loop 1 end")];
  const releaseWindow = [await fieldFrames("loop 2 start"), await fieldFrames("loop 2 end")];

  const forward = await acrossAKey();
  if (!isWindow(forward.held, sustainWindow, rate)) {
    throw new Error(`a held key repeats ${showWindow(forward.held)}, want loop 1's bounds`);
  }
  if (!isWindow(forward.freed, releaseWindow, rate)) {
    throw new Error(`the key coming up leaves ${showWindow(forward.freed)}, want loop 2's bounds`);
  }

  // Backward next: a window a playhead cannot trace into, which Chrome
  // wraps on the next render quantum. Each field moves in the order
  // that keeps its own loop valid.
  await commitField("loop 1 end", "3000");
  await commitField("loop 1 start", "2000");
  await commitField("loop 2 start", "500");
  await commitField("loop 2 end", "1200");
  const backHeld = [await fieldFrames("loop 1 start"), await fieldFrames("loop 1 end")];
  const backFreed = [await fieldFrames("loop 2 start"), await fieldFrames("loop 2 end")];
  if (backFreed[0] >= backHeld[0]) {
    throw new Error(`the release window starts at ${backFreed[0]}, not behind ${backHeld[0]}`);
  }

  const backward = await acrossAKey();
  if (!isWindow(backward.held, backHeld, rate)) {
    throw new Error(`a held key repeats ${showWindow(backward.held)}, want the later window`);
  }
  if (!isWindow(backward.freed, backFreed, rate)) {
    throw new Error(`the key coming up leaves ${showWindow(backward.freed)}, want the earlier one`);
  }

  // The document is shared, so put both designations back.
  await designate("Release loop", "none");
  await designate("Sustain loop", "none");
});

await step("a press schedules the firmware's envelope (WASM core)", async () => {
  // The plan's own verification step. A known envelope goes in through
  // the grid, and what comes back is what the browser's own AudioParam
  // was told. Nothing here recomputes the model: 0.387 s is the
  // disassembly's figure for a full sweep at panel 50, so a wrong
  // stepper fails this even when its unit tests agree with it.
  // Stage 1 sweeps to full at panel 50, stage 2 holds there, and the
  // sustain sits on stage 2, so a held key runs exactly two stages.
  await commitField("DCA envelope stage 1 rate", "50");
  await commitField("DCA envelope stage 1 level", "99");
  await commitField("DCA envelope stage 2 rate", "50");
  await commitField("DCA envelope stage 2 level", "99");
  // The mark is a document edit, so the press has to wait for the core
  // to answer or it plays the envelope as it was.
  await page.getByRole("button", { name: "DCA envelope set sustain stage 2" }).click();
  await page.waitForFunction(
    () =>
      document.querySelector(".marked-sus")?.getAttribute("aria-label") ===
      "DCA envelope set sustain stage 2",
    undefined,
    { timeout: 5000 },
  );

  // Note on holds at zero, then ramps once per stage, so one ordered
  // log carries both the start time and the stages.
  await page.evaluate(() => {
    window.__env = [];
    window.__hold = AudioParam.prototype.setValueAtTime;
    window.__ramp = AudioParam.prototype.linearRampToValueAtTime;
    AudioParam.prototype.setValueAtTime = function (value, time) {
      window.__env.push({ kind: "hold", value, time });
      return window.__hold.call(this, value, time);
    };
    AudioParam.prototype.linearRampToValueAtTime = function (value, time) {
      window.__env.push({ kind: "ramp", value, time });
      return window.__ramp.call(this, value, time);
    };
    window.__exp = AudioParam.prototype.exponentialRampToValueAtTime;
    AudioParam.prototype.exponentialRampToValueAtTime = function (value, time) {
      window.__env.push({ kind: "ramp", value, time });
      return window.__exp.call(this, value, time);
    };
  });

  let log;
  let release;
  try {
    const key = page.locator('[data-testid="key-48"]');
    // The key reads velocity off click height, and the envelope's own
    // velocity scaling bends every stop below full at a light press.
    // The bottom of the key is the full press this figure describes.
    const box = await key.boundingBox();
    if (!box) throw new Error("the keyboard has no key 48");
    await key.dispatchEvent("pointerdown", {
      clientY: box.y + box.height - 2,
      clientX: box.x + box.width / 2,
      pointerId: 1,
    });
    await page.locator(".keyboardbar[data-auditioning]").waitFor({ timeout: 5000 });
    log = await page.evaluate(() => window.__env);
    await page.evaluate(() => {
      window.__stopAt = null;
      window.__stop = AudioBufferSourceNode.prototype.stop;
      AudioBufferSourceNode.prototype.stop = function (t) {
        window.__stopAt = t ?? null;
        return window.__stop.call(this, t);
      };
    });
    await key.dispatchEvent("pointerup", { pointerId: 1 });
    release = await page.evaluate(() => {
      const at = window.__stopAt;
      AudioBufferSourceNode.prototype.stop = window.__stop;
      delete window.__stop;
      delete window.__stopAt;
      return { stopAt: at, events: window.__env.slice() };
    });
  } finally {
    await page.evaluate(() => {
      AudioParam.prototype.setValueAtTime = window.__hold;
      AudioParam.prototype.linearRampToValueAtTime = window.__ramp;
      AudioParam.prototype.exponentialRampToValueAtTime = window.__exp;
      delete window.__hold;
      delete window.__ramp;
      delete window.__exp;
      delete window.__env;
    });
  }

  // The note opens at the code a stop of zero writes rather than at
  // silence, so the note on is the first hold event rather than a
  // zero.
  const start = log.find((e) => e.kind === "hold");
  const ramps = log.filter((e) => e.kind === "ramp");
  if (!start) throw new Error("the press scheduled no note on");
  if (ramps.length !== 2) {
    throw new Error(`the press scheduled ${ramps.length} attack ramps, want 2`);
  }
  const first = ramps[0].time - start.time;
  if (Math.abs(first - 0.387) > 0.03) {
    throw new Error(`stage 1 takes ${first.toFixed(3)} s, want 0.387 s at panel 50`);
  }
  // Stage 2 starts where stage 1 stopped, so it holds rather than moves.
  if (Math.abs(ramps[1].value - ramps[0].value) > 1e-9) {
    throw new Error("stage 2 holds the sustain level, so it should not move");
  }
  // The key coming up runs the release stages, and the source has to
  // outlive them: a stop during the release is the click this work
  // set out to remove.
  const lastRamp = release.events.at(-1);
  if (release.stopAt === null) throw new Error("the release never stopped the source");
  if (release.stopAt < lastRamp.time) {
    throw new Error(
      `the source stops at ${release.stopAt.toFixed(3)} s, during a release that ends at ${lastRamp.time.toFixed(3)} s`,
    );
  }
  // Five edits went in, so five come back out: the steps after this
  // one share the document, and a partial restore leaks four stage
  // edits into all of them.
  for (let i = 0; i < 5; i++) {
    await page.getByRole("button", { name: "Undo" }).click();
  }
  await page.waitForFunction(
    () => document.querySelector('[aria-label="DCA envelope stage 1 rate"]')?.value !== "50",
    undefined,
    { timeout: 5000 },
  );
});

await step("velocity bends the envelope over the real core (WASM core)", async () => {
  // The preview reads four schema ids by name to bend a note by the
  // press and the key. Both the fake and the shell can agree on a
  // stale name, so only a press over the real core catches a rename
  // on the Go side. A full press and a soft one have to differ, and
  // they only differ through this field.
  // The label is shared with the stepper's own controls, so this
  // takes the field itself.
  const field = page.locator('input[aria-label="To amplitude"]');
  await field.fill("100");
  await field.press("Enter");
  await page.waitForFunction(
    () => document.querySelector('input[aria-label="To amplitude"]')?.value === "100",
    undefined,
    { timeout: 5000 },
  );

  const peakAt = async (fraction) => {
    await page.evaluate(() => {
      window.__peak = 0;
      window.__hold = AudioParam.prototype.setValueAtTime;
      window.__exp = AudioParam.prototype.exponentialRampToValueAtTime;
      const see = (v) => {
        window.__peak = Math.max(window.__peak, v);
      };
      AudioParam.prototype.setValueAtTime = function (v, t) {
        see(v);
        return window.__hold.call(this, v, t);
      };
      AudioParam.prototype.exponentialRampToValueAtTime = function (v, t) {
        see(v);
        return window.__exp.call(this, v, t);
      };
    });
    const key = page.locator('[data-testid="key-48"]');
    const box = await key.boundingBox();
    if (!box) throw new Error("the keyboard has no key 48");
    await key.dispatchEvent("pointerdown", {
      clientX: box.x + box.width / 2,
      clientY: box.y + box.height * fraction,
      pointerId: 1,
    });
    await page.locator(".keyboardbar[data-auditioning]").waitFor({ timeout: 5000 });
    await key.dispatchEvent("pointerup", { pointerId: 1 });
    return page.evaluate(() => {
      const peak = window.__peak;
      AudioParam.prototype.setValueAtTime = window.__hold;
      AudioParam.prototype.exponentialRampToValueAtTime = window.__exp;
      delete window.__hold;
      delete window.__exp;
      delete window.__peak;
      return peak;
    });
  };

  const soft = await peakAt(0.08);
  const hard = await peakAt(0.99);
  if (!(soft < hard)) {
    throw new Error(`a soft press peaked at ${soft} against ${hard} for a hard one`);
  }
  await page.getByRole("button", { name: "Undo" }).click();
});

await step("the import estimate names the machine (WASM core)", async () => {
  // The estimate crosses the boundary through a map built field by
  // field, so a figure the fake supplies can be absent in the browser
  // with nothing failing on either side.
  const wavPath = join(tmpdir(), "fizzle-smoke-mem.wav");
  writeFileSync(wavPath, monoWav(600000, 18000));
  await pickFiles([wavPath]);
  await page.getByText("Import 1 WAV").waitFor({ timeout: 5000 });
  const line = await page
    .locator(".dialog .desc")
    .filter({ hasText: "Becomes about" })
    .first()
    .innerText();
  if (!/Needs .* to load; your FZ has /.test(line)) {
    throw new Error(`the estimate says: ${line}`);
  }
  await page.getByRole("button", { name: "Cancel" }).click();
  // The dialog leaves the document untouched, so the steps that follow
  // meet the instrument they expect.
  await page.getByRole("dialog").waitFor({ state: "detached", timeout: 5000 });
});

await step("R14's Sample group reads and edits over the WASM core", async () => {
  // Sample rate is a schema select, so it reaches the screen through
  // the same path every other schema control takes.
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
  // The bar carries the message a user reads, not the machine code.
  if (!text?.includes("an FZ image is")) throw new Error(`alert says: ${text}`);
  if (text.includes("invalid-image")) throw new Error(`alert leaks the code: ${text}`);
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

// R17's central gesture: dragging a loop handle commits a frame the
// numeric field reads back. The pair document's first voice is open.
await step("dragging a loop handle commits frames (R17)", async () => {
  const start = page.getByLabel("loop 1 start");
  await start.fill("100");
  await start.press("Enter");
  // This voice's loop arrives collapsed at its end, so the start
  // commit must land before the end moves below the old start.
  await page.waitForFunction(
    () => Number(document.querySelector('[aria-label="loop 1 start"]')?.value) < 1000,
    undefined,
    { timeout: 5000 },
  );
  const end = page.getByLabel("loop 1 end");
  await end.fill("8000");
  await end.press("Enter");
  // The commit snaps to the nearest zero crossing, so wait for a
  // confirmed value near the fill rather than the literal.
  try {
    await page.waitForFunction(
      () => {
        const v = document.querySelector('[aria-label="loop 1 end"]')?.value;
        return Number(v) > 7000 && Number(v) < 9000;
      },
      undefined,
      { timeout: 5000 },
    );
  } catch {
    const seen = await page.evaluate(
      () => document.querySelector('[aria-label="loop 1 end"]')?.value,
    );
    throw new Error(`loop end did not commit near 8000; field shows ${String(seen)}`);
  }
  const committed = await page.evaluate(
    () => document.querySelector('[aria-label="loop 1 end"]')?.value,
  );

  // The waveform sits below the fold in the headless viewport, and a
  // drag off screen grabs nothing.
  await page.evaluate(() => {
    document.querySelector('[data-testid="waveform"]')?.scrollIntoView({ block: "center" });
  });
  const handle = await page.evaluateHandle(() => {
    const host = document.querySelector('[data-testid="waveform"] div');
    const el = [...(host?.shadowRoot?.querySelectorAll("[part]") ?? [])].find((n) =>
      /^region region-/.test(n.getAttribute("part")),
    );
    return el?.querySelector('[part*="region-handle-right"]') ?? null;
  });
  const box = await handle.asElement()?.boundingBox();
  if (!box) throw new Error("no right loop handle to drag");
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 120, box.y + box.height / 2, { steps: 10 });
  await page.mouse.up();
  await page.waitForFunction(
    (before) => {
      const v = document.querySelector('[aria-label="loop 1 end"]')?.value;
      return v !== before && Number(v) > 0;
    },
    committed,
    { timeout: 10000 },
  );
});

// J1 over the real core: a folder of WAVs becomes a kit laid up the
// keyboard, through the real folder picker input.
await step("a WAV folder converts to a kit (J1, WASM core)", async () => {
  await page.getByRole("button", { name: "Eject", exact: true }).click();
  const discard = page.getByRole("button", { name: "Discard" });
  try {
    await discard.waitFor({ timeout: 1500 });
    await discard.click();
  } catch {
    /* clean */
  }
  await page.getByRole("button", { name: "New disk" }).click();
  await page.getByRole("button", { name: "Create" }).click();
  await page.getByText("[FZ DISK 1]").waitFor({ timeout: 5000 });

  const dir = join(tmpdir(), `fizzle-smoke-kit-${Date.now()}`);
  execSync(`mkdir -p ${JSON.stringify(dir)}`);
  for (let i = 0; i < 3; i++) {
    writeFileSync(join(dir, `hit0${i}.wav`), monoWav(4000, 18000));
  }
  await page.getByLabel("folder").setInputFiles(dir);
  await page.getByText("Import 3 WAVs").waitFor({ timeout: 5000 });
  await page.getByRole("button", { name: "Convert" }).click();
  await page.getByText("3 WAVs mapped up the keyboard").waitFor({ timeout: 15000 });
  await page.waitForFunction(
    () => document.querySelectorAll('table[aria-label="instrument voices"] tbody tr').length === 3,
    undefined,
    { timeout: 10000 },
  );
});

// A band of the editing surface over the real module: bank rename,
// area duplicate, swap, and delete, undo and redo, a voice export,
// the instrument delete, a fresh empty instrument, and the eject.
await step("the editing surface commits over the real core", async () => {
  await page.getByRole("tab", { name: "Banks and Areas" }).click();
  await page.getByRole("table", { name: "areas" }).waitFor({ timeout: 5000 });
  // The rename field appears on a double click of the bank strip.
  await page.getByRole("button", { name: /\(3\)/ }).dblclick();
  const bankName = page.getByLabel("bank name");
  await bankName.fill("SMOKE BANK");
  await bankName.press("Enter");
  await page.getByRole("button", { name: /SMOKE BANK \(3\)/ }).waitFor({ timeout: 5000 });

  await page.getByRole("button", { name: "duplicate area 1" }).click();
  await page.waitForFunction(
    () => document.querySelectorAll('table[aria-label="areas"] tbody tr').length === 4,
    undefined,
    { timeout: 5000 },
  );
  // The swap has to move the area, so read the first row's voice
  // before and after: a no-op reorder would leave it unchanged.
  const firstVoiceBefore = await page.evaluate(
    () => document.querySelector('table[aria-label="areas"] tbody tr')?.textContent?.trim() ?? "",
  );
  await page.getByRole("button", { name: "move area 1 down" }).click();
  await page.waitForFunction(
    (before) =>
      (document.querySelector('table[aria-label="areas"] tbody tr')?.textContent?.trim() ?? "") !==
      before,
    firstVoiceBefore,
    { timeout: 5000 },
  );
  await page.getByRole("button", { name: "delete area 2" }).click();
  await page.waitForFunction(
    () => document.querySelectorAll('table[aria-label="areas"] tbody tr').length === 3,
    undefined,
    { timeout: 5000 },
  );
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelectorAll('table[aria-label="areas"] tbody tr').length === 4,
    undefined,
    { timeout: 5000 },
  );
  await page.getByRole("button", { name: "Redo", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelectorAll('table[aria-label="areas"] tbody tr').length === 3,
    undefined,
    { timeout: 5000 },
  );

  await page.getByRole("tab", { name: "Voices" }).click();
  const exportDl = [];
  const listener = (d) => exportDl.push(d);
  page.on("download", listener);
  await page.getByRole("button", { name: /export HIT00/ }).click();
  await page.getByRole("button", { name: "As .fzv" }).click();
  const fzvDeadline = Date.now() + 10000;
  while (exportDl.length < 1 && Date.now() < fzvDeadline) {
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  page.off("download", listener);
  if (exportDl.length < 1) throw new Error("no .fzv download arrived");

  await page.getByRole("button", { name: /full/ }).click({ button: "right" });
  // Right click opens the delete confirmation for the instrument row.
  await page.getByText("Delete the instrument?").waitFor({ timeout: 5000 });
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  try {
    await page.getByRole("button", { name: "New empty instrument" }).first().waitFor({
      timeout: 5000,
    });
  } catch {
    await page.screenshot({ path: "/tmp/smoke-editing.png" });
    const seen = await page.evaluate(() =>
      [...document.querySelectorAll("button")].map((b) => b.textContent?.trim()).slice(0, 20),
    );
    throw new Error(`no empty-instrument button; buttons: ${seen.join(" | ")}`);
  }
  await page.getByRole("button", { name: "New empty instrument" }).first().click();
  await page.getByText("FULL-DATA-FZ").waitFor({ timeout: 5000 });

  await page.getByRole("button", { name: "Eject", exact: true }).click();
  const discard = page.getByRole("button", { name: "Discard" });
  try {
    await discard.waitFor({ timeout: 1500 });
    await discard.click();
  } catch {
    /* clean */
  }
  await page.getByRole("button", { name: "New disk" }).waitFor({ timeout: 5000 });
});

// The cap rule over a stored file, with no edit anywhere in the step:
// the window the node receives is the core's reading of loop_sus and
// loop_end and nothing else.
await step("a stored loop chain moves the window at the key (WASM core)", async () => {
  const RATE = 18000;
  const LOW = [3600, 21600];
  const HIGH = [39600, 57600];

  await pickFiles([LOOPDEMO]);
  await page.waitForFunction(
    () => document.querySelectorAll('table[aria-label="instrument voices"] tbody tr').length === 5,
    undefined,
    { timeout: 15000 },
  );

  const pressAndRelease = async (row) => {
    await page.locator('table[aria-label="instrument voices"] tbody tr').nth(row).click();
    await page.getByText("61,200 frames").waitFor({ timeout: 5000 });
    return acrossAKey("key-60");
  };

  // Loop 1 for the sustain, loop 3 for the end: only the key moves it.
  const lowHigh = await pressAndRelease(0);
  if (!isWindow(lowHigh.held, LOW, RATE)) {
    throw new Error(`1 LOW HIGH holds ${showWindow(lowHigh.held)}, want the low window`);
  }
  if (!isWindow(lowHigh.freed, HIGH, RATE)) {
    throw new Error(`1 LOW HIGH frees to ${showWindow(lowHigh.freed)}, want the high window`);
  }

  // No sustain loop, so the cap is the end loop from note on
  // (F000:122B) and the high window repeats from the press.
  const highOnly = await pressAndRelease(2);
  if (!isWindow(highOnly.held, HIGH, RATE)) {
    throw new Error(
      `3 HIGH ONLY holds ${showWindow(highOnly.held)}, want the high window from note on`,
    );
  }

  // Row 4 names neither, so it plays through.
  const noLoop = await pressAndRelease(4);
  if (noLoop.held.on) {
    throw new Error(`5 NO LOOP repeats ${showWindow(noLoop.held)}, want no loop at all`);
  }
});

await browser.close();
await server.close();

if (errors.length > 0 || failed) {
  if (errors.length > 0) console.error(`console errors:\n${errors.join("\n")}`);
  process.exit(1);
}
console.log("smoke passed: the mockup UI drives the WASM core, byte identical, console clean");
