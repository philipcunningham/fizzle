// Rows are focusable and select on Enter or Space (Q5), but the row
// handler must not cancel keys that came from a control inside the row.
// A cancelled keydown stops a native button activating and stops a
// space reaching a text field, which would leave Duplicate, Delete,
// Export, and Map keyboard-dead and make a two-word voice name
// impossible to type.
import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { openInstrumentDisk } from "./helpers";

// getAllByRole is typed as possibly-empty; the row is there, or the
// table lookup above it has already failed.
function rowAt(table: HTMLElement, index: number): HTMLElement {
  const row = within(table).getAllByRole("row")[index];
  if (!row) throw new Error(`no row ${String(index)}`);
  return row;
}

// fireEvent returns false when a handler called preventDefault, which
// is exactly the default action a button or a text field needs.
function pressed(element: Element, key: string): boolean {
  return fireEvent.keyDown(element, { key, bubbles: true });
}

describe("row keyboard handling", () => {
  it("leaves Enter on a row's own buttons to the button", async () => {
    await openInstrumentDisk();
    fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
    await screen.findByRole("table", { name: "areas" });

    for (const name of ["duplicate area 1", "delete area 1", "move area 1 down"]) {
      expect(pressed(screen.getByRole("button", { name }), "Enter")).toBe(true);
    }
  });

  it("leaves Enter on a voice row's buttons to the button", async () => {
    await openInstrumentDisk();
    const voices = await screen.findByRole("table", { name: "instrument voices" });
    const row = rowAt(voices, 1);

    for (const button of within(row).getAllByRole("button")) {
      expect(pressed(button, "Enter")).toBe(true);
    }
  });

  it("lets a space reach the rename field, so a name can hold two words", async () => {
    await openInstrumentDisk();
    const voices = await screen.findByRole("table", { name: "instrument voices" });
    const row = rowAt(voices, 1);

    fireEvent.keyDown(row, { key: "F2" });
    const input = await screen.findByLabelText("voice name");
    expect(pressed(input, " ")).toBe(true);

    fireEvent.change(input, { target: { value: "BASS DRUM" } });
    fireEvent.blur(input);
    expect((await within(voices).findAllByText("BASS DRUM")).length).toBeGreaterThan(0);
  });

  it("lets a space reach a loop row's number cells", async () => {
    await openInstrumentDisk();
    const cell = await screen.findByLabelText("loop 1 start");
    expect(pressed(cell, " ")).toBe(true);
  });

  it("still selects the row when the row itself takes the key", async () => {
    await openInstrumentDisk();
    fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
    const table = await screen.findByRole("table", { name: "areas" });
    const row = rowAt(table, 2);

    fireEvent.keyDown(row, { key: "Enter" });
    await screen.findByText(/Edit area · SNARE/);
  });
});
