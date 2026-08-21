// The sustain loop rule, on its own: which of a voice's eight loops
// repeats while a key is held, and when none of them does.
import { describe, expect, it } from "vitest";
import type { VoiceDetail } from "../src/boundary/contract";
import { isReleaseLoop, isSustainLoop, releaseLoop, sustainLoop } from "../src/viewstate/loops";

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
    // Loops 1 to 7 each get bounds that are a function of their own
    // index, distinct from every other loop, so a test that reads the
    // wrong index fails instead of quietly matching a sibling.
    loops: Array.from({ length: 8 }, (_, i) =>
      i === 0 ? { ...first, xf: 0, tm: 0 } : { start: i * 100, end: i * 100 + 500, xf: 0, tm: 0 },
    ),
    dca: envelope,
    dcf: envelope,
  };
}

/**
 * The same voice, with a release loop named. Loops 1 to 7 each carry
 * distinct, usable bounds, so naming one gives a usable loop without
 * more setup.
 */
function withRelease(loopSustain: number, loopRelease: number): VoiceDetail {
  return { ...detail(loopSustain, { start: 100, end: 900 }), loopRelease };
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

describe("the loop a released key moves to", () => {
  it("is the loop the voice names", () => {
    expect(releaseLoop(withRelease(8, 2))).toMatchObject({ start: 200, end: 700 });
  });

  it("is absent when the voice names none", () => {
    expect(releaseLoop(withRelease(8, 8))).toBeUndefined();
  });

  it("is absent when the named loop has no range", () => {
    const d = withRelease(8, 0);
    d.loops[0] = { start: 500, end: 500, xf: 0, tm: 0 };
    expect(releaseLoop(d)).toBeUndefined();
  });

  // 146 corpus voices name one loop for both roles.
  it("is the sustain loop too when one loop serves both", () => {
    const d = withRelease(3, 3);
    expect(releaseLoop(d)).toEqual(sustainLoop(d));
    expect(isReleaseLoop(d, 3)).toBe(true);
    expect(isSustainLoop(d, 3)).toBe(true);
  });

  it("marks only the loop the voice names", () => {
    const d = withRelease(8, 2);
    expect(isReleaseLoop(d, 2)).toBe(true);
    expect(isReleaseLoop(d, 1)).toBe(false);
  });
});
