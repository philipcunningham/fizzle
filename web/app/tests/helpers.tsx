// Shared drivers for the shell interaction tests: a fresh disk via
// the new disk dialog, and an opened image carrying the fake's
// instrument.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect } from "vitest";
import type { Core } from "../src/boundary/contract";
import { IMAGE_SIZE } from "../src/boundary/contract";
import { createScenarioCore } from "./support/scenarioCore";
import { App } from "../src/shell/App";

// Both openers take a core, so a test can stage a refusal the fake
// has no way to produce and still drive the real shell.
export async function openDisk(core: Core = createScenarioCore()) {
  render(<App core={core} />);
  fireEvent.click(await screen.findByRole("button", { name: "New disk" }));
  fireEvent.change(await screen.findByLabelText("disk label"), { target: { value: "MY DISK" } });
  fireEvent.click(screen.getByRole("button", { name: "Create" }));
  await screen.findByText("[MY DISK]");
}

export async function openInstrumentDisk(core: Core = createScenarioCore()) {
  render(<App core={core} />);
  const image = new File([new Uint8Array(IMAGE_SIZE)], "TECHNO.img");
  fireEvent.click(await screen.findByRole("button", { name: "Browse" }));
  fireEvent.change(screen.getByLabelText("fz files"), { target: { files: [image] } });
  await screen.findByText("[OPENED]");
}

export function pickFiles(files: File[]) {
  fireEvent.change(screen.getByLabelText("fz files"), { target: { files } });
}

/**
 * A 44-byte WAV header whose fmt and data fields declare the given
 * shape. Header-only scans (channels, rate, frame count) read the
 * declared sizes, so no sample data needs allocating.
 */
export function wavFixture(
  channels: number,
  rate: number,
  frames: number,
): Uint8Array<ArrayBuffer> {
  const b = wavHeader(channels);
  const dv = new DataView(b.buffer);
  dv.setUint32(24, rate, true);
  dv.setUint32(40, frames * channels * 2, true);
  return b;
}

/** A 44-byte WAV header, enough for the shell's channel count scan. */
export function wavHeader(channels: number): Uint8Array<ArrayBuffer> {
  const b = new Uint8Array(44);
  const dv = new DataView(b.buffer);
  const ascii = (s: string, at: number) => {
    for (let i = 0; i < s.length; i++) b[at + i] = s.charCodeAt(i);
  };
  ascii("RIFF", 0);
  dv.setUint32(4, 36, true);
  ascii("WAVE", 8);
  ascii("fmt ", 12);
  dv.setUint32(16, 16, true);
  dv.setUint16(20, 1, true);
  dv.setUint16(22, channels, true);
  ascii("data", 36);
  return b;
}

/**
 * Commits a numeric field and waits for the core's answer to come
 * back. The wait is the point: a field shows a draft the moment it is
 * typed, so an assertion or a second edit that runs before the commit
 * lands reads the old document, and the browser tests have been caught
 * by that more than once.
 */
export async function commitField(label: string, value: string) {
  const field = screen.getByLabelText(label);
  fireEvent.change(field, { target: { value } });
  fireEvent.blur(field);
  await waitFor(() => {
    expect(screen.getByLabelText<HTMLInputElement>(label).value).toBe(value);
  });
}
