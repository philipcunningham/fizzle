// The DCA envelope as the firmware runs it. Every figure asserted here
// comes from reverse engineering the FZ-1, recorded in
// llm-wiki/topics/envelope-timing.md, rather than from the code under
// test: a test that reads its expectations out of the implementation
// pins whatever bug the implementation has.
import { describe, expect, it } from "vitest";
import type { EnvelopeSnapshot } from "../src/boundary/contract";
import { amplitude, amplitudeAt, attack, levelAt, release, stageSeconds } from "../src/ui/dca";

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

  // Note on caps the run at the lower of the sustain and end stages
  // (F000:123B), so a sustain stage at or past the end stage never
  // holds anything. The voice runs to its end stage, which note on
  // forces to silence, and frees its own slot with the key still
  // down. Sustain past end is how the format spells a one shot, and
  // most factory voices are written that way.
  it("stops at the end stage when the sustain sits past it", () => {
    const env = envelope({ sustain: 7, end: 2, stops: [99, 70, 88, 0, 0, 0, 0, 0] });
    const segments = attack(env);
    expect(segments).toHaveLength(3);
    expect(segments.at(-1)?.level).toBe(0);
  });

  it("falls silent while the key is still down for a one shot", () => {
    const env = envelope({ sustain: 3, end: 3, stops: [99, 70, 60, 55, 0, 0, 0, 0] });
    expect(attack(env).at(-1)?.level).toBe(0);
  });

  // A voice that does hold keeps its sustain level, because its end
  // stage is one of the stages release runs rather than attack.
  it("holds the sustain level when the sustain comes first", () => {
    const env = envelope({ sustain: 1, end: 3, stops: [99, 70, 40, 20, 0, 0, 0, 0] });
    const segments = attack(env);
    expect(segments).toHaveLength(2);
    expect(segments.at(-1)?.level).toBeCloseTo(stopByteOf(70) / 255, 6);
  });
});

/** The stop byte the core computes for a panel value. */
function stopByteOf(display: number): number {
  return Math.floor((255 * (display - 1)) / 99) + 1;
}

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
  it("takes the keyboard follow as the panel shows it, eight to a byte", () => {
    const env = envelope({ sustain: 0, stops: [50, 0, 0, 0, 0, 0, 0, 0] });
    // A panel 1 is byte 8: (36 * 8) >> 4 is 18, not (36 * 1) >> 4.
    const one = attack(env, { ...NO_SCALING, note: 96, levelKF: 1 });
    expect(one[0]?.level).toBeCloseTo((127 + 18) / 255, 6);
  });

  it("follows the keyboard away from the voice's centre", () => {
    const env = envelope();
    const atCentre = attack(env, { ...NO_SCALING, levelKF: 15 });
    const wayUp = attack(env, { ...NO_SCALING, note: 96, levelKF: 15 });
    expect(wayUp.at(-1)?.level).not.toBeCloseTo(atCentre.at(-1)?.level ?? 0, 3);
  });

  // The coefficients themselves, each figure worked from the note on
  // arithmetic rather than from this module. Directional assertions
  // pass whichever way a shift or a sign goes, so these state the
  // number the firmware arrives at.
  describe("the arithmetic note on performs", () => {
    // Stops are scaled by ((key - centre) * dca_kf) >> 4, an arithmetic
    // shift, added to the stop byte and clamped to 0 to 255. The
    // firmware multiplies by the stored byte, which is eight times the
    // -15 to 15 the panel and the schema show.
    it("shifts the key follow on a stop by four places, and floors it", () => {
      const env = envelope({ sustain: 0, stops: [50, 0, 0, 0, 0, 0, 0, 0] });
      // stopByte(50) is 127. A panel 3 is byte 24, and 35 keys up:
      // (35 * 24) >> 4 is 52, where 52.5 is the exact quotient.
      const up = attack(env, { ...NO_SCALING, note: 95, levelKF: 3 });
      expect(up[0]?.level).toBeCloseTo((127 + 52) / 255, 6);
      // The same distance down is -53, not -52. An arithmetic shift
      // floors where a division truncates.
      const down = attack(env, { ...NO_SCALING, note: 25, levelKF: 3 });
      expect(down[0]?.level).toBeCloseTo((127 - 53) / 255, 6);
    });

    // Velocity enters as min(velocity + 0x10, 0x7F), and the stop term
    // is 2 * (((vel * vel_dca_kf * 2) >> 8) - vel_dca_kf).
    it("doubles the velocity term on a stop, and normalises it away at zero", () => {
      const env = envelope({ sustain: 0, stops: [99, 0, 0, 0, 0, 0, 0, 0] });
      // A press of 1 enters as 17: ((17 * 80 * 2) >> 8) is 10, less 80
      // is -70, doubled is -140, from a full 255.
      const soft = attack(env, { ...NO_SCALING, velocity: 1, velLevel: 80 });
      expect(soft[0]?.level).toBeCloseTo((255 - 140) / 255, 6);
      // A full press lands one doubled step short of the stored stop,
      // because this path carries no rounding correction: 79 less 80,
      // doubled, is -2.
      const hard = attack(env, { ...NO_SCALING, velocity: 127, velLevel: 80 });
      expect(hard[0]?.level).toBeCloseTo((255 - 2) / 255, 6);
    });

    it("saturates the velocity offset rather than wrapping it", () => {
      const env = envelope({ sustain: 0, stops: [99, 0, 0, 0, 0, 0, 0, 0] });
      // Without the 0x10 offset a press of 1 would land on 255 - 254.
      const soft = attack(env, { ...NO_SCALING, velocity: 1, velLevel: 127 });
      expect(soft[0]?.level).toBeCloseTo((255 - 222) / 255, 6);
    });

    // A negative sensitivity skips the normalising subtraction, which
    // inverts the curve: the softer press is the louder one. The shift
    // stays arithmetic over a negative product, so it floors.
    it("inverts the press for a negative sensitivity", () => {
      const env = envelope({ sustain: 0, stops: [99, 0, 0, 0, 0, 0, 0, 0] });
      // ((17 * -80 * 2) >> 8) is -11, doubled is -22.
      const soft = attack(env, { ...NO_SCALING, velocity: 1, velLevel: -80 });
      expect(soft[0]?.level).toBeCloseTo((255 - 22) / 255, 6);
      // ((127 * -80 * 2) >> 8) is -80, doubled is -160.
      const hard = attack(env, { ...NO_SCALING, velocity: 127, velLevel: -80 });
      expect(hard[0]?.level).toBeCloseTo((255 - 160) / 255, 6);
      expect(soft[0]?.level ?? 0).toBeGreaterThan(hard[0]?.level ?? 0);
    });

    it("inverts the rate for a negative sensitivity too", () => {
      const env = envelope({ sustain: 0, rates: [50, 0, 0, 0, 0, 0, 0, 0] });
      // (-11 + 1) >> 1 is -5, so byte 64 becomes 59, panel 46.
      const soft = attack(env, { ...NO_SCALING, velocity: 1, velRate: -80 });
      expect(soft[0]?.seconds).toBeCloseTo(stageSeconds(0, 255, 46), 9);
      // (-80 + 1) >> 1 is -40, so byte 64 becomes 24, panel 18.
      const hard = attack(env, { ...NO_SCALING, velocity: 127, velRate: -80 });
      expect(hard[0]?.seconds).toBeCloseTo(stageSeconds(0, 255, 18), 9);
    });

    // Rates are scaled by ((key - centre) * dca_rs) >> 7, seven places
    // rather than four, so the same key follow moves a rate far less.
    it("shifts the key follow on a rate by seven places", () => {
      const env = envelope({ sustain: 0, rates: [50, 0, 0, 0, 0, 0, 0, 0] });
      // Rate byte 64, plus (36 * 16) >> 7 which is 4, is byte 68. The
      // panel value that maps to byte 68 is 53.
      const up = attack(env, { ...NO_SCALING, note: 96, rateKF: 2 });
      expect(up[0]?.seconds).toBeCloseTo(stageSeconds(0, 255, 53), 9);
    });

    // The rate term is (((vel * vel_dca_rs * 2) >> 8) + 1 - vel_dca_rs)
    // >> 1. The + 1 makes a full press exact, so the stored rate is
    // what a full press runs.
    it("leaves a full press on the stored rate, and slows a soft one", () => {
      const env = envelope({ sustain: 0, rates: [50, 0, 0, 0, 0, 0, 0, 0] });
      const hard = attack(env, { ...NO_SCALING, velocity: 127, velRate: 80 });
      expect(hard[0]?.seconds).toBeCloseTo(stageSeconds(0, 255, 50), 9);
      // A press of 1: ((17 * 80 * 2) >> 8) is 10, plus 1 less 80 is
      // -69, halved with an arithmetic shift is -35. Byte 64 - 35 is
      // 29, which the panel shows as 22.
      const soft = attack(env, { ...NO_SCALING, velocity: 1, velRate: 80 });
      expect(soft[0]?.seconds).toBeCloseTo(stageSeconds(0, 255, 22), 9);
    });
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

// Where a run of stages sits partway through. A key released mid
// attack starts its release from the level the envelope had reached,
// so the engine has to be able to ask for it rather than read a value
// back out of the scheduler.
describe("the level partway through a run of stages", () => {
  const stages = [
    { seconds: 2, level: 1 },
    { seconds: 4, level: 0.5 },
  ];

  it("starts at silence", () => {
    expect(levelAt(stages, 0)).toBe(0);
  });

  it("interpolates along the stage it is in", () => {
    expect(levelAt(stages, 1)).toBeCloseTo(0.5, 6);
    expect(levelAt(stages, 2)).toBeCloseTo(1, 6);
    expect(levelAt(stages, 4)).toBeCloseTo(0.75, 6);
  });

  it("holds the last stop once the run is over", () => {
    expect(levelAt(stages, 6)).toBeCloseTo(0.5, 6);
    expect(levelAt(stages, 600)).toBeCloseTo(0.5, 6);
  });

  it("passes an instant stage instantly", () => {
    expect(levelAt([{ seconds: 0, level: 1 }, ...stages], 0)).toBe(1);
  });

  it("is silent for a run with no stages", () => {
    expect(levelAt([], 3)).toBe(0);
  });
});

// The loudness law. The DCA is an analog amplifier inside the filter
// chip (MB87186, which the FZ-1 parts list calls FM-1 and fits four
// of, two channels each), driven by a 10 bit control word the
// firmware steps as one monotone number.
//
// Which levels are loud relative to which is the firmware's own map
// and is exact. How many dB the whole range covers is not: the chip's
// documented 0 to -87.75 dB over the full word would put 57.6 dB
// across the codes the firmware uses, which spreads a velocity
// sensitive voice over 46 dB and leaves ordinary playing inaudible.
// The preview covers the range in 36 dB instead, tuned by ear until a
// hardware measurement can settle it.
describe("what a level sounds like", () => {
  const dbOf = (level: number) => 20 * Math.log10(amplitude(level));

  it("puts the loudest code the firmware writes at full scale", () => {
    expect(amplitude(1)).toBeCloseTo(1, 9);
  });

  // A stop of zero is code 224, which is 671 steps below the loudest
  // code. The FZ's envelope floor is not digital silence: the chip's
  // own mute is a control word of zero, which the firmware writes
  // only when it frees the voice.
  it("floors at the code a stop of zero writes, not at silence", () => {
    expect(dbOf(0)).toBeCloseTo(-36, 1);
    expect(amplitude(0)).toBeGreaterThan(0);
  });

  // The level to code map spends 159 of its 671 steps on the bottom
  // 62.5% of the level range and the rest on the top, so the scale is
  // steeply top weighted. Half level is nowhere near half loudness.
  it("is top weighted, as the expansion table makes it", () => {
    // The map spends 159 of its 671 code steps on the bottom 62.5% of
    // the level range, so half level sits four fifths of the way down.
    expect(dbOf(0.5)).toBeCloseTo(-29.1, 1);
    expect(dbOf(159 / 255)).toBeCloseTo(-27.5, 1);
  });

  it("rises with the level, without a step backwards", () => {
    let last = -Infinity;
    for (let byte = 0; byte <= 255; byte++) {
      const db = dbOf(byte / 255);
      expect(db).toBeGreaterThan(last);
      last = db;
    }
  });

  it("holds a level outside the range to the ends of it", () => {
    expect(amplitude(-1)).toBe(amplitude(0));
    expect(amplitude(2)).toBe(amplitude(1));
  });
});

// The engine schedules a stage as a ramp that is straight in dB, so
// where a release starts has to be read off that same line. Reading
// the level's own straight line instead lands somewhere else, and the
// note steps as the key comes up.
describe("the loudness partway through a run of stages", () => {
  const stages = [{ seconds: 2, level: 1 }];

  it("is halfway in dB at the halfway point", () => {
    const floor = 20 * Math.log10(amplitude(0));
    const top = 20 * Math.log10(amplitude(1));
    const half = 20 * Math.log10(amplitudeAt(stages, 1));
    expect(half).toBeCloseTo((floor + top) / 2, 6);
  });

  it("meets the level's own reading at the ends of a stage", () => {
    expect(amplitudeAt(stages, 0)).toBeCloseTo(amplitude(levelAt(stages, 0)), 9);
    expect(amplitudeAt(stages, 2)).toBeCloseTo(amplitude(levelAt(stages, 2)), 9);
  });

  it("holds the last stop once the run is over", () => {
    expect(amplitudeAt(stages, 99)).toBeCloseTo(amplitude(1), 9);
  });
});
