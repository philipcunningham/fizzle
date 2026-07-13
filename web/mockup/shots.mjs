// Capture reference screenshots of the mockup for the operator gate.
// Usage: node shots.mjs <output-dir>

import { chromium } from "playwright";
import { preview } from "vite";

const outDir = process.argv[2] ?? ".";
const server = await preview({ preview: { port: 4519 } });
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });

await page.goto("http://localhost:4519/");
await page.getByRole("heading", { name: "fizzle" }).waitFor();
await page.screenshot({ path: `${outDir}/01-start.png` });

await page.getByRole("button", { name: "Browse", exact: true }).click();
await page.locator(".filerow", { hasText: "FULL-DATA-FZ" }).click();
await page.locator(".waveform-wrap canvas").first().waitFor();
await page.screenshot({ path: `${outDir}/02-voices.png` });

await page.getByRole("tab", { name: "Banks and Areas" }).click();
await page.locator("table.term tbody tr").first().click();
await page.screenshot({ path: `${outDir}/03-banks-areas.png` });

await page.getByRole("tab", { name: "Effects" }).click();
await page.screenshot({ path: `${outDir}/04-effects.png` });

await page.getByRole("button", { name: "journeys ▾" }).click();
await page.getByText("J1. WAV folder to playable disk").click();
await page.screenshot({ path: `${outDir}/05-journey-guide.png` });

await browser.close();
await server.close();
console.log(`screenshots written to ${outDir}`);
