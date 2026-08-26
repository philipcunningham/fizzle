import type { InstrumentSnapshot } from "../boundary/contract";

export type EditorTab = "voices" | "banks" | "effects";

/** Derives every editor and keyboard selection from the shell's small view state. */
export function deriveEditorFocus(
  instrument: InstrumentSnapshot | null,
  tab: EditorTab,
  selectedSlot: number | null,
  selectedBank: number,
  selectedArea: number | null,
) {
  const voice =
    instrument?.voices.find((candidate) => candidate.slot === selectedSlot) ??
    instrument?.voices[0] ??
    null;
  const bank = instrument?.banks[Math.min(selectedBank, (instrument.banks.length || 1) - 1)];
  const area = selectedArea === null ? null : (bank?.areas[selectedArea] ?? null);
  const areaVoice = area
    ? (instrument?.voices.find((candidate) => candidate.slot === area.voiceSlot) ?? null)
    : null;
  const focusVoice = tab === "banks" && areaVoice ? areaVoice : voice;
  const highlight =
    tab === "banks"
      ? area
        ? [{ lo: area.keyLow, hi: area.keyHigh }]
        : (bank?.areas.map((candidate) => ({ lo: candidate.keyLow, hi: candidate.keyHigh })) ??
          null)
      : voice &&
          typeof voice.params?.["keyLow"] === "number" &&
          typeof voice.params["keyHigh"] === "number"
        ? [{ lo: voice.params["keyLow"], hi: voice.params["keyHigh"] }]
        : null;
  const focusRoot =
    tab === "banks"
      ? (area?.root ?? null)
      : typeof focusVoice?.params?.["rootKey"] === "number"
        ? focusVoice.params["rootKey"]
        : null;

  return { voice, bank, area, areaVoice, focusVoice, highlight, focusRoot };
}
