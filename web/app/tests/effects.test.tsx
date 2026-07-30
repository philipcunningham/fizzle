// The effects screen (R19) over the fake core: the bend stepper and
// the 3 by 7 controller modulation matrix.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { openInstrumentDisk } from "./helpers";

async function openEffectsTab() {
  await openInstrumentDisk();
  fireEvent.click(screen.getByRole("tab", { name: "Effects" }));
  await screen.findByText("Controller modulation matrix");
}

describe("effects", () => {
  it("edits a matrix cell by keyboard and reads back the confirmed value", async () => {
    await openEffectsTab();
    const cell = screen.getByRole("spinbutton", { name: "Mod wheel to LFO pitch" });
    fireEvent.keyDown(cell, { key: "ArrowUp" });
    await waitFor(() => {
      expect(
        screen
          .getByRole("spinbutton", { name: "Mod wheel to LFO pitch" })
          .getAttribute("aria-valuenow"),
      ).toBe("1");
    });
  });

  it("edits the bend range and undoes it", async () => {
    await openEffectsTab();
    const bend = screen.getByLabelText("Bend range (1/8 semi)");
    fireEvent.change(bend, { target: { value: "24" } });
    fireEvent.blur(bend);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("Bend range (1/8 semi)").value).toBe("24");
    });
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("Bend range (1/8 semi)").value).toBe("16");
    });
  });
});
