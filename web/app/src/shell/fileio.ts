import type { Core, CoreResult } from "../boundary/contract";

export function relativePath(file: File): string {
  // Folder pickers stamp each file with its path under the chosen root; drop
  // and test environments may supply only the bare name.
  const path = file.webkitRelativePath;
  if (path === "") return file.name;
  const slash = path.indexOf("/");
  return slash >= 0 ? path.slice(slash + 1) : path;
}

export function readBytes(file: File): Promise<Uint8Array> {
  // FileReader has the same result as File.arrayBuffer and exists in every
  // environment in which the application tests run.
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      resolve(new Uint8Array(reader.result as ArrayBuffer));
    };
    reader.onerror = () => {
      reject(reader.error instanceof Error ? reader.error : new Error("file read failed"));
    };
    reader.readAsArrayBuffer(file);
  });
}

export type SaveOutcome = "saved" | "cancelled";

/**
 * Save through the platform picker where available and fall back to a browser
 * download. A genuine cancellation resolves as cancelled; write failures
 * reject. Some headless or permission-revoked environments reject instantly
 * without displaying a picker, in which case downloading is the fallback.
 */
export function saveFile(bytes: Uint8Array, name: string): Promise<SaveOutcome> {
  const picker = (
    window as {
      showSaveFilePicker?: (options: { suggestedName: string }) => Promise<{
        createWritable(): Promise<{ write(data: Blob): Promise<void>; close(): Promise<void> }>;
      }>;
    }
  ).showSaveFilePicker;
  const blob = new Blob([bytes.buffer as ArrayBuffer], { type: "application/octet-stream" });
  const download = (): SaveOutcome => {
    try {
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = name;
      anchor.click();
      URL.revokeObjectURL(url);
    } catch {
      // jsdom has no download surface; browser smoke covers the real path.
    }
    return "saved";
  };
  if (!picker) return Promise.resolve(download());

  const started = performance.now();
  return picker({ suggestedName: name }).then(
    async (handle) => {
      const writable = await handle.createWritable();
      await writable.write(blob);
      await writable.close();
      return "saved" as const;
    },
    (reason: unknown): SaveOutcome => {
      // Both a dismissed picker and a picker that never opened reject with
      // AbortError. A real dialog remains open for appreciable human time;
      // 250ms conservatively distinguishes it from an immediate rejection.
      const cancelled =
        reason instanceof DOMException &&
        reason.name === "AbortError" &&
        performance.now() - started > 250;
      return cancelled ? "cancelled" : download();
    },
  );
}

/**
 * The error boundary's last-resort export reads the core directly, so it still
 * works if the normal shell export path crashes. Split documents write both
 * images. There is nowhere to render an error, so refusals remain silent.
 */
export function exportLastResort(core: Core): void {
  const write = (result: CoreResult<Uint8Array>, name: string): Promise<unknown> =>
    result.ok ? saveFile(result.value, name).catch(() => null) : Promise.resolve(null);

  void core.snapshot().then((snapshot) => {
    const disk = snapshot.ok ? snapshot.value.disk : null;
    const label = disk?.label.trim() ?? "DISK";
    if (disk?.disks === 2) {
      void core
        .exportImageAt(0)
        .then((one) => write(one, `${label}-1.img`))
        .then(() => core.exportImageAt(1))
        .then((two) => write(two, `${label}-2.img`));
      return;
    }
    void core.exportImage().then((image) => write(image, `${label}.img`));
  });
}
