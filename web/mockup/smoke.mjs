// Headless smoke test: drives the built mockup through the main click
// paths and fails on any console error. Uses the locally installed Chrome
// channel so no browser download is needed.

import { chromium } from "playwright";
import { preview } from "vite";

const server = await preview({ preview: { port: 4517 } });
const errors = [];

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
    errors.push(`${name}: ${err}`);
  }
};

await page.goto("http://localhost:4517/");

await step("start screen renders", async () => {
  await page.getByRole("heading", { name: "fizzle" }).waitFor({ timeout: 5000 });
});

await step("new disk dialog opens and creates", async () => {
  await page.getByRole("button", { name: "new disk" }).click();
  await page.getByLabel("disk label").fill("SMOKE");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await page.getByText("[SMOKE]").waitFor({ timeout: 3000 });
});

await step("simulated WAV import maps voices and reports in the bar", async () => {
  await page.getByRole("button", { name: "import ▾" }).click();
  await page.getByText("WAV folder").click();
  await page.getByRole("button", { name: "Convert", exact: true }).click();
  await page.getByText("TOM", { exact: false }).first().waitFor({ timeout: 3000 });
  await page.locator(".barmsg.ok", { hasText: "imported 3 WAVs" }).waitFor({ timeout: 3000 });
});

await step("waveform mounts for the selected voice", async () => {
  await page.locator(".waveform-wrap canvas").first().waitFor({ timeout: 5000 });
});

await step("banks tab shows areas and range pickers", async () => {
  await page.getByRole("tab", { name: "Banks and Areas" }).click();
  await page.getByText("Areas in BANK 1", { exact: false }).waitFor({ timeout: 3000 });
});

await step("effects tab renders the 21 cell matrix", async () => {
  await page.getByRole("tab", { name: "Effects" }).click();
  await page.getByText("Controller modulation matrix").waitFor({ timeout: 3000 });
  const cells = await page.getByRole("spinbutton").count();
  if (cells !== 21) throw new Error(`expected 21 matrix cells, got ${cells}`);
});

let matrixBefore;
await step("matrix cell arrow-key edit increments the value", async () => {
  const cell = page.getByRole("spinbutton").first();
  matrixBefore = Number(await cell.getAttribute("aria-valuenow"));
  await cell.focus();
  await page.keyboard.press("ArrowUp");
  await page.waitForTimeout(200);
  const after = Number(await cell.getAttribute("aria-valuenow"));
  if (after !== matrixBefore + 1) throw new Error(`expected ${matrixBefore + 1}, got ${after}`);
});

await step("undo reverts the matrix edit", async () => {
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await page.waitForTimeout(200);
  const cell = page.getByRole("spinbutton").first();
  const reverted = Number(await cell.getAttribute("aria-valuenow"));
  if (reverted !== matrixBefore) throw new Error(`expected ${matrixBefore}, got ${reverted}`);
});

await step("export clears the dirty dot and reports in the bar", async () => {
  await page.getByRole("button", { name: "Export", exact: true }).click();
  await page.locator(".barmsg.ok", { hasText: "exported SMOKE.img" }).waitFor({ timeout: 3000 });
  const dot = await page.locator('header span[title="Unexported changes"]').count();
  if (dot !== 0) throw new Error("dirty dot still visible after export");
});

await step("journey guide opens and tracks", async () => {
  await page.getByRole("button", { name: "journeys ▾" }).click();
  await page.getByText("J3. Rework an old disk").click();
  await page.getByRole("complementary", { name: "journey walkthrough" }).waitFor({ timeout: 3000 });
});

await step("switch disk prompt guards unexported changes", async () => {
  // Dirty the document again first; export above cleared the flag.
  const cell = page.getByRole("spinbutton").first();
  await cell.focus();
  await page.keyboard.press("ArrowUp");
  await page.getByRole("button", { name: "close disk" }).click();
  await page.getByRole("heading", { name: "Unexported changes" }).waitFor({ timeout: 3000 });
  // Closing now lands on the start screen; the seed disk opens from there.
  await page.getByRole("button", { name: "Discard", exact: true }).click();
  await page.getByRole("button", { name: "Browse", exact: true }).click();
  await page.getByText("[FZ SESSION 1]").waitFor({ timeout: 3000 });
});

await step("seed instrument opens with 9 voices", async () => {
  await page.locator(".filerow", { hasText: "FULL-DATA-FZ" }).click();
  await page.getByText("Voices (9/64)").waitFor({ timeout: 3000 });
});

await step("mapping the spare voice consumes the marker and reports", async () => {
  await page.getByRole("button", { name: "map RIM SPARE" }).click();
  await page.getByRole("button", { name: "map RIM SPARE" }).waitFor({ state: "hidden", timeout: 3000 });
  await page.locator(".barmsg.status", { hasText: "RIM SPARE mapped to" }).waitFor({ timeout: 3000 });
});

await step("duplicate produces no bar message", async () => {
  // allTextContents never waits, so an expired status line reads as
  // an empty list instead of stalling the step.
  const before = (await page.locator(".barmsg").allTextContents()).join("|");
  await page.getByRole("tab", { name: "Banks and Areas" }).click();
  await page.getByRole("button", { name: "duplicate area" }).first().click();
  await page.waitForTimeout(200);
  const after = (await page.locator(".barmsg").allTextContents()).join("|");
  if (after !== before) throw new Error(`bar changed on duplicate: ${after}`);
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await page.getByRole("tab", { name: "Voices" }).click();
});

await step("over-capacity import raises a persistent error", async () => {
  // Pile WAV folders onto the seed disk until the two disk ceiling
  // rejects one; the mock charges 68 KB a file.
  let raised = false;
  for (let i = 0; i < 20; i++) {
    await page.getByRole("button", { name: "import ▾" }).click();
    await page.getByText("WAV folder").click();
    await page.getByRole("button", { name: "Convert", exact: true }).click();
    await page.waitForTimeout(100);
    if (await page.locator(".barmsg.error").count()) {
      raised = true;
      break;
    }
  }
  if (!raised) throw new Error("no over-capacity error after 20 folder imports");
  await page.locator(".barmsg.error", { hasText: "import rejected" }).waitFor({ timeout: 1000 });
  // The successful import just before the rejection saturated the
  // keyboard, so its status line reports the pile-up (J5).
  await page.locator(".barmsg", { hasText: "keyboard full" }).waitFor({ timeout: 1000 });
  // The capacity bar echoes the alarm at the control itself.
  await page.locator(".capacity.over").waitFor({ timeout: 1000 });
});

await step("a later status line never buries the error", async () => {
  await page.getByRole("button", { name: "import ▾" }).click();
  await page.getByText(".fzv voice dump").click();
  await page.locator(".barmsg.status", { hasText: "joined the voice list" }).waitFor({ timeout: 3000 });
  await page.locator(".barmsg.error", { hasText: "import rejected" }).waitFor({ timeout: 1000 });
  await page.getByRole("button", { name: "dismiss error" }).click();
  await page.locator(".barmsg.error").waitFor({ state: "hidden", timeout: 1000 });
});

await step("status lines expire after 5 seconds", async () => {
  await page.locator(".barmsg.status", { hasText: "joined the voice list" }).waitFor({ state: "hidden", timeout: 7000 });
});

await step("bank file on disk offers actions", async () => {
  await page.getByText("DRUMS2.fzb").click();
  await page.getByRole("button", { name: /Add as bank/ }).click();
  await page.getByRole("tab", { name: "Banks and Areas" }).click();
  await page.getByRole("button", { name: "DRUMS2 (0)" }).waitFor({ timeout: 3000 });
  await page.getByRole("tab", { name: "Voices" }).click();
});

await step("loop region renders and drags", async () => {
  const region = page.locator('[part^="region "]').first();
  await region.waitFor({ timeout: 5000 });
  await region.scrollIntoViewIfNeeded();
  const before = await page.getByLabel("loop start frame").inputValue();
  const box = await region.boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 60, box.y + box.height / 2, { steps: 8 });
  await page.mouse.up();
  await page.waitForTimeout(300);
  const after = await page.getByLabel("loop start frame").inputValue();
  if (before === after) throw new Error(`loop start unchanged (${before})`);
});

await step("loop table edits and selects the active loop", async () => {
  await page.getByLabel("loop 2 crossfade").fill("64");
  await page.getByLabel("loop 2 start").click();
  await page.getByText("Loop 2").first().waitFor({ timeout: 3000 });
});

await step("region drag edits the selected loop, not loop 1", async () => {
  const l1 = await page.getByLabel("loop 1 start").inputValue();
  const l2 = await page.getByLabel("loop 2 start").inputValue();
  const region = page.locator('[part^="region "]').first();
  await region.scrollIntoViewIfNeeded();
  const box = await region.boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 - 50, box.y + box.height / 2, { steps: 8 });
  await page.mouse.up();
  await page.waitForTimeout(300);
  const l1After = await page.getByLabel("loop 1 start").inputValue();
  const l2After = await page.getByLabel("loop 2 start").inputValue();
  if (l1After !== l1) throw new Error(`loop 1 changed (${l1} to ${l1After})`);
  if (l2After === l2) throw new Error("loop 2 unchanged by drag");
});

await step("replacing the instrument guards unexported changes", async () => {
  await page.getByRole("button", { name: "import ▾" }).click();
  await page.getByText(".fzf full dump").click();
  await page.getByRole("heading", { name: "Replace the instrument?" }).waitFor({ timeout: 3000 });
  await page.getByText("Unexported changes will be lost", { exact: false }).waitFor({ timeout: 1000 });
  await page.getByRole("button", { name: "Export first", exact: true }).click();
  await page.getByText("Unexported changes will be lost", { exact: false }).waitFor({ state: "hidden", timeout: 1000 });
  await page.getByRole("button", { name: "Replace the instrument", exact: true }).click();
  await page.getByText("Instrument: STRINGS").waitFor({ timeout: 3000 });
  // The full dump row leads with the instrument's name; the fixed
  // firmware name sits under it.
  await page.locator(".filerow", { hasText: "STRINGS" }).waitFor({ timeout: 3000 });
  await page.locator(".filerow", { hasText: "FULL-DATA-FZ" }).waitFor({ timeout: 3000 });
});

await step("deleting the full dump warns, guards, and reports", async () => {
  await page.locator(".filerow", { hasText: "FULL-DATA-FZ" }).click({ button: "right" });
  await page.getByRole("heading", { name: "Delete STRINGS?" }).waitFor({ timeout: 3000 });
  await page.getByText("removes the instrument and all", { exact: false }).waitFor({ timeout: 1000 });
  // The replace above left the document dirty, so the guard shows.
  await page.getByText("Unexported changes will be lost", { exact: false }).waitFor({ timeout: 1000 });
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await page.locator(".barmsg.status", { hasText: "deleted STRINGS" }).waitFor({ timeout: 3000 });
  await page.getByRole("button", { name: "New empty instrument" }).first().waitFor({ timeout: 3000 });
});

await step("component inventory renders", async () => {
  await page.getByRole("button", { name: "components" }).click();
  await page.getByRole("heading", { name: "Envelope editor" }).waitFor({ timeout: 3000 });
});

await browser.close();
await server.close();

if (errors.length > 0) {
  console.log("\nsmoke test FAILED:");
  errors.forEach((e) => console.log(` - ${e}`));
  process.exit(1);
}
console.log("\nsmoke test passed with no console errors");
process.exit(0);
