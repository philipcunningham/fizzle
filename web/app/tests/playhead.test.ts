// Where the playhead is, from the numbers note on already knows. Web
// Audio reports no position, so this models it, and the fold's answers
// were measured against Chrome before it was written.
import { describe, expect, it } from "vitest";
import { frameAt, type PlayheadPlan } from "../src/ui/playhead";

function plan(over: Partial<PlayheadPlan> = {}): PlayheadPlan {
  return {
    startedAt: 10,
    rate: 1,
    sampleRate: 18000,
    frames: 61200,
    window: { start: 3600, end: 21600 },
    releasedAt: null,
    releaseWindow: null,
    ...over,
  };
}

describe("the playhead's position", () => {
  it("runs from the sample's start at the note's own rate", () => {
    expect(frameAt(plan(), 10)).toBe(0);
    expect(frameAt(plan(), 10.1)).toBeCloseTo(1800, 6);
    expect(frameAt(plan({ rate: 2 }), 10.1)).toBeCloseTo(3600, 6);
  });

  it("reads as the start before the note begins", () => {
    expect(frameAt(plan(), 9.5)).toBe(0);
  });

  it("folds back into the window once it passes the end", () => {
    // 1.3 s in is frame 23400, which is 1800 past the window's end.
    expect(frameAt(plan(), 11.3)).toBeCloseTo(3600 + 1800, 6);
  });

  it("stays inside the window however long the key is held", () => {
    for (const seconds of [1.2, 2.5, 7, 30]) {
      const frame = frameAt(plan(), 10 + seconds);
      expect(frame).toBeGreaterThanOrEqual(3600);
      expect(frame).toBeLessThan(21600);
    }
  });

  it("clamps at the last frame with no window at all", () => {
    const one = plan({ window: null });
    expect(frameAt(one, 10.1)).toBeCloseTo(1800, 6);
    expect(frameAt(one, 100)).toBe(61199);
  });

  it("traces forward into a release window that sits ahead", () => {
    // Released at 1 s, so frame 18000, with the end loop at 39600.
    // Half a second later the playhead is still travelling to it.
    const moved = plan({
      releasedAt: 11,
      releaseWindow: { start: 39600, end: 57600 },
    });
    // Measured the same way: released at 18000 into a window ahead,
    // Chrome carries straight on (18256 at 256 frames past the move,
    // 19024 at 1024, 22096 at 4096) rather than jumping to it.
    expect(frameAt(moved, 11.5)).toBeCloseTo(27000, 6);
    const wrapped = frameAt(moved, 13.3);
    expect(wrapped).toBeGreaterThanOrEqual(39600);
    expect(wrapped).toBeLessThan(57600);
  });

  it("restarts a release window that sits behind, at its start", () => {
    // Measured in Chrome, rendering a ramp offline and reading the
    // source frame back out of the output. Held in the high window to
    // frame 54000 and released into the low one, Chrome reads 3600
    // plus the time since the move: it resets to the window's start
    // rather than folding the overshoot, so the phase is lost.
    // (3840 at 256 frames past the move, 4608 at 1024, 7680 at 4096,
    // each a render quantum behind the model's own straight line.)
    const high = { start: 39600, end: 57600 };
    expect(frameAt(plan({ window: high }), 13)).toBeCloseTo(54000, 6);

    const back = plan({
      window: high,
      releasedAt: 13,
      releaseWindow: { start: 3600, end: 21600 },
    });
    expect(frameAt(back, 13.05)).toBeCloseTo(3600 + 900, 6);
    expect(frameAt(back, 13.5)).toBeCloseTo(3600 + 9000, 6);
  });

  it("reads a time before the key came up as a held note", () => {
    // The shell only ever reads forwards, but a reader that answered
    // the release path for an earlier time would be lying about it.
    const back = plan({ releasedAt: 13, releaseWindow: { start: 39600, end: 57600 } });
    expect(frameAt(back, 12.5)).toBeCloseTo(frameAt(plan(), 12.5), 6);
  });

  it("keeps repeating the sustain window when no release window is named", () => {
    const held = plan({ releasedAt: 11, releaseWindow: null });
    const frame = frameAt(held, 12.4);
    expect(frame).toBeGreaterThanOrEqual(3600);
    expect(frame).toBeLessThan(21600);
  });
});
