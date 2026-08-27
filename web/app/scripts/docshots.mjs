// The manual's screenshots. Drives the built app to a fixed state over
// the real WASM core and writes PNGs into docs/images/. These are
// documentation, not baselines: nothing here compares against a
// committed image, and there is no per-platform directory. Regenerate
// deliberately with `make docshots` when the surface they show moves.
import { copyFileSync, mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "playwright";
import { preview } from "vite";
import { makeRegionFill } from "./pagehelpers.mjs";

// Its own port: 4519 belongs to the smoke, 4523 to the baselines.
const PORT = 4527;
const OUT = new URL("../../../docs/images", import.meta.url).pathname;
const LOOPDEMO = new URL("../../../testdata/synthetic/LOOPDEMO.img", import.meta.url).pathname;
const TECHNO = new URL("../../../testdata/synthetic/TECHNO.img", import.meta.url).pathname;

// The run writes here and moves the set into docs/images only once
// every shot has landed. A failure part way through then leaves the
// committed images as they were, rather than half of one generation
// beside half of another.
const staging = mkdtempSync(join(tmpdir(), "fizzle-docshots-"));

let server;
let browser;
try {
  server = await preview({ preview: { port: PORT } });
  browser = await chromium.launch({
    channel: "chrome",
    // The GPU raster path decides text antialiasing and the blending of
    // the rounded borders per run, which moved a few dozen pixels
    // between two runs of the same script. On the software path a
    // rerun rewrites the same bytes.
    args: [
      "--disable-gpu",
      "--disable-lcd-text",
      "--font-render-hinting=none",
      "--force-color-profile=srgb",
    ],
  });
  const page = await browser.newPage({
    viewport: { width: 1280, height: 900 },
    // Twice the pixels, so a reader on a dense display can read the
    // labels the prose names.
    deviceScaleFactor: 2,
    // A shot taken mid transition lands wherever the clock left it.
    reducedMotion: "reduce",
  });

  const shot = async (name, selector) => {
    const buf = await (selector ? page.locator(selector).first().screenshot() : page.screenshot());
    writeFileSync(join(staging, `${name}.png`), buf);
    console.log(`shot ${name}.png (${String(buf.length)} bytes)`);
  };

  // The smoke's picker, including its guard for the confirmation that
  // appears when a disk is already open.
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

  await page.goto(`http://localhost:${String(PORT)}/`);
  await page.getByRole("button", { name: "New disk" }).waitFor({ timeout: 15000 });

  // LOOPDEMO carries five voices over one sample, each with its own loop
  // chain, so the strip has something to say.
  await pickFiles([LOOPDEMO]);
  await page.waitForFunction(
    () => document.querySelectorAll('table[aria-label="instrument voices"] tbody tr').length === 5,
    undefined,
    { timeout: 20000 },
  );
  // The strip draws in two passes, the peaks and then the loop region
  // over them, and the frame count lands before either. A shot taken
  // between the passes shows a strip with no loop marked on it, which is
  // the one thing these images exist to show.
  await page.getByText("61,200 frames").waitFor({ timeout: 15000 });
  const regionFill = makeRegionFill(page);
  const drawn = Date.now() + 15000;
  while ((await regionFill()) === null && Date.now() < drawn) {
    await page.waitForTimeout(50);
  }
  if ((await regionFill()) === null) throw new Error("the strip drew no loop region");
  // Text measured against a fallback face lands on different pixels.
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(600);

  await shot("workspace");
  await shot("capacity", ".capacitystack");
  await shot("waveform", '.panel:has([data-testid="waveform"])');

  // TECHNO carries eight banks over 32 voices, which one bank of one
  // voice can't show.
  await pickFiles([TECHNO]);
  await page.getByRole("tab", { name: "Banks and Areas" }).click();
  await page.getByRole("table", { name: "areas" }).waitFor({ timeout: 20000 });
  // Selecting an area opens the range editor under the table, which is
  // where an area's key and velocity spans are set.
  await page.getByRole("table", { name: "areas" }).locator("tbody tr").first().click();
  await page.getByText("Key range (drag or type)").waitFor({ timeout: 5000 });
  await page.waitForTimeout(400);

  await shot("banks-and-areas", ".tabbody .centered");

  // Copied rather than renamed: the staging directory can sit on
  // another filesystem, and a rename across one fails.
  mkdirSync(OUT, { recursive: true });
  for (const name of readdirSync(staging)) {
    copyFileSync(join(staging, name), join(OUT, name));
    console.log(`published ${name}`);
  }
} finally {
  // Cleanup can't be allowed to replace the failure that brought us
  // here, so each step reports and carries on. A browser or a port left
  // alive would fail the next run for an unrelated reason.
  for (const close of [() => browser?.close(), () => server?.close()]) {
    try {
      await close();
    } catch (err) {
      console.error(`cleanup failed: ${String(err)}`);
    }
  }
  try {
    rmSync(staging, { recursive: true, force: true });
  } catch (err) {
    console.error(`cleanup failed: ${String(err)}`);
  }
}
