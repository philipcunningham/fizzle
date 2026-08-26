import { keepPreviousData, useQuery } from "@tanstack/react-query";
import type { Channel, Core, SampleRate } from "../boundary/contract";
import type { PendingDialog } from "../dialogs/Dialogs";
import { toFileMap } from "../viewstate/place";

/** Owns the import dialog's document-aware preflight query. */
export function useImportEstimate(
  core: Core,
  dialog: PendingDialog | null,
  revision: number,
  rate: string,
  stereo: string,
) {
  const wavDialog = dialog?.kind === "wavImport" ? dialog : null;
  const query = useQuery({
    queryKey: [
      "estimate",
      revision,
      wavDialog?.files.map((file) => `${file.name}:${String(file.bytes.length)}`).join("|") ?? "",
      rate,
      stereo,
    ],
    queryFn: () =>
      core.estimateImport(
        toFileMap(wavDialog?.files ?? []),
        (Number(rate) * 1000) as SampleRate,
        stereo.toLowerCase() as Channel,
      ),
    enabled: wavDialog !== null,
    placeholderData: keepPreviousData,
  });
  const result = wavDialog === null ? null : (query.data ?? null);
  return {
    estimate: result?.ok ? result.value : null,
    estimateError: result !== null && !result.ok ? result.error.message : null,
  };
}
