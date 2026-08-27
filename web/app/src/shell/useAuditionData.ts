import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import type { BankSnapshot, Core, InstrumentSnapshot, InstrumentVoice } from "../boundary/contract";
import { queryKeys } from "../queries/client";
import type { EditorTab } from "./editorFocus";

/** Owns decoded-audio and waveform queries, including bank prefetch. */
export function useAuditionData(
  core: Core,
  tab: EditorTab,
  instrument: InstrumentSnapshot | null,
  bank: BankSnapshot | undefined,
  voice: InstrumentVoice | null,
  focusVoice: InstrumentVoice | null,
) {
  const queryClient = useQueryClient();
  const frames = voice?.voice?.frames ?? 0;
  const peaksQuery = useQuery({
    queryKey: queryKeys.peaks(0, `slot-${String(voice?.slot ?? -1)}:${voice?.audioKey ?? ""}`),
    queryFn: () => core.slotPeaks(voice?.slot ?? 0, 0, frames, 2048),
    enabled: voice !== null && frames > 0,
    placeholderData: keepPreviousData,
  });
  const auditionQuery = useQuery({
    queryKey: ["audition", focusVoice?.slot ?? -1, focusVoice?.audioKey ?? ""],
    queryFn: () => core.auditionSlot(focusVoice?.slot ?? 0),
    enabled: focusVoice !== null,
    placeholderData: keepPreviousData,
  });

  useEffect(() => {
    if (tab !== "banks" || !bank) return;
    const slots = new Set(bank.areas.map((area) => area.voiceSlot));
    for (const slot of slots) {
      const slotVoice = instrument?.voices.find((candidate) => candidate.slot === slot);
      void queryClient
        .query({
          queryKey: ["audition", slot, slotVoice?.audioKey ?? ""],
          queryFn: () => core.auditionSlot(slot),
        })
        .catch(() => undefined);
    }
  }, [bank, core, instrument, queryClient, tab]);

  const slotPCM = (slot: number, audioKey: string) =>
    queryClient.query({
      queryKey: ["audition", slot, audioKey],
      queryFn: () => core.auditionSlot(slot),
    });

  return {
    peaks: peaksQuery.data?.ok ? peaksQuery.data.value : null,
    auditionData:
      auditionQuery.data?.ok && !auditionQuery.isPlaceholderData ? auditionQuery.data.value : null,
    slotPCM,
  };
}
