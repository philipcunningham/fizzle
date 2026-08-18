// Area resolution for the banks tab keyboard: a pressed key sounds
// through the bank's areas the way the hardware plays them, so the
// resolver has to honour key range, velocity range, and layering.
import { describe, expect, it } from "vitest";
import type { AreaSnapshot } from "../src/boundary/contract";
import { matchAreas } from "../src/viewstate/mapping";

function area(overrides: Partial<AreaSnapshot>): AreaSnapshot {
  return {
    voiceSlot: 0,
    voiceName: "V",
    keyLow: 0,
    keyHigh: 127,
    root: 60,
    velLow: 1,
    velHigh: 127,
    midiChannel: 1,
    output: 255,
    outputLabel: "all",
    volume: 0,
    ...overrides,
  };
}

describe("matchAreas", () => {
  const bank = [
    area({ voiceSlot: 0, keyHigh: 59 }),
    area({ voiceSlot: 1, keyLow: 60, velLow: 64 }),
    area({ voiceSlot: 2, keyLow: 60, velHigh: 63 }),
  ];

  it("resolves by key range", () => {
    expect(matchAreas(bank, 40, 100).map((a) => a.voiceSlot)).toEqual([0]);
  });

  it("resolves by velocity range inside a key split", () => {
    expect(matchAreas(bank, 70, 100).map((a) => a.voiceSlot)).toEqual([1]);
    expect(matchAreas(bank, 70, 30).map((a) => a.voiceSlot)).toEqual([2]);
  });

  it("range edges belong to the area", () => {
    expect(matchAreas(bank, 59, 100).map((a) => a.voiceSlot)).toEqual([0]);
    expect(matchAreas(bank, 70, 64).map((a) => a.voiceSlot)).toEqual([1]);
    expect(matchAreas(bank, 70, 63).map((a) => a.voiceSlot)).toEqual([2]);
  });

  it("layers every matching area, the way the hardware sounds them", () => {
    const layered = [area({ voiceSlot: 3 }), area({ voiceSlot: 4 })];
    expect(matchAreas(layered, 60, 90).map((a) => a.voiceSlot)).toEqual([3, 4]);
  });

  it("finds nothing for an uncovered key", () => {
    expect(matchAreas([area({ voiceSlot: 0, keyLow: 36, keyHigh: 48 })], 60, 100)).toEqual([]);
  });
});
