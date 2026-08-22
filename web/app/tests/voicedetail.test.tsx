// Loops (with cross-fade and time) and envelopes on instrument voice
// slots (R16, R17), through the voice editor's tables.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createFakeCore } from "../src/core/fake";
import { commitField, openInstrumentDisk } from "./helpers";

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

  // The waveform draws the selected loop; when that loop is the one the
  // voice repeats, the caption beside it says so. The designation is a
  // Radix select these tests can't drive, so it goes through the core
  // and the field commit below refetches the snapshot carrying it.
  it("says which loop repeats when the drawn one is the sustain loop", async () => {
    const core = createFakeCore();
    await openInstrumentDisk(core);
    await screen.findByLabelText("loop 1 start");
    expect(screen.queryByText(/repeats while held/)).toBeNull();

    await core.setSlotLoopSelect(0, 0, 8);
    await commitField("loop 1 start", "100");
    expect(screen.getByText(/repeats while held/)).toBeTruthy();
  });

  // The cap, min(loopSustain, loopRelease) (F000:122B), is what holds
  // while a key is down. A release loop earns its own caption only when
  // it sits above the cap, so the fixture needs a genuine sustain loop
  // below a distinct release loop, not "no sustain loop" alone: naming
  // no sustain loop makes the release loop the cap's own loop, and it
  // reads as the sustain loop instead (the case the next test covers).
  // 459 corpus voices name loop_sus below loop_end, but none carries a
  // usable loop in both roles, so this fixture's shape is synthetic.
  it("says a release loop repeats after the key comes up", async () => {
    const core = createFakeCore();
    await openInstrumentDisk(core);
    await screen.findByLabelText("loop 1 start");
    expect(screen.queryByText(/repeats after the key/)).toBeNull();

    // Loop 1 is the sustain loop, loop 2 the release loop, with its own
    // distinct bounds above the cap. Selecting loop 2's row draws it.
    await core.setSlotLoopSelect(0, 0, 1);
    await commitField("loop 1 start", "500");
    await commitField("loop 1 end", "1200");
    await commitField("loop 2 start", "2000");
    await commitField("loop 2 end", "3500");
    fireEvent.click(screen.getByLabelText("loop 2 start"));
    expect(await screen.findByText(/repeats after the key/i)).toBeTruthy();
  });

  // A loop named for both roles (146 corpus voices take this shape) caps
  // the chain at note on already, at min(loop_sus, loop_end) (F000:122B),
  // so the chain runs no further than the sustain loop while the key is
  // held. It reads as the sustain loop, and only that caption shows.
  it("shows only the sustain caption when one loop serves both roles", async () => {
    const core = createFakeCore();
    await openInstrumentDisk(core);
    await screen.findByLabelText("loop 1 start");

    await core.setSlotLoopSelect(0, 0, 0);
    await commitField("loop 1 start", "500");
    await commitField("loop 1 end", "1200");
    expect(await screen.findByText(/repeats while held/)).toBeTruthy();
    expect(screen.queryByText(/repeats after the key/)).toBeNull();
  });
});

// The rate is fixed when a sample is taken, and the FZ panel offers no
// way to change a loaded voice's rate, so fizzle offers no control for
// it either. The LFO sync row is the schema select that replaced it,
// and it has to reach the screen through the same schema-driven path
// with no code of its own.
describe("the Sample group on a slot", () => {
  it("offers no sample rate control, because the panel offers none", async () => {
    await openInstrumentDisk();
    await screen.findByRole("combobox", { name: "Playback" });
    expect(screen.queryByRole("combobox", { name: "Sample rate (Hz)" })).toBeNull();
  });

  it("still shows the rate, because a voice plays back at it", async () => {
    await openInstrumentDisk();
    const rate = await screen.findByLabelText("sample rate");
    expect(rate.textContent).toContain("18000");
  });

  it("renders LFO sync as a select carrying the voice's flag", async () => {
    await openInstrumentDisk();
    const trigger = await screen.findByRole("combobox", { name: "Sync" });
    expect(trigger.textContent).toContain("off");
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

// The voice list's Range column reads the voice's own key range from
// the schema params. The fake used to omit them, so the column showed
// its placeholder in every test and a regression there was invisible.
describe("the voice list's key range", () => {
  it("renders the range each voice carries", async () => {
    await openInstrumentDisk();
    const table = await screen.findByRole("table", { name: "instrument voices" });
    expect(table.textContent).toContain("C2..C7");
  });
});
