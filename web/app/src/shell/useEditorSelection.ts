import { useState } from "react";
import type { InstrumentSnapshot } from "../boundary/contract";
import { deriveEditorFocus } from "./editorFocus";
import type { EditorTab } from "./editorFocus";

/** Owns view-only editor selection. No selection is written into the document. */
export function useEditorSelection(instrument: InstrumentSnapshot | null) {
  const [tab, setTab] = useState<EditorTab>("voices");
  const [selectedSlot, setSelectedSlot] = useState<number | null>(null);
  const [selectedBank, setSelectedBank] = useState(0);
  const [selectedArea, setSelectedArea] = useState<number | null>(null);
  const [selectedLoop, setSelectedLoop] = useState(0);
  const focus = deriveEditorFocus(instrument, tab, selectedSlot, selectedBank, selectedArea);

  const selectBank = (bank: number) => {
    setSelectedBank(bank);
    setSelectedArea(null);
  };
  const clearArea = () => {
    setSelectedArea(null);
  };

  return {
    tab,
    setTab,
    selectedSlot,
    setSelectedSlot,
    selectedBank,
    selectedArea,
    setSelectedArea,
    selectedLoop,
    setSelectedLoop,
    selectBank,
    clearArea,
    ...focus,
  };
}
