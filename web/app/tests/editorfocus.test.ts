import { describe, expect, it } from "vitest";
import type { InstrumentSnapshot } from "../src/boundary/contract";
import { deriveEditorFocus } from "../src/shell/editorFocus";

const instrument: InstrumentSnapshot = {
  fileName: "FULL-DATA-FZ",
  banks: [
    {
      name: "BANK A",
      areas: [
        {
          voiceSlot: 2,
          voiceName: "AREA VOICE",
          keyLow: 24,
          keyHigh: 48,
          root: 36,
          velLow: 1,
          velHigh: 127,
          midiChannel: 1,
          output: 255,
          outputLabel: "all",
          volume: 0,
        },
      ],
    },
  ],
  voices: [
    {
      slot: 1,
      name: "SELECTED",
      referenced: false,
      params: { rootKey: 60, keyLow: 12, keyHigh: 96 },
    },
    { slot: 2, name: "AREA VOICE", referenced: true, params: { rootKey: 72 } },
  ],
};

describe("editor focus", () => {
  it("uses the selected voice mapping on the voices tab", () => {
    const focus = deriveEditorFocus(instrument, "voices", 1, 0, 0);
    expect(focus.focusVoice?.slot).toBe(1);
    expect(focus.focusRoot).toBe(60);
    expect(focus.highlight).toEqual([{ lo: 12, hi: 96 }]);
  });

  it("uses the area's voice, range, and root on the banks tab", () => {
    const focus = deriveEditorFocus(instrument, "banks", 1, 0, 0);
    expect(focus.focusVoice?.slot).toBe(2);
    expect(focus.focusRoot).toBe(36);
    expect(focus.highlight).toEqual([{ lo: 24, hi: 48 }]);
  });
});
