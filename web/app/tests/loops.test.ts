// The sustain loop rule, on its own: which of a voice's eight loops
// repeats while a key is held, and when none of them does.
import { describe, expect, it } from "vitest";
import type { VoiceDetail } from "../src/boundary/contract";
import { isSustainLoop, sustainLoop } from "../src/viewstate/loops";

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

// The waveform marks the loop it draws when that loop is the one the
// voice repeats, so the question is asked of an index.
describe("whether a loop is the sustain loop", () => {
  it("holds for the named loop alone", () => {
    expect(isSustainLoop(detail(2, { start: 100, end: 900 }), 2)).toBe(true);
    expect(isSustainLoop(detail(2, { start: 100, end: 900 }), 0)).toBe(false);
  });

  it("fails when the voice names none", () => {
    expect(isSustainLoop(detail(8, { start: 100, end: 900 }), 8)).toBe(false);
  });

  // The preview won't repeat a loop with no range, so the waveform
  // can't promise it does.
  it("fails when the named loop has no range", () => {
    expect(isSustainLoop(detail(0, { start: 900, end: 900 }), 0)).toBe(false);
  });

  it("fails for a file with no voice detail", () => {
    expect(isSustainLoop(undefined, 0)).toBe(false);
  });
});
