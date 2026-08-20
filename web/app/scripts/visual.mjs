// Visual baselines for the canvas components (R16, R17): drives the
// built app to a seeded state and screenshots the waveform and both
// envelope editors. With --update the shots become the baselines;
// otherwise they must match the committed baselines byte for byte.
// Font and canvas rendering differ across platforms, so baselines are
// per-platform and this stays a local gate, not a CI one.
import { mkdirSync, readFileSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "playwright";
import { preview } from "vite";

const update = process.argv.includes("--update");
const dir = new URL(`../visual/${process.platform}`, import.meta.url).pathname;
mkdirSync(dir, { recursive: true });

const server = await preview({ preview: { port: 4523 } });
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({
  viewport: { width: 1100, height: 900 },
  // A shot taken while a transition is in flight lands wherever the
  // clock left it, which is what made one node on the DCF graph differ
  // by a couple of levels between runs.
  reducedMotion: "reduce",
});
await page
  .addStyleTag({
    content: "*, *::before, *::after { transition: none !important; animation: none !important; }",
  })
  .catch(() => undefined);

// A deterministic seeded state: one imported WAV, one loop set.
const monoWav = (samples, rate) => {
  const data = Buffer.alloc(samples * 2);
  for (let i = 0; i < samples; i++) {
    data.writeInt16LE(Math.round(20000 * Math.sin(i / 25) * Math.exp(-i / 2500)), i * 2);
  }
  const header = Buffer.alloc(44);
  header.write("RIFF", 0);
  header.writeUInt32LE(36 + data.length, 4);
  header.write("WAVEfmt ", 8);
  header.writeUInt32LE(16, 16);
  header.writeUInt16LE(1, 20);
  header.writeUInt16LE(1, 22);
  header.writeUInt32LE(rate, 24);
  header.writeUInt32LE(rate * 2, 28);
  header.writeUInt16LE(2, 32);
  header.writeUInt16LE(16, 34);
  header.write("data", 36);
  header.writeUInt32LE(data.length, 40);
  return Buffer.concat([header, data]);
};

await page.goto("http://localhost:4523/");
await page.getByRole("button", { name: "New disk" }).click();
await page.getByLabel("disk label").fill("VISUAL");
await page.getByRole("button", { name: "Create" }).click();
await page.getByText("[VISUAL]").waitFor({ timeout: 10000 });
const wavPath = join(tmpdir(), "visual.wav");
writeFileSync(wavPath, monoWav(4000, 18000));
await page.getByLabel("fz files").setInputFiles(wavPath);
await page.getByRole("button", { name: "Convert" }).click();
await page.getByText("Voices (1/64)").waitFor({ timeout: 10000 });
const start = page.getByLabel("loop 1 start");
await start.waitFor({ timeout: 10000 });
await start.fill("500");
await start.blur();
await page.waitForFunction(
  () => document.querySelector('[aria-label="loop 1 start"]')?.value === "500",
  undefined,
  { timeout: 5000 },
);
// Let the waveform settle before shooting.
await page.waitForTimeout(400);

const shots = [
  ["waveform", '[data-testid="waveform"]'],
  ["dca-envelope", 'svg[aria-label="DCA envelope envelope graph"]'],
  ["dcf-envelope", 'svg[aria-label="DCF envelope envelope graph"]'],
];

let failed = false;
const compare = async (name, selector) => {
  const shot = await page.locator(selector).first().screenshot();
  const path = join(dir, `${name}.png`);
  if (update || !existsSync(path)) {
    writeFileSync(path, shot);
    console.log(`baseline ${update ? "updated" : "created"}: ${name}`);
  } else if (!readFileSync(path).equals(shot)) {
    writeFileSync(join(dir, `${name}.actual.png`), shot);
    console.log(`FAIL ${name}: differs from baseline (see ${name}.actual.png)`);
    failed = true;
  } else {
    console.log(`ok   ${name}`);
  }
};

for (const [name, selector] of shots) {
  await compare(name, selector);
}

// The same strip once loop 1 is the loop the voice repeats: the region
// changes hue, which only a screenshot can hold. Shot last, so the
// three above keep the state their baselines were taken in.
const regionFill = () =>
  page.evaluate(() => {
    const host = document.querySelector('[data-testid="waveform"] div');
    const el = [...(host?.shadowRoot?.querySelectorAll("[part]") ?? [])].find((n) =>
      /^region region-/.test(n.getAttribute("part")),
    );
    return el ? getComputedStyle(el).backgroundColor : null;
  });

const plain = await regionFill();
await page.getByRole("combobox", { name: "Sustain loop" }).click();
await page.getByRole("option", { name: "1", exact: true }).click();
await page.getByText("repeats while held").waitFor({ timeout: 5000 });

// Waits on the drawn fill rather than a stopwatch, since recolouring
// rebuilds the region and a slow machine would shoot the old one. The
// hue itself is what the baseline judges, so this only asks that the
// fill moved, and says what it stalled on if it never does.
const deadline = Date.now() + 5000;
let fill = plain;
while (fill === plain && Date.now() < deadline) {
  await new Promise((resolve) => setTimeout(resolve, 50));
  fill = await regionFill();
}
if (fill === plain) {
  console.log(`FAIL waveform-sustain: the region is still filled ${plain}, unmarked`);
  failed = true;
} else {
  await compare("waveform-sustain", '[data-testid="waveform"]');
}

await browser.close();
await server.close();
process.exit(failed ? 1 : 0);
