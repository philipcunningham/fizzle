// The DCA envelope as the firmware runs it. Every figure asserted here
// comes from reverse engineering the FZ-1, recorded in
// llm-wiki/topics/envelope-timing.md, rather than from the code under
// test: a test that reads its expectations out of the implementation
// pins whatever bug the implementation has.
import { describe, expect, it } from "vitest";
import { stageSeconds } from "../src/ui/dca";

/** The full 0 to 255 span, which is what the worked figures describe. */
const FULL = 255;

describe("how long a stage takes", () => {
  // Seconds are |level delta| * 2.048 / table[rate], the 125 Hz
  // stepper over the rate table at F000:0490.
  it("matches the firmware's own worked figures for a full sweep", () => {
    for (const [panel, want] of [
      [50, 0.387],
      [25, 2.301],
      [12, 7.461],
      [6, 18.651],
    ] as const) {
      expect(stageSeconds(0, FULL, panel)).toBeCloseTo(want, 2);
    }
  });

  // The output stage moves one code per 4 kHz tick (F000:0A5D), so the
  // full 224 to 895 code range takes 168 ms however fast the envelope
  // asks to be.
  it("floors a fast stage at what the output can slew", () => {
    expect(stageSeconds(0, FULL, 98)).toBeCloseTo(0.168, 3);
    expect(stageSeconds(0, FULL, 90)).toBeCloseTo(0.168, 3);
    expect(stageSeconds(0, FULL, 75)).toBeCloseTo(0.168, 3);
  });

  // A rate byte of 0x7F, which the panel shows as 99, writes the ports
  // directly at F000:2094 and skips the slew.
  it("makes panel 99 instant, where 98 is not", () => {
    expect(stageSeconds(0, FULL, 99)).toBe(0);
    expect(stageSeconds(0, FULL, 98)).toBeGreaterThan(0.1);
  });

  // Note on clamps an effective rate into 1 to 0x7F at F000:12E9. A
  // stored zero therefore crawls at table[1], which is 3 units an
  // update: 255 * 256 / 3 / 125 is 174.08 seconds. Without the clamp
  // the increment would be zero and the stage would never end.
  it("treats a rate of zero as the slowest the firmware allows", () => {
    expect(stageSeconds(0, FULL, 0)).toBeCloseTo(174.08, 2);
    expect(Number.isFinite(stageSeconds(0, FULL, 0))).toBe(true);
  });

  it("scales with the distance travelled", () => {
    const full = stageSeconds(0, FULL, 25);
    const half = stageSeconds(0, Math.round(FULL / 2), 25);
    expect(half).toBeCloseTo(full / 2, 1);
  });

  it("takes the same time in either direction", () => {
    expect(stageSeconds(FULL, 0, 30)).toBeCloseTo(stageSeconds(0, FULL, 30), 6);
  });

  it("costs nothing when the level does not move", () => {
    expect(stageSeconds(120, 120, 40)).toBe(0);
  });
});
