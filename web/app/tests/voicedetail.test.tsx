// Loops (with cross-fade and time) and envelopes on instrument voice
// slots (R16, R17), through the voice editor's tables.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { openInstrumentDisk } from "./helpers";

describe("loops on a slot", () => {
  it("edits start and end in the loops table, clamped to the frames", async () => {
    await openInstrumentDisk();
    const start = await screen.findByLabelText("loop 2 start");
    fireEvent.change(start, { target: { value: "100" } });
    fireEvent.blur(start);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("loop 2 start").value).toBe("100");
    });

    const end = screen.getByLabelText("loop 2 end");
    fireEvent.change(end, { target: { value: "9999999" } });
    fireEvent.blur(end);
    await waitFor(() => {
      // KICK is slot 0: 4096 frames in the fake.
      expect(screen.getByLabelText<HTMLInputElement>("loop 2 end").value).toBe("4096");
    });
  });

  it("edits cross-fade and time (the R14 loop attributes)", async () => {
    await openInstrumentDisk();
    const xf = await screen.findByLabelText("loop 3 crossfade");
    fireEvent.change(xf, { target: { value: "512" } });
    fireEvent.blur(xf);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("loop 3 crossfade").value).toBe("512");
    });
    const field = screen.getByLabelText("loop 3 time");
    fireEvent.change(field, { target: { value: "4000" } });
    fireEvent.blur(field);
    await waitFor(() => {
      // Clamped to the format's 1022 ceiling.
      expect(screen.getByLabelText<HTMLInputElement>("loop 3 time").value).toBe("1022");
    });
  });

  it("selects the sustain loop designation", async () => {
    await openInstrumentDisk();
    const trigger = await screen.findByRole("combobox", { name: "Sustain loop" });
    expect(trigger.textContent).toContain("none");
  });
});

// R14's Sample group. Sample rate is a schema select, so it must reach
// the screen through the schema-driven control path with no code of its
// own, and the value shown must be the params readout's.
describe("the Sample group on a slot", () => {
  it("renders sample rate as a select carrying the voice's rate", async () => {
    await openInstrumentDisk();
    const trigger = await screen.findByRole("combobox", { name: "Sample rate (Hz)" });
    expect(trigger.textContent).toContain("18000");
  });
});

describe("envelopes on a slot", () => {
  it("edits a DCA stage from the numeric grid in display scale", async () => {
    await openInstrumentDisk();
    const rate = await screen.findByLabelText("DCA envelope stage 2 rate");
    fireEvent.change(rate, { target: { value: "65" } });
    fireEvent.blur(rate);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("DCA envelope stage 2 rate").value).toBe("65");
    });
  });

  it("moves the sustain designation from the grid marks", async () => {
    await openInstrumentDisk();
    fireEvent.click(
      await screen.findByRole("button", { name: "DCF envelope set sustain stage 4" }),
    );
    await waitFor(() => {
      const meta = screen.getAllByText(/Sustain S4/);
      expect(meta.length).toBeGreaterThan(0);
    });
  });

  it("undoes an envelope edit as one step", async () => {
    await openInstrumentDisk();
    const rate = await screen.findByLabelText("DCA envelope stage 1 rate");
    const before = (rate as HTMLInputElement).value;
    fireEvent.change(rate, { target: { value: "11" } });
    fireEvent.blur(rate);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("DCA envelope stage 1 rate").value).toBe("11");
    });
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("DCA envelope stage 1 rate").value).toBe(
        before,
      );
    });
  });
});
