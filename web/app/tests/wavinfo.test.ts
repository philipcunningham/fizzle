// The channel count scan drives one UI affordance: hiding the stereo
// choice for mono files. It never decides anything the core enforces.
import { describe, expect, it } from "vitest";
import { wavChannels } from "../src/shell/wavinfo";
import { wavHeader } from "./helpers";

describe("wavChannels", () => {
  it("reads 1 from a mono file", () => {
    expect(wavChannels(wavHeader(1))).toBe(1);
  });

  it("reads 2 from a stereo file", () => {
    expect(wavChannels(wavHeader(2))).toBe(2);
  });

  it("returns null for anything unparseable", () => {
    expect(wavChannels(new Uint8Array(10))).toBeNull();
    expect(wavChannels(new Uint8Array(100).fill(65))).toBeNull();
  });

  it("finds fmt when another chunk precedes it", () => {
    const base = wavHeader(2);
    const shifted = new Uint8Array(56);
    const dv = new DataView(shifted.buffer);
    shifted.set(base.subarray(0, 12), 0); // RIFF/WAVE
    // A JUNK chunk before fmt.
    shifted.set([0x4a, 0x55, 0x4e, 0x4b], 12);
    dv.setUint32(16, 4, true);
    shifted.set(base.subarray(12, 36), 24); // fmt chunk
    expect(wavChannels(shifted)).toBe(2);
  });
});
