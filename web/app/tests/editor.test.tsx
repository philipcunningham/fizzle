// The voice editor screen (R14) over the fake core: schema-driven
// panels editing instrument voice slots, rename, map, and the export
// dialog.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { openInstrumentDisk } from "./helpers";

describe("voice editor", () => {
  it("renders schema groups and edits a knob field on the slot", async () => {
    await openInstrumentDisk();
    await screen.findByText("Voices (3/64)");

    const cutoff = screen.getByRole("slider", { name: "Cutoff" });
    expect(cutoff.getAttribute("aria-valuenow")).toBe("127");
    fireEvent.keyDown(cutoff, { key: "ArrowDown" });
    await waitFor(() => {
      expect(screen.getByRole("slider", { name: "Cutoff" }).getAttribute("aria-valuenow")).toBe(
        "126",
      );
    });
  });

  it("edits a stepper field and clamps typed values to the schema", async () => {
    await openInstrumentDisk();
    const tune = await screen.findByLabelText("Tune (cents)");
    fireEvent.change(tune, { target: { value: "99999" } });
    fireEvent.blur(tune);
    await waitFor(() => {
      // The panel's TUNE row stops at a semitone either way.
      expect(screen.getByLabelText<HTMLInputElement>("Tune (cents)").value).toBe("100");
    });
  });

  it("selects another voice from the table", async () => {
    await openInstrumentDisk();
    fireEvent.click((await screen.findAllByText("SNARE"))[0] as HTMLElement);
    // The waveform caption follows the selected voice's frames
    // (4096 + slot*256; SNARE is slot 1).
    await screen.findByText(/4,352 frames/);
  });

  it("renames a voice by double click", async () => {
    await openInstrumentDisk();
    const name = (await screen.findAllByText("KICK"))[0] as HTMLElement;
    fireEvent.doubleClick(name);
    const input = await screen.findByLabelText("voice name");
    fireEvent.change(input, { target: { value: "THUMP" } });
    fireEvent.blur(input);
    await waitFor(() => {
      expect(screen.getAllByText("THUMP").length).toBeGreaterThan(0);
    });
  });

  it("maps an unreferenced voice in one action (R13)", async () => {
    await openInstrumentDisk();
    await screen.findByText("SPARE");
    expect(screen.getByText("∘")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "map SPARE" }));
    await waitFor(() => {
      expect(screen.queryByText("∘")).toBeNull();
    });
  });

  it("opens the export dialog for a voice (R18)", async () => {
    await openInstrumentDisk();
    fireEvent.click(await screen.findByRole("button", { name: "export SNARE" }));
    await screen.findByText("Export voice");
    fireEvent.click(screen.getByRole("button", { name: "As .fzv" }));
    await waitFor(() => {
      const statuses = screen.getAllByRole("status").map((s) => s.textContent);
      expect(statuses.join(" ")).toContain("exported SNARE as .fzv");
    });
  });
});
