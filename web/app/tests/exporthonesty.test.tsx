import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useExport } from "../src/shell/useExport";
import { instrumentSnapshot, presentationCore } from "./support/presentation";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function exportHarness() {
  const snapshot = instrumentSnapshot();
  const messages = {
    reported: [] as string[],
    failed: [] as string[],
    said: [] as string[],
    clean: 0,
  };
  const { result } = renderHook(() =>
    useExport(presentationCore(snapshot), snapshot.disk, snapshot.disk?.instrument ?? null, {
      report: (error) => messages.reported.push(error.message),
      fail: (message) => messages.failed.push(message),
      say: (message) => messages.said.push(message),
      markClean: () => {
        messages.clean += 1;
      },
    }),
  );
  return { result, messages };
}

describe("export honesty", () => {
  it("keeps the document dirty when the native picker is cancelled", async () => {
    let time = 0;
    vi.spyOn(performance, "now").mockImplementation(() => (time += 1000));
    vi.stubGlobal(
      "showSaveFilePicker",
      vi.fn(() => Promise.reject(new DOMException("cancelled", "AbortError"))),
    );
    const { result, messages } = exportHarness();
    act(() => {
      result.current.exportImage();
    });
    await waitFor(() => {
      expect(messages.said).toContain("export cancelled; the disk still holds unsaved changes");
    });
    expect(messages.clean).toBe(0);
  });

  it("reports a failed write and keeps the document dirty", async () => {
    vi.stubGlobal(
      "showSaveFilePicker",
      vi.fn(() =>
        Promise.resolve({
          createWritable: () => Promise.reject(new Error("the volume is full")),
        }),
      ),
    );
    const { result, messages } = exportHarness();
    act(() => {
      result.current.exportImage();
    });
    await waitFor(() => {
      expect(messages.failed.join(" ")).toContain("the volume is full");
    });
    expect(messages.clean).toBe(0);
  });

  it("marks clean only after bytes reach a writable", async () => {
    const write = vi.fn(() => Promise.resolve());
    const close = vi.fn(() => Promise.resolve());
    vi.stubGlobal(
      "showSaveFilePicker",
      vi.fn(() =>
        Promise.resolve({
          createWritable: () => Promise.resolve({ write, close }),
        }),
      ),
    );
    const { result, messages } = exportHarness();
    act(() => {
      result.current.exportImage();
    });
    await waitFor(() => {
      expect(messages.clean).toBe(1);
    });
    expect(write).toHaveBeenCalledOnce();
    expect(close).toHaveBeenCalledOnce();
  });
});
