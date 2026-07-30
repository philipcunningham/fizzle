// Export honesty (R25): the dirty flag may only clear on bytes that
// actually reached the disk. A cancelled picker, a failed write, and an
// undo after a save each leave (or restore) the unexported marker.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { openInstrumentDisk } from "./helpers";

// The picker only counts a rejection as a cancel when a dialog was up
// long enough for a person to dismiss it, so time has to move.
function stubClock() {
  let t = 0;
  vi.spyOn(performance, "now").mockImplementation(() => (t += 1000));
}

async function dirtyEdit() {
  await openInstrumentDisk();
  const field = screen.getByLabelText("loop 1 start");
  fireEvent.change(field, { target: { value: "5" } });
  fireEvent.blur(field);
  await screen.findByText("●");
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("export", () => {
  it("keeps the changes marked when the save picker is cancelled", async () => {
    stubClock();
    vi.stubGlobal(
      "showSaveFilePicker",
      vi.fn(() => Promise.reject(new DOMException("cancelled", "AbortError"))),
    );
    await dirtyEdit();

    fireEvent.click(screen.getByRole("button", { name: "Export" }));

    await screen.findByText(/export cancelled/);
    expect(screen.getByText("●")).toBeTruthy();
  });

  it("reports a write that fails and keeps the changes marked", async () => {
    stubClock();
    vi.stubGlobal(
      "showSaveFilePicker",
      vi.fn(() =>
        Promise.resolve({
          createWritable: () => Promise.reject(new Error("the volume is full")),
        }),
      ),
    );
    await dirtyEdit();

    fireEvent.click(screen.getByRole("button", { name: "Export" }));

    await waitFor(() => {
      const alerts = screen.getAllByRole("alert").map((a) => a.textContent);
      expect(alerts.join(" ")).toContain("the volume is full");
    });
    expect(screen.getByText("●")).toBeTruthy();
  });

  it("clears the mark on a real write, and an undo marks it again", async () => {
    // jsdom has no picker, so the save takes the download path and
    // lands.
    await dirtyEdit();

    fireEvent.click(screen.getByRole("button", { name: "Export" }));
    await screen.findByText(/exported/);
    await waitFor(() => {
      expect(screen.queryByText("●")).toBeNull();
    });

    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    await screen.findByText("●");
  });
});
