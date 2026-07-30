// Shared drivers for the shell interaction tests: a fresh disk via
// the new disk dialog, and an opened image carrying the fake's
// instrument.
import { fireEvent, render, screen } from "@testing-library/react";
import type { Core } from "../src/boundary/contract";
import { IMAGE_SIZE } from "../src/boundary/contract";
import { createFakeCore } from "../src/core/fake";
import { App } from "../src/shell/App";

// Both openers take a core, so a test can stage a refusal the fake
// has no way to produce and still drive the real shell.
export async function openDisk(core: Core = createFakeCore()) {
  render(<App core={core} />);
  fireEvent.click(await screen.findByRole("button", { name: "New disk" }));
  fireEvent.change(await screen.findByLabelText("disk label"), { target: { value: "MY DISK" } });
  fireEvent.click(screen.getByRole("button", { name: "Create" }));
  await screen.findByText("[MY DISK]");
}

export async function openInstrumentDisk(core: Core = createFakeCore()) {
  render(<App core={core} />);
  const image = new File([new Uint8Array(IMAGE_SIZE)], "TECHNO.img");
  fireEvent.click(await screen.findByRole("button", { name: "Browse…" }));
  fireEvent.change(screen.getByLabelText("fz files"), { target: { files: [image] } });
  await screen.findByText("[OPENED]");
}

export function pickFiles(files: File[]) {
  fireEvent.change(screen.getByLabelText("fz files"), { target: { files } });
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
