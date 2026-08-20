// The DCA envelope as the firmware runs it. Every figure asserted here
// comes from reverse engineering the FZ-1, recorded in
// llm-wiki/topics/envelope-timing.md, rather than from the code under
// test: a test that reads its expectations out of the implementation
// pins whatever bug the implementation has.
import { describe, expect, it } from "vitest";
import type { EnvelopeSnapshot } from "../src/boundary/contract";
import { attack, release, stageSeconds } from "../src/ui/dca";

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

/** A voice's envelope, in the display units the snapshot carries. */
function envelope(over: Partial<EnvelopeSnapshot> = {}): EnvelopeSnapshot {
  return {
    sustain: 1,
    end: 2,
    rates: [99, 60, 60, 50, 50, 50, 50, 50],
    stops: [99, 70, 0, 0, 0, 0, 0, 0],
    ...over,
  };
}

describe("the stages a held key runs", () => {
  it("runs stage zero through the sustain stage, and stops there", () => {
    const segments = attack(envelope({ sustain: 2 }));
    expect(segments).toHaveLength(3);
  });

  // Stops are display units; the level a segment reaches is that stop
  // as a fraction of full scale.
  it("reaches each stage's own stop level", () => {
    const [first, second] = attack(envelope());
    expect(first?.level).toBeCloseTo(1, 2);
    expect(second?.level).toBeCloseTo(70 / 99, 1);
  });

  it("holds nothing back for a voice whose sustain is stage zero", () => {
    expect(attack(envelope({ sustain: 0 }))).toHaveLength(1);
  });
});

describe("the stages a released key runs", () => {
  // The ordinary case: sustain plus one through the end stage.
  it("runs from after the sustain stage to the end stage", () => {
    const segments = release(envelope({ sustain: 1, end: 3 }), 0.7, true);
    expect(segments).toHaveLength(2);
  });

  // Note on forces the end stage falling to silence at F000:1351,
  // whatever the file stores for it.
  it("always ends at silence, whatever the stored stop says", () => {
    const env = envelope({ sustain: 1, end: 2, stops: [99, 70, 88, 0, 0, 0, 0, 0] });
    const segments = release(env, 0.7, true);
    expect(segments.at(-1)?.level).toBe(0);
  });

  // A sustain pointer past the end stage leaves nothing to run, which
  // the factory piano does with Sus 7 and End 4.
  it("has no stages when the sustain sits past the end", () => {
    expect(release(envelope({ sustain: 7, end: 4 }), 0.5, true)).toHaveLength(0);
  });

  // Released before the sustain stage was reached, the firmware jumps
  // straight to the end stage at F000:1512 and runs it alone.
  it("runs the end stage alone when the key came up early", () => {
    const segments = release(envelope({ sustain: 1, end: 3 }), 0.4, false);
    expect(segments).toHaveLength(1);
    expect(segments[0]?.level).toBe(0);
  });

  it("times the first release stage from where the note actually was", () => {
    const env = envelope({ sustain: 0, end: 1, rates: [99, 30, 60, 50, 50, 50, 50, 50] });
    const fromHigh = release(env, 1, true)[0]?.seconds ?? 0;
    const fromLow = release(env, 0.25, true)[0]?.seconds ?? 0;
    expect(fromLow).toBeLessThan(fromHigh);
  });
});

/** No scaling at all: the envelope as stored. */
const NO_SCALING = {
  velocity: 127,
  note: 60,
  centre: 60,
  levelKF: 0,
  rateKF: 0,
  velLevel: 0,
  velRate: 0,
};

// Velocity and key scaling are applied once, at note on, into per-voice
// copies of the rates and stops (F000:12B4 to F000:135E). Velocity
// enters as min(velocity + 0x10, 0x7F), so a sensitivity of zero is a
// no op whatever the velocity.
describe("scaling by velocity and key", () => {
  it("changes nothing when both sensitivities are zero", () => {
    const env = envelope();
    const soft = attack(env, { ...NO_SCALING, velocity: 1 });
    const hard = attack(env, { ...NO_SCALING, velocity: 127 });
    expect(soft).toEqual(hard);
    expect(soft).toEqual(attack(env));
  });

  it("plays quieter for a softer press when level sensitivity is on", () => {
    const env = envelope();
    const soft = attack(env, { ...NO_SCALING, velocity: 1, velLevel: 60 });
    const hard = attack(env, { ...NO_SCALING, velocity: 127, velLevel: 60 });
    expect(soft.at(-1)?.level ?? 0).toBeLessThan(hard.at(-1)?.level ?? 0);
    // Velocity moves the level, not how many stages there are.
    expect(soft).toHaveLength(hard.length);
  });

  it("moves the stage times when rate sensitivity is on", () => {
    const env = envelope({ sustain: 1, rates: [40, 40, 40, 40, 40, 40, 40, 40] });
    const soft = attack(env, { ...NO_SCALING, velocity: 1, velRate: 80 });
    const hard = attack(env, { ...NO_SCALING, velocity: 127, velRate: 80 });
    expect(soft[0]?.seconds).not.toBeCloseTo(hard[0]?.seconds ?? 0, 3);
  });

  // Key follow is (key - centre) scaled: >>4 for level, >>7 for rate.
  it("follows the keyboard away from the voice's centre", () => {
    const env = envelope();
    const atCentre = attack(env, { ...NO_SCALING, levelKF: 15 });
    const wayUp = attack(env, { ...NO_SCALING, note: 96, levelKF: 15 });
    expect(wayUp.at(-1)?.level).not.toBeCloseTo(atCentre.at(-1)?.level ?? 0, 3);
  });

  it("never lets a scaled stage stall or overflow", () => {
    const env = envelope({
      rates: [1, 1, 1, 1, 1, 1, 1, 1],
      stops: [99, 99, 99, 99, 99, 99, 99, 99],
    });
    for (const velocity of [1, 64, 127]) {
      for (const velRate of [-127, 0, 127]) {
        for (const velLevel of [-127, 0, 127]) {
          const segments = attack(env, { ...NO_SCALING, velocity, velRate, velLevel });
          for (const segment of segments) {
            expect(Number.isFinite(segment.seconds)).toBe(true);
            expect(segment.level).toBeGreaterThanOrEqual(0);
            expect(segment.level).toBeLessThanOrEqual(1);
          }
        }
      }
    }
  });
});
