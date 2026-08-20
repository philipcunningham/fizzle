// The sustain loop rule, on its own: which of a voice's eight loops
// repeats while a key is held, and when none of them does.
import { describe, expect, it } from "vitest";
import type { VoiceDetail } from "../src/boundary/contract";
import { sustainLoop } from "../src/viewstate/loops";

function detail(loopSustain: number, first: { start: number; end: number }): VoiceDetail {
  const envelope = {
    sustain: 7,
    end: 7,
    rates: new Array<number>(8).fill(50),
    stops: new Array<number>(8).fill(99),
  };
  return {
    frames: 4096,
    sampleRate: 18000,
    genStart: 0,
    genEnd: 4096,
    loopSustain,
    loopRelease: 8,
    loops: Array.from({ length: 8 }, (_, i) =>
      i === 0 ? { ...first, xf: 0, tm: 0 } : { start: 0, end: 4096, xf: 0, tm: 0 },
    ),
    dca: envelope,
    dcf: envelope,
  };
}

describe("the sustain loop", () => {
  it("is the loop the voice names", () => {
    expect(sustainLoop(detail(0, { start: 100, end: 900 }))).toMatchObject({
      start: 100,
      end: 900,
    });
  });

  it("is absent when the voice names none", () => {
    // 8 is the format's "no loop" index, past the last of the eight.
    expect(sustainLoop(detail(8, { start: 100, end: 900 }))).toBeUndefined();
  });

  it("is absent when the named loop has no range", () => {
    expect(sustainLoop(detail(0, { start: 900, end: 900 }))).toBeUndefined();
    expect(sustainLoop(detail(0, { start: 900, end: 100 }))).toBeUndefined();
  });

  it("is absent for a file with no voice detail", () => {
    expect(sustainLoop(undefined)).toBeUndefined();
  });
});
