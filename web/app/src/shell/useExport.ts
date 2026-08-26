import { useCallback } from "react";
import type { Core, CoreError, DiskSnapshot, InstrumentSnapshot } from "../boundary/contract";
import { saveFile } from "./fileio";
import type { SaveOutcome } from "./fileio";

interface ExportMessages {
  report: (error: CoreError) => void;
  fail: (message: string) => void;
  say: (message: string, kind?: "status" | "ok") => void;
  markClean: () => void;
}

/** Owns durable document output. The shell only decides when an export begins. */
export function useExport(
  core: Core,
  disk: DiskSnapshot | null,
  instrument: InstrumentSnapshot | null,
  messages: ExportMessages,
) {
  const { report, fail, say, markClean } = messages;

  const exportImage = useCallback(
    (then?: () => void) => {
      const diskLabel = disk?.label.trim() ?? "DISK";
      const landed = (outcomes: SaveOutcome[]) => {
        if (outcomes.includes("cancelled")) {
          say("export cancelled; the disk still holds unsaved changes");
          return;
        }
        markClean();
        say(`exported ${diskLabel}`, "ok");
        then?.();
      };
      const writeFailed = (reason: unknown) => {
        fail(`save failed: ${reason instanceof Error ? reason.message : String(reason)}`);
      };

      if (disk?.disks === 2) {
        void core.exportImageAt(0).then((one) => {
          if (!one.ok) {
            report(one.error);
            return;
          }
          void core.exportImageAt(1).then((two) => {
            if (!two.ok) {
              report(two.error);
              return;
            }
            saveFile(one.value, `${diskLabel}-1.img`)
              .then(async (first) => {
                if (first === "cancelled") return [first];
                return [first, await saveFile(two.value, `${diskLabel}-2.img`)];
              })
              .then(landed, writeFailed);
          });
        });
        return;
      }
      void core.exportImage().then((result) => {
        if (!result.ok) {
          report(result.error);
          return;
        }
        saveFile(result.value, `${diskLabel}.img`).then((outcome) => {
          landed([outcome]);
        }, writeFailed);
      });
    },
    [core, disk, fail, markClean, report, say],
  );

  const exportInstrumentFile = useCallback(() => {
    const dumpName = instrument?.fileName ?? "FULL-DATA-FZ";
    const target = `${disk?.label.trim() ?? "INSTRUMENT"}.fzf`;
    void core.extractFile(dumpName).then((result) => {
      if (!result.ok) {
        report(result.error);
        return;
      }
      saveFile(result.value, target).then(
        (outcome) => {
          say(
            outcome === "saved" ? `exported ${target}` : "export cancelled; nothing was written",
            outcome === "saved" ? "ok" : "status",
          );
        },
        (reason: unknown) => {
          fail(`save failed: ${reason instanceof Error ? reason.message : String(reason)}`);
        },
      );
    });
  }, [core, disk, fail, instrument, report, say]);

  return { exportImage, exportInstrumentFile };
}
